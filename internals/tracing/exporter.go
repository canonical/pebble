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
		return nil, fmt.Errorf("unsupported OTLP protocol %q (only http/protobuf and http/json are supported)", protocol)
	}

	// The signal-specific endpoint is used as-is, whereas the general one
	// is a base URL that the signal's path is appended to.
	if endpoint := os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"); endpoint != "" {
		config.endpoint = endpoint
	} else if endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); endpoint != "" {
		config.endpoint = strings.TrimSuffix(endpoint, "/") + "/v1/traces"
	} else {
		return nil, errors.New("no OTLP endpoint configured")
	}
	u, err := url.Parse(config.endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid OTLP endpoint: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("invalid OTLP endpoint %q: scheme must be http or https", config.endpoint)
	}

	config.headers, err = parseHeaders(otlpEnv("HEADERS"))
	if err != nil {
		return nil, err
	}

	if timeout := otlpEnv("TIMEOUT"); timeout != "" {
		ms, err := strconv.Atoi(timeout)
		if err != nil || ms < 0 {
			return nil, fmt.Errorf("invalid OTLP timeout %q: must be a number of milliseconds", timeout)
		}
		config.timeout = time.Duration(ms) * time.Millisecond
	}

	switch compression := otlpEnv("COMPRESSION"); compression {
	case "", "none":
	case "gzip":
		config.gzip = true
	default:
		return nil, fmt.Errorf("unsupported OTLP compression %q", compression)
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
			return nil, fmt.Errorf("invalid OTLP header %q: must be key=value", pair)
		}
		value, err := url.PathUnescape(strings.TrimSpace(value))
		if err != nil {
			return nil, fmt.Errorf("invalid OTLP header %q: %w", key, err)
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

	mu       sync.Mutex
	shutdown bool
}

var _ sdktrace.SpanExporter = (*exporter)(nil)

func newExporter(config *exporterConfig, version string) *exporter {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = config.tls
	return &exporter{
		config:    config,
		client:    &http.Client{Transport: transport},
		userAgent: fmt.Sprintf("%s/%s", cmd.ProgramName, version),
	}
}

// ExportSpans implements sdktrace.SpanExporter.
func (e *exporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.mu.Lock()
	shutdown := e.shutdown
	e.mu.Unlock()
	if shutdown || len(spans) == 0 {
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
		if secs, convErr := strconv.Atoi(resp.Header.Get("Retry-After")); convErr == nil && secs > 0 {
			return time.Duration(secs) * time.Second, err
		}
		return 0, err
	default:
		return -1, err
	}
}

// Shutdown implements sdktrace.SpanExporter.
func (e *exporter) Shutdown(ctx context.Context) error {
	e.mu.Lock()
	e.shutdown = true
	e.mu.Unlock()
	e.client.CloseIdleConnections()
	return nil
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
