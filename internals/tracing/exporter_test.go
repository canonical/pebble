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
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	. "gopkg.in/check.v1"

	commonpb "github.com/canonical/pebble/internals/otlp/common/v1"
	tracepb "github.com/canonical/pebble/internals/otlp/trace/v1"
	"github.com/canonical/pebble/internals/testutil"
)

type exporterSuite struct {
	testutil.BaseTest
}

var _ = Suite(&exporterSuite{})

func (s *exporterSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	// Start each test with none of the exporter variables set.
	for _, name := range []string{"ENDPOINT", "PROTOCOL", "HEADERS", "TIMEOUT", "COMPRESSION", "CERTIFICATE", "CLIENT_CERTIFICATE", "CLIENT_KEY"} {
		s.Setenv("OTEL_EXPORTER_OTLP_"+name, "")
		s.Setenv("OTEL_EXPORTER_OTLP_TRACES_"+name, "")
	}
}

func (s *exporterSuite) TestExporterConfigFromEnv(c *C) {
	s.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4318/")
	s.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "a=1, b = x%20y ,api-key=foo=bar")
	s.Setenv("OTEL_EXPORTER_OTLP_TIMEOUT", "2500")
	s.Setenv("OTEL_EXPORTER_OTLP_TRACES_COMPRESSION", "gzip")

	config, err := exporterConfigFromEnv()
	c.Assert(err, IsNil)
	c.Check(config.endpoint, Equals, "http://collector:4318/v1/traces")
	c.Check(config.headers, DeepEquals, map[string]string{"a": "1", "b": "x y", "api-key": "foo=bar"})
	c.Check(config.timeout, Equals, 2500*time.Millisecond)
	c.Check(config.gzip, Equals, true)
	c.Check(config.tls, IsNil)

	// The signal-specific endpoint is used as-is.
	s.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "https://collector/custom")
	config, err = exporterConfigFromEnv()
	c.Assert(err, IsNil)
	c.Check(config.endpoint, Equals, "https://collector/custom")
	c.Check(config.json, Equals, false)
}

func (s *exporterSuite) TestExporterConfigFromEnvProtocol(c *C) {
	s.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4318")
	for _, test := range []struct {
		protocol string
		json     bool
	}{
		{"", false},
		{"http/protobuf", false},
		{"http/json", true},
	} {
		s.Setenv("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", test.protocol)
		config, err := exporterConfigFromEnv()
		c.Assert(err, IsNil)
		c.Check(config.json, Equals, test.json, Commentf("protocol %q", test.protocol))
	}
}

func (s *exporterSuite) TestExporterConfigFromEnvErrors(c *C) {
	tests := []struct {
		env map[string]string
		err string
	}{{
		env: map[string]string{},
		err: "no OTLP endpoint configured",
	}, {
		env: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "collector:4318"},
		err: `invalid OTLP endpoint "collector:4318/v1/traces": scheme must be http or https`,
	}, {
		env: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c", "OTEL_EXPORTER_OTLP_PROTOCOL": "grpc"},
		err: `unsupported OTLP protocol "grpc" \(only http/protobuf and http/json are supported\)`,
	}, {
		env: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c", "OTEL_EXPORTER_OTLP_HEADERS": "novalue"},
		err: `invalid OTLP header "novalue": must be key=value`,
	}, {
		env: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c", "OTEL_EXPORTER_OTLP_TIMEOUT": "10s"},
		err: `invalid OTLP timeout "10s": must be a number of milliseconds`,
	}, {
		env: map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://c", "OTEL_EXPORTER_OTLP_COMPRESSION": "zstd"},
		err: `unsupported OTLP compression "zstd"`,
	}}
	for _, test := range tests {
		c.Logf("expecting error %q", test.err)
		s.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
		s.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "")
		s.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "")
		s.Setenv("OTEL_EXPORTER_OTLP_TIMEOUT", "")
		s.Setenv("OTEL_EXPORTER_OTLP_COMPRESSION", "")
		for k, v := range test.env {
			s.Setenv(k, v)
		}
		_, err := exporterConfigFromEnv()
		c.Check(err, ErrorMatches, test.err)
	}
}

