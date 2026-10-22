// Copyright (c) 2026 Canonical Ltd
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License version 3 as
// published by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package tracing

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/canonical/pebble/cmd"
)

const (
	defaultExportTimeout = 10 * time.Second
	maxRetryBackoff      = 5 * time.Second
	maxErrorBodySize     = 1024
)

// exporterConfig holds the OTLP/HTTP trace exporter settings.
type exporterConfig struct {
	endpoint string
	headers  map[string]string
	timeout  time.Duration
	gzip     bool
	tls      *tls.Config
	// json selects the JSON encoding (the http/json protocol) rather than
	// the default protobuf encoding (http/protobuf).
	json bool
}

// otlpEnv returns the value of the trace-specific OTEL_EXPORTER_OTLP_TRACES_*
// variable if set, falling back to the general OTEL_EXPORTER_OTLP_* one.
func otlpEnv(name string) string {
	if v := os.Getenv("OTEL_EXPORTER_OTLP_TRACES_" + name); v != "" {
		return v
	}
	return os.Getenv("OTEL_EXPORTER_OTLP_" + name)
}

// exporterConfigFromEnv reads the exporter configuration from the standard
// OpenTelemetry environment variables. See:
// https://opentelemetry.io/docs/specs/otel/protocol/exporter/
func exporterConfigFromEnv() (*exporterConfig, error) {
	config := &exporterConfig{timeout: defaultExportTimeout}

	switch protocol := otlpEnv("PROTOCOL"); protocol {
	case "", "http/protobuf":
	case "http/json":
		config.json = true
	default:
		return nil, fmt.Errorf("cannot use OTLP protocol %q: only http/protobuf and http/json are supported", protocol)
	}

	// The signal-specific endpoint is used as-is, whereas the general one
	// is a base URL that the signal's path is appended to.
	if endpoint := os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"); endpoint != "" {
		config.endpoint = endpoint
	} else if endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); endpoint != "" {
		config.endpoint = strings.TrimSuffix(endpoint, "/") + "/v1/traces"
	} else {
		return nil, errors.New("cannot find OTLP endpoint: OTEL_EXPORTER_OTLP_ENDPOINT or OTEL_EXPORTER_OTLP_TRACES_ENDPOINT must be set")
	}
	u, err := url.Parse(config.endpoint)
	if err != nil {
		return nil, fmt.Errorf("cannot parse OTLP endpoint: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("cannot use OTLP endpoint %q: scheme must be http or https", config.endpoint)
	}

	config.headers, err = parseHeaders(otlpEnv("HEADERS"))
	if err != nil {
		return nil, err
	}

	if timeout := otlpEnv("TIMEOUT"); timeout != "" {
		ms, err := strconv.Atoi(timeout)
		if err != nil || ms < 0 {
			return nil, fmt.Errorf("cannot parse OTLP timeout %q: must be a number of milliseconds", timeout)
		}
		config.timeout = time.Duration(ms) * time.Millisecond
	}

	switch compression := otlpEnv("COMPRESSION"); compression {
	case "", "none":
	case "gzip":
		config.gzip = true
	default:
		return nil, fmt.Errorf("cannot use OTLP compression %q: only gzip is supported", compression)
	}

	config.tls, err = tlsConfigFromEnv()
	if err != nil {
		return nil, err
	}
	return config, nil
}

// parseHeaders parses a list of headers in the OTEL_EXPORTER_OTLP_HEADERS
// format: comma-separated key=value pairs, with URL-encoded values.
func parseHeaders(s string) (map[string]string, error) {
	headers := make(map[string]string)
	for pair := range strings.SplitSeq(s, ",") {
		if strings.TrimSpace(pair) == "" {
			continue
		}
		key, value, ok := strings.Cut(pair, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("cannot parse OTLP header %q: must be key=value", pair)
		}
		value, err := url.PathUnescape(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("cannot parse OTLP header %q: %w", key, err)
		}
		headers[key] = value
	}
	return headers, nil
}

func tlsConfigFromEnv() (*tls.Config, error) {
	caFile := otlpEnv("CERTIFICATE")
	certFile := otlpEnv("CLIENT_CERTIFICATE")
	keyFile := otlpEnv("CLIENT_KEY")
	if caFile == "" && certFile == "" && keyFile == "" {
		return nil, nil
	}

	config := &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("cannot read OTLP CA certificate: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("cannot parse OTLP CA certificate %q", caFile)
		}
		config.RootCAs = pool
	}
	if certFile != "" || keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("cannot load OTLP client certificate: %w", err)
		}
		config.Certificates = []tls.Certificate{cert}
	}
	return config, nil
}

// exporter is an sdktrace.SpanExporter that sends spans to an OTLP/HTTP
// endpoint using the protobuf or JSON encoding.
//
// This is used instead of the upstream otlptracehttp exporter, which pulls
// gRPC into the binary.
type exporter struct {
	config    *exporterConfig
	client    *http.Client
	userAgent string

	// ctx is the lifetime of the exporter: cancelling it abandons the
	// exports in progress, including any retries (see abandon).
	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	shutdown bool
	// exporting counts the ExportSpans calls in progress, and idle is closed
	// by the last of them to finish after Shutdown is called (or by Shutdown
	// itself if there are none), so that Shutdown can wait for them.
	exporting int
	idle      chan struct{}
}

