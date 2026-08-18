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

package daemon

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	. "gopkg.in/check.v1"

	commonpb "github.com/canonical/pebble/internals/otlp/common/v1"
	resourcepb "github.com/canonical/pebble/internals/otlp/resource/v1"
	tracepb "github.com/canonical/pebble/internals/otlp/trace/v1"
)

var otlpTracesTestLayer = `
services:
    test1:
        override: replace
        command: sleep 10
`

func (s *apiSuite) postOTLPTraces(c *C, service, contentType string, body []byte) *httptest.ResponseRecorder {
	s.vars = map[string]string{"name": service}
	req, err := http.NewRequest("POST", "/v1/services/"+service+"/otlp/v1/traces", bytes.NewReader(body))
	c.Assert(err, IsNil)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	v1PostOTLPTraces(apiCmd("/v1/services/{name}/otlp/v1/traces"), req, nil).ServeHTTP(rec, req)
	return rec
}

// otlpTracesTargetServer is a small httptest.Server that records the
// bodies of received OTLP/HTTP trace export requests.
type otlpTracesTargetServer struct {
	*httptest.Server
	received chan []byte
}

func newOTLPTracesTargetServer() *otlpTracesTargetServer {
	ts := &otlpTracesTargetServer{
		received: make(chan []byte, 10),
	}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		ts.received <- body
		w.WriteHeader(http.StatusOK)
	}))
	return ts
}

func (ts *otlpTracesTargetServer) waitForTraces(c *C) *tracepb.TracesData {
	select {
	case body := <-ts.received:
		var data tracepb.TracesData
		err := protojson.Unmarshal(body, &data)
		c.Assert(err, IsNil)
		return &data
	case <-time.After(5 * time.Second):
		c.Fatal("timed out waiting for traces to be forwarded to target")
		return nil
	}
}

func (ts *otlpTracesTargetServer) checkNoTracesReceived(c *C) {
	select {
	case body := <-ts.received:
		c.Fatalf("did not expect traces to be forwarded, got: %s", body)
	case <-time.After(100 * time.Millisecond):
	}
}

func (s *apiSuite) TestOTLPTracesJSON(c *C) {
	target := newOTLPTracesTargetServer()
	defer target.Close()

	writeTestLayer(s.pebbleDir, otlpTracesTestLayer)
	s.daemon(c)
	s.startOverlord()
	s.addLayer(c, fmt.Sprintf(`
trace-targets:
    otel-traces:
        override: replace
        type: opentelemetry
        location: %s
        services: [test1]
`, target.URL))
	s.startAndWaitService(c, "test1")

	body := []byte(`{
		"resourceSpans": [{
			"scopeSpans": [{
				"spans": [{"name": "handle-request"}]
			}]
		}]
	}`)
	rec := s.postOTLPTraces(c, "test1", "application/json", body)
	c.Assert(rec.Code, Equals, http.StatusOK)
	c.Check(rec.Header().Get("Content-Type"), Equals, "application/json")
	c.Check(rec.Body.String(), Equals, "{}")

	data := target.waitForTraces(c)
	c.Assert(data.GetResourceSpans(), HasLen, 1)
	rs := data.GetResourceSpans()[0]
	attrs := attrMap(rs.GetResource().GetAttributes())
	c.Check(attrs["service.name"], Equals, "test1")
	c.Check(attrs["pebble.service"], Equals, "test1")
	c.Check(attrs["service.instance.id"], Not(Equals), "")
	c.Assert(rs.GetScopeSpans(), HasLen, 1)
	c.Check(rs.GetScopeSpans()[0].GetSpans()[0].GetName(), Equals, "handle-request")
}

func (s *apiSuite) TestOTLPTracesProtobuf(c *C) {
	target := newOTLPTracesTargetServer()
	defer target.Close()

	writeTestLayer(s.pebbleDir, otlpTracesTestLayer)
	s.daemon(c)
	s.startOverlord()
	s.addLayer(c, fmt.Sprintf(`
trace-targets:
    otel-traces:
        override: replace
        type: opentelemetry
        location: %s
        services: [test1]
`, target.URL))
	s.startAndWaitService(c, "test1")

	data := &tracepb.TracesData{
		ResourceSpans: []*tracepb.ResourceSpans{{
			Resource: &resourcepb.Resource{
				Attributes: []*commonpb.KeyValue{{
					Key:   "existing.attr",
					Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "keep-me"}},
				}},
			},
			ScopeSpans: []*tracepb.ScopeSpans{{
				Spans: []*tracepb.Span{{Name: "db-query"}},
			}},
		}},
	}
	body, err := proto.Marshal(data)
	c.Assert(err, IsNil)

	rec := s.postOTLPTraces(c, "test1", "application/x-protobuf", body)
	c.Assert(rec.Code, Equals, http.StatusOK)
	c.Check(rec.Header().Get("Content-Type"), Equals, "application/x-protobuf")
	c.Check(rec.Body.Bytes(), HasLen, 0)

	received := target.waitForTraces(c)
	c.Assert(received.GetResourceSpans(), HasLen, 1)
	rs := received.GetResourceSpans()[0]
	attrs := attrMap(rs.GetResource().GetAttributes())
	c.Check(attrs["service.name"], Equals, "test1")
	c.Check(attrs["existing.attr"], Equals, "keep-me")
	c.Check(rs.GetScopeSpans()[0].GetSpans()[0].GetName(), Equals, "db-query")
}

func (s *apiSuite) TestOTLPTracesUnknownServiceDiscarded(c *C) {
	target := newOTLPTracesTargetServer()
	defer target.Close()

	writeTestLayer(s.pebbleDir, otlpTracesTestLayer)
	s.daemon(c)
	s.startOverlord()
	s.addLayer(c, fmt.Sprintf(`
trace-targets:
    otel-traces:
        override: replace
        type: opentelemetry
        location: %s
        services: [test1]
`, target.URL))
	s.startAndWaitService(c, "test1")

	body := []byte(`{"resourceSpans": [{"scopeSpans": [{"spans": [{"name": "should_be_dropped"}]}]}]}`)
	rec := s.postOTLPTraces(c, "test1-not-enrolled", "application/json", body)
	c.Assert(rec.Code, Equals, http.StatusOK)
	c.Check(rec.Body.String(), Equals, "{}")

	target.checkNoTracesReceived(c)
}

func (s *apiSuite) TestOTLPTracesUnsupportedContentType(c *C) {
	writeTestLayer(s.pebbleDir, otlpTracesTestLayer)
	s.daemon(c)
	s.startOverlord()
	s.startAndWaitService(c, "test1")

	rec := s.postOTLPTraces(c, "test1", "text/plain", []byte("nope"))
	c.Assert(rec.Code, Equals, http.StatusUnsupportedMediaType)
}

func (s *apiSuite) TestOTLPTracesMalformedBody(c *C) {
	writeTestLayer(s.pebbleDir, otlpTracesTestLayer)
	s.daemon(c)
	s.startOverlord()
	s.startAndWaitService(c, "test1")

	rec := s.postOTLPTraces(c, "test1", "application/json", []byte("not json"))
	c.Assert(rec.Code, Equals, http.StatusBadRequest)

	rec = s.postOTLPTraces(c, "test1", "application/x-protobuf", []byte{0xff, 0xff, 0xff})
	c.Assert(rec.Code, Equals, http.StatusBadRequest)
}