type fakeCollector struct {
	mu       sync.Mutex
	requests []*http.Request
	data     []*tracepb.TracesData
	// responses holds the status codes to return, in order; once
	// exhausted, 200 is returned.
	responses []int
}

func (c *fakeCollector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, r)

	var body io.Reader = r.Body
	if r.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		body = gz
	}
	b, err := io.ReadAll(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var data tracepb.TracesData
	switch r.Header.Get("Content-Type") {
	case "application/x-protobuf":
		err = proto.Unmarshal(b, &data)
	case "application/json":
		err = unmarshalJSON(b, &data)
	default:
		err = fmt.Errorf("unexpected content type %q", r.Header.Get("Content-Type"))
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if len(c.responses) > 0 {
		code := c.responses[0]
		c.responses = c.responses[1:]
		if code != http.StatusOK {
			http.Error(w, "try later", code)
			return
		}
	}
	c.data = append(c.data, &data)
	w.Header().Set("Content-Type", r.Header.Get("Content-Type"))
}

// unmarshalJSON decodes the OTLP/HTTP JSON encoding, which differs from the
// protobuf JSON mapping in using hex rather than base64 for trace and span
// IDs. It fails if any ID isn't valid hex.
func unmarshalJSON(b []byte, data *tracepb.TracesData) error {
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return err
	}
	unhex := func(obj map[string]any, fields ...string) error {
		for _, field := range fields {
			v, ok := obj[field].(string)
			if !ok {
				continue
			}
			id, err := hex.DecodeString(v)
			if err != nil {
				return fmt.Errorf("invalid %s %q: %w", field, v, err)
			}
			obj[field] = base64.StdEncoding.EncodeToString(id)
		}
		return nil
	}
	for _, rs := range objects(doc["resourceSpans"]) {
		for _, ss := range objects(rs["scopeSpans"]) {
			for _, span := range objects(ss["spans"]) {
				if err := unhex(span, "traceId", "spanId", "parentSpanId"); err != nil {
					return err
				}
				for _, link := range objects(span["links"]) {
					if err := unhex(link, "traceId", "spanId"); err != nil {
						return err
					}
				}
			}
		}
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return protojson.Unmarshal(b, data)
}

// recordSpans creates spans using a real tracer provider and returns them
// once ended.
func recordSpans() []sdktrace.ReadOnlySpan {
	recorder := &spanRecorder{}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(recorder),
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", "test"))),
	)
	defer tp.Shutdown(context.Background())

	remoteParent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1},
		SpanID:     trace.SpanID{2},
		TraceFlags: trace.FlagsSampled,
		Remote:     true,
	})
	ctx := trace.ContextWithRemoteSpanContext(context.Background(), remoteParent)
	ctx, parent := tp.Tracer("scope-a").Start(ctx, "parent", trace.WithAttributes(
		attribute.String("s", "v"),
		attribute.Int("i", 42),
		attribute.Bool("b", true),
		attribute.Float64("f", 1.5),
		attribute.StringSlice("ss", []string{"x", "y"}),
	))
	_, child := tp.Tracer("scope-b").Start(ctx, "child")
	child.AddEvent("retry", trace.WithAttributes(attribute.String("reason", "busy")))
	child.RecordError(errors.New("boom"))
	child.SetStatus(codes.Error, "boom")
	child.End()
	parent.End()
	return recorder.spans
}

type spanRecorder struct {
	spans []sdktrace.ReadOnlySpan
}

func (r *spanRecorder) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	r.spans = append(r.spans, spans...)
	return nil
}

func (r *spanRecorder) Shutdown(ctx context.Context) error { return nil }

func (s *exporterSuite) TestExportSpansProtobuf(c *C) {
	s.testExportSpans(c, false)
}

func (s *exporterSuite) TestExportSpansJSON(c *C) {
	s.testExportSpans(c, true)
}