var _ sdktrace.SpanExporter = (*exporter)(nil)

func newExporter(config *exporterConfig, version string) *exporter {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = config.tls
	ctx, cancel := context.WithCancel(context.Background())
	return &exporter{
		config:    config,
		client:    &http.Client{Transport: transport},
		userAgent: fmt.Sprintf("%s/%s", cmd.ProgramName, version),
		ctx:       ctx,
		cancel:    cancel,
		idle:      make(chan struct{}),
	}
}

// ExportSpans implements sdktrace.SpanExporter. It returns once the spans
// have been accepted by the collector, or when ctx is done, the export
// timeout passes or the exporter is shut down (see Shutdown), whichever
// comes first.
func (e *exporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.mu.Lock()
	if e.shutdown {
		e.mu.Unlock()
		return nil
	}
	e.exporting++
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		e.exporting--
		if e.shutdown && e.exporting == 0 {
			close(e.idle)
		}
		e.mu.Unlock()
	}()
	if len(spans) == 0 {
		return nil
	}

	// TracesData is wire-compatible with the ExportTraceServiceRequest
	// message that the endpoint expects: both have the resource spans as
	// field 1, named resourceSpans in JSON. This avoids importing the
	// collector protos.
	data := tracesData(spans)
	var body []byte
	var err error
	if e.config.json {
		body, err = data.MarshalJSON()
	} else {
		body, err = data.MarshalBinary()
	}
	if err != nil {
		return fmt.Errorf("cannot marshal spans: %w", err)
	}
	if e.config.gzip {
		body, err = gzipBytes(body)
		if err != nil {
			return fmt.Errorf("cannot compress spans: %w", err)
		}
	}

	ctx, cancel := context.WithTimeout(ctx, e.config.timeout)
	defer cancel()
	// Give up when the exporter is shut down or abandoned, too.
	stop := context.AfterFunc(e.ctx, cancel)
	defer stop()

	backoff := 100 * time.Millisecond
	for {
		retryAfter, err := e.send(ctx, body)
		if err == nil {
			return nil
		}
		if retryAfter < 0 {
			return err
		}
		if retryAfter == 0 {
			retryAfter = backoff
			backoff = min(backoff*2, maxRetryBackoff)
		}
		select {
		case <-time.After(retryAfter):
		case <-ctx.Done():
			return fmt.Errorf("%w (%v)", err, ctx.Err())
		}
	}
}

// send makes a single export request. If the request failed but may be
// retried, it returns the delay requested by the server (or 0 for the
// default backoff); otherwise it returns a negative delay.
func (e *exporter) send(ctx context.Context, body []byte) (retryAfter time.Duration, err error) {
	req, err := http.NewRequestWithContext(ctx, "POST", e.config.endpoint, bytes.NewReader(body))
	if err != nil {
		return -1, err
	}
	for k, v := range e.config.headers {
		req.Header.Set(k, v)
	}
	if e.config.json {
		req.Header.Set("Content-Type", "application/json")
	} else {
		req.Header.Set("Content-Type", "application/x-protobuf")
	}
	req.Header.Set("User-Agent", e.userAgent)
	if e.config.gzip {
		req.Header.Set("Content-Encoding", "gzip")
	}

	resp, err := e.client.Do(req)
	if err != nil {
		// Connection errors are transient.
		return 0, fmt.Errorf("cannot export spans: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// Drain the body so the connection can be reused. A partial
		// success response is informational, so there's nothing to do.
		io.Copy(io.Discard, resp.Body)
		return 0, nil
	}

	msg, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodySize))
	err = fmt.Errorf("cannot export spans: server returned %s: %q", resp.Status, msg)
	switch resp.StatusCode {
	case http.StatusTooManyRequests, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		// Retryable, per the OTLP/HTTP specification.
		return parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()), err
	default:
		return -1, err
	}
}

// parseRetryAfter returns the delay requested by a Retry-After header value,
// which is either a number of seconds or an HTTP-date, or 0 if the value is
// empty, malformed or already passed.
func parseRetryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	if secs, err := strconv.Atoi(value); err == nil {
		return max(time.Duration(secs)*time.Second, 0)
	}
	if t, err := http.ParseTime(value); err == nil {
		return max(t.Sub(now), 0)
	}
	return 0
}

// Shutdown implements sdktrace.SpanExporter. It waits for the exports in
// progress to finish, abandoning them if ctx is done first, and then releases
// the exporter's connections. Exports started after Shutdown is called are
// dropped.
func (e *exporter) Shutdown(ctx context.Context) error {
	e.mu.Lock()
	if !e.shutdown {
		e.shutdown = true
		if e.exporting == 0 {
			close(e.idle)
		}
	}
	e.mu.Unlock()
	defer e.cancel()
	defer e.client.CloseIdleConnections()

	select {
	case <-e.idle:
		return nil
	default:
	}
	select {
	case <-e.idle:
		return nil
	case <-ctx.Done():
		e.abandon()
		<-e.idle
		return fmt.Errorf("cannot wait for trace exports to finish: %w", ctx.Err())
	}
}

// abandon makes the exports in progress give up, including any retries
// waiting to be made, without waiting for them. It's used when there's no
// longer time for them to finish, such as when the process is exiting.
func (e *exporter) abandon() {
	e.cancel()
}

func gzipBytes(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