func (s *exporterSuite) testExportSpans(c *C, useJSON bool) {
	collector := &fakeCollector{}
	server := httptest.NewServer(collector)
	defer server.Close()

	e := newExporter(&exporterConfig{
		endpoint: server.URL + "/v1/traces",
		headers:  map[string]string{"Authorization": "Bearer token"},
		timeout:  5 * time.Second,
		gzip:     true,
		json:     useJSON,
	}, "1.2.3")
	err := e.ExportSpans(context.Background(), recordSpans())
	c.Assert(err, IsNil)

	c.Assert(collector.requests, HasLen, 1)
	req := collector.requests[0]
	c.Check(req.Method, Equals, "POST")
	c.Check(req.URL.Path, Equals, "/v1/traces")
	if useJSON {
		c.Check(req.Header.Get("Content-Type"), Equals, "application/json")
	} else {
		c.Check(req.Header.Get("Content-Type"), Equals, "application/x-protobuf")
	}
	c.Check(req.Header.Get("Content-Encoding"), Equals, "gzip")
	c.Check(req.Header.Get("Authorization"), Equals, "Bearer token")
	c.Check(req.Header.Get("User-Agent"), Equals, "pebble/1.2.3")

	c.Assert(collector.data, HasLen, 1)
	data := collector.data[0]
	c.Assert(data.ResourceSpans, HasLen, 1)
	rs := data.ResourceSpans[0]
	c.Check(attrMap(rs.Resource.Attributes)["service.name"].GetStringValue(), Equals, "test")
	c.Assert(rs.ScopeSpans, HasLen, 2)
	spans := make(map[string]*tracepb.Span)
	for _, ss := range rs.ScopeSpans {
		c.Assert(ss.Spans, HasLen, 1)
		spans[ss.Scope.Name] = ss.Spans[0]
	}

	parent := spans["scope-a"]
	c.Assert(parent, NotNil)
	c.Check(parent.Name, Equals, "parent")
	wantTraceID := trace.TraceID{1}
	c.Check(parent.TraceId, DeepEquals, wantTraceID[:])
	wantParentID := trace.SpanID{2}
	c.Check(parent.ParentSpanId, DeepEquals, wantParentID[:])
	// Sampled, has-is-remote, is-remote.
	c.Check(parent.Flags, Equals, uint32(0x01|0x100|0x200))
	c.Check(parent.Kind, Equals, tracepb.Span_SPAN_KIND_INTERNAL)
	c.Check(parent.StartTimeUnixNano, Not(Equals), uint64(0))
	c.Check(parent.EndTimeUnixNano >= parent.StartTimeUnixNano, Equals, true)
	attrs := attrMap(parent.Attributes)
	c.Check(attrs["s"].GetStringValue(), Equals, "v")
	c.Check(attrs["i"].GetIntValue(), Equals, int64(42))
	c.Check(attrs["b"].GetBoolValue(), Equals, true)
	c.Check(attrs["f"].GetDoubleValue(), Equals, 1.5)
	ss := attrs["ss"].GetArrayValue().GetValues()
	c.Assert(ss, HasLen, 2)
	c.Check(ss[0].GetStringValue(), Equals, "x")
	c.Check(ss[1].GetStringValue(), Equals, "y")

	child := spans["scope-b"]
	c.Assert(child, NotNil)
	c.Check(child.ParentSpanId, DeepEquals, parent.SpanId)
	c.Check(child.Flags, Equals, uint32(0x01|0x100))
	c.Check(child.Status.Code, Equals, tracepb.Status_STATUS_CODE_ERROR)
	c.Check(child.Status.Message, Equals, "boom")
	c.Assert(child.Events, HasLen, 2)
	c.Check(child.Events[0].Name, Equals, "retry")
	c.Check(attrMap(child.Events[0].Attributes)["reason"].GetStringValue(), Equals, "busy")
	c.Check(child.Events[1].Name, Equals, "exception")
}

func (s *exporterSuite) TestMarshalJSON(c *C) {
	b, err := marshalJSON(tracesData(recordSpans()))
	c.Assert(err, IsNil)

	var doc struct {
		ResourceSpans []struct {
			ScopeSpans []struct {
				Scope struct {
					Name string `json:"name"`
				} `json:"scope"`
				Spans []map[string]any `json:"spans"`
			} `json:"scopeSpans"`
		} `json:"resourceSpans"`
	}
	c.Assert(json.Unmarshal(b, &doc), IsNil)
	c.Assert(doc.ResourceSpans, HasLen, 1)
	spans := make(map[string]map[string]any)
	for _, ss := range doc.ResourceSpans[0].ScopeSpans {
		c.Assert(ss.Spans, HasLen, 1)
		spans[ss.Scope.Name] = ss.Spans[0]
	}

	// IDs are hex, not base64.
	parent := spans["scope-a"]
	c.Check(parent["traceId"], Equals, "01000000000000000000000000000000")
	c.Check(parent["parentSpanId"], Equals, "0200000000000000")
	c.Check(parent["spanId"], Matches, "[0-9a-f]{16}")
	child := spans["scope-b"]
	c.Check(child["parentSpanId"], Equals, parent["spanId"])

	// Enums are integers, and 64-bit integers are strings.
	c.Check(parent["kind"], Equals, float64(1))
	c.Check(child["status"], DeepEquals, map[string]any{"code": float64(2), "message": "boom"})
	c.Check(parent["startTimeUnixNano"], FitsTypeOf, "")
	var intAttr any
	for _, attr := range parent["attributes"].([]any) {
		if attr := attr.(map[string]any); attr["key"] == "i" {
			intAttr = attr["value"]
		}
	}
	c.Check(intAttr, DeepEquals, map[string]any{"intValue": "42"})
}

func (s *exporterSuite) TestExportSpansRetries(c *C) {
	collector := &fakeCollector{responses: []int{http.StatusServiceUnavailable, http.StatusTooManyRequests}}
	server := httptest.NewServer(collector)
	defer server.Close()

	e := newExporter(&exporterConfig{endpoint: server.URL, timeout: 5 * time.Second}, "1")
	err := e.ExportSpans(context.Background(), recordSpans())
	c.Assert(err, IsNil)
	c.Check(collector.requests, HasLen, 3)
	c.Check(collector.data, HasLen, 1)
}

func (s *exporterSuite) TestExportSpansPermanentError(c *C) {
	collector := &fakeCollector{responses: []int{http.StatusBadRequest}}
	server := httptest.NewServer(collector)
	defer server.Close()

	e := newExporter(&exporterConfig{endpoint: server.URL, timeout: 5 * time.Second}, "1")
	err := e.ExportSpans(context.Background(), recordSpans())
	c.Check(err, ErrorMatches, `cannot export spans: server returned 400 Bad Request: .*`)
	c.Check(collector.requests, HasLen, 1)
}

func (s *exporterSuite) TestExportSpansTimeout(c *C) {
	collector := &fakeCollector{responses: []int{503, 503, 503, 503, 503, 503, 503, 503, 503, 503}}
	server := httptest.NewServer(collector)
	defer server.Close()

	e := newExporter(&exporterConfig{endpoint: server.URL, timeout: 250 * time.Millisecond}, "1")
	err := e.ExportSpans(context.Background(), recordSpans())
	c.Check(err, ErrorMatches, `cannot export spans: server returned 503 Service Unavailable: .* \(context deadline exceeded\)`)
}

func (s *exporterSuite) TestExportSpansAfterShutdown(c *C) {
	collector := &fakeCollector{}
	server := httptest.NewServer(collector)
	defer server.Close()

	e := newExporter(&exporterConfig{endpoint: server.URL, timeout: time.Second}, "1")
	c.Assert(e.Shutdown(context.Background()), IsNil)
	c.Assert(e.ExportSpans(context.Background(), recordSpans()), IsNil)
	c.Check(collector.requests, HasLen, 0)
}

func attrMap(kvs []*commonpb.KeyValue) map[string]*commonpb.AnyValue {
	m := make(map[string]*commonpb.AnyValue)
	for _, kv := range kvs {
		m[kv.Key] = kv.Value
	}
	return m
}
