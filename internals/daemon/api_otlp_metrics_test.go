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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	"google.golang.org/protobuf/proto"
	. "gopkg.in/check.v1"

	commonpb "github.com/canonical/pebble/internals/otlp/common/v1"
	metricspb "github.com/canonical/pebble/internals/otlp/metrics/v1"
	resourcepb "github.com/canonical/pebble/internals/otlp/resource/v1"
)

var otlpMetricsTestLayer = `
services:
    test1:
        override: replace
        command: sleep 10
`

func (s *apiSuite) addLayer(c *C, layerYAML string) {
	payload, err := json.Marshal(map[string]any{
		"action":  "add",
		"combine": true,
		"label":   "otlp-metrics-test",
		"format":  "yaml",
		"layer":   layerYAML,
	})
	c.Assert(err, IsNil)
	req, err := http.NewRequest("POST", "/v1/layers", bytes.NewReader(payload))
	c.Assert(err, IsNil)
	rsp := v1PostLayers(apiCmd("/v1/layers"), req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Assert(rec.Code, Equals, 200, Commentf("body: %s", rec.Body.String()))
}

func (s *apiSuite) postOTLPMetrics(c *C, service, contentType string, body []byte) *httptest.ResponseRecorder {
	s.vars = map[string]string{"name": service}
	req, err := http.NewRequest("POST", "/v1/services/"+service+"/otlp/v1/metrics", bytes.NewReader(body))
	c.Assert(err, IsNil)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	v1PostOTLPMetrics(apiCmd("/v1/services/{name}/otlp/v1/metrics"), req, nil).ServeHTTP(rec, req)
	return rec
}

// otlpMetricsTargetServer is a small httptest.Server that records the
// bodies of received OTLP/HTTP metrics export requests.
type otlpMetricsTargetServer struct {
	*httptest.Server
	received chan []byte
}

func newOTLPMetricsTargetServer() *otlpMetricsTargetServer {
	ts := &otlpMetricsTargetServer{
		received: make(chan []byte, 10),
	}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		ts.received <- body
		w.WriteHeader(http.StatusOK)
	}))
	return ts
}

func (ts *otlpMetricsTargetServer) waitForMetrics(c *C) *metricspb.MetricsData {
	select {
	case body := <-ts.received:
		var data metricspb.MetricsData
		err := proto.Unmarshal(body, &data)
		c.Assert(err, IsNil)
		return &data
	case <-time.After(5 * time.Second):
		c.Fatal("timed out waiting for metrics to be forwarded to target")
		return nil
	}
}

func (ts *otlpMetricsTargetServer) checkNoMetricsReceived(c *C) {
	select {
	case body := <-ts.received:
		c.Fatalf("did not expect metrics to be forwarded, got: %s", body)
	case <-time.After(100 * time.Millisecond):
	}
}

func attrMap(attrs []*commonpb.KeyValue) map[string]string {
	m := make(map[string]string, len(attrs))
	for _, a := range attrs {
		m[a.GetKey()] = a.GetValue().GetStringValue()
	}
	return m
}

func (s *apiSuite) TestOTLPMetricsJSON(c *C) {
	target := newOTLPMetricsTargetServer()
	defer target.Close()

	writeTestLayer(s.pebbleDir, otlpMetricsTestLayer)
	s.daemon(c)
	s.startOverlord()
	s.addLayer(c, fmt.Sprintf(`
metrics-targets:
    otel-metrics:
        override: replace
        type: opentelemetry
        location: %s
        services: [test1]
`, target.URL))
	s.startAndWaitService(c, "test1")

	body := []byte(`{
		"resourceMetrics": [{
			"scopeMetrics": [{
				"metrics": [{"name": "requests_total"}]
			}]
		}]
	}`)
	rec := s.postOTLPMetrics(c, "test1", "application/json", body)
	c.Assert(rec.Code, Equals, http.StatusOK)
	c.Check(rec.Header().Get("Content-Type"), Equals, "application/json")
	c.Check(rec.Body.String(), Equals, "{}")

	data := target.waitForMetrics(c)
	c.Assert(data.GetResourceMetrics(), HasLen, 1)
	rm := data.GetResourceMetrics()[0]
	attrs := attrMap(rm.GetResource().GetAttributes())
	c.Check(attrs["service.name"], Equals, "test1")
	c.Check(attrs["pebble.service"], Equals, "test1")
	c.Check(attrs["service.instance.id"], Not(Equals), "")
	c.Assert(rm.GetScopeMetrics(), HasLen, 1)
	c.Check(rm.GetScopeMetrics()[0].GetMetrics()[0].GetName(), Equals, "requests_total")
}

func (s *apiSuite) TestOTLPMetricsProtobuf(c *C) {
	target := newOTLPMetricsTargetServer()
	defer target.Close()

	writeTestLayer(s.pebbleDir, otlpMetricsTestLayer)
	s.daemon(c)
	s.startOverlord()
	s.addLayer(c, fmt.Sprintf(`
metrics-targets:
    otel-metrics:
        override: replace
        type: opentelemetry
        location: %s
        services: [test1]
`, target.URL))
	s.startAndWaitService(c, "test1")

	data := &metricspb.MetricsData{
		ResourceMetrics: []*metricspb.ResourceMetrics{{
			Resource: &resourcepb.Resource{
				Attributes: []*commonpb.KeyValue{{
					Key:   "existing.attr",
					Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "keep-me"}},
				}},
			},
			ScopeMetrics: []*metricspb.ScopeMetrics{{
				Metrics: []*metricspb.Metric{{Name: "cpu_usage"}},
			}},
		}},
	}
	body, err := proto.Marshal(data)
	c.Assert(err, IsNil)

	rec := s.postOTLPMetrics(c, "test1", "application/x-protobuf", body)
	c.Assert(rec.Code, Equals, http.StatusOK)
	c.Check(rec.Header().Get("Content-Type"), Equals, "application/x-protobuf")
	c.Check(rec.Body.Bytes(), HasLen, 0)

	received := target.waitForMetrics(c)
	c.Assert(received.GetResourceMetrics(), HasLen, 1)
	rm := received.GetResourceMetrics()[0]
	attrs := attrMap(rm.GetResource().GetAttributes())
	c.Check(attrs["service.name"], Equals, "test1")
	c.Check(attrs["existing.attr"], Equals, "keep-me")
	c.Check(rm.GetScopeMetrics()[0].GetMetrics()[0].GetName(), Equals, "cpu_usage")
}

func (s *apiSuite) TestOTLPMetricsUnknownServiceDiscarded(c *C) {
	target := newOTLPMetricsTargetServer()
	defer target.Close()

	writeTestLayer(s.pebbleDir, otlpMetricsTestLayer)
	s.daemon(c)
	s.startOverlord()
	s.addLayer(c, fmt.Sprintf(`
metrics-targets:
    otel-metrics:
        override: replace
        type: opentelemetry
        location: %s
        services: [test1]
`, target.URL))
	s.startAndWaitService(c, "test1")

	body := []byte(`{"resourceMetrics": [{"scopeMetrics": [{"metrics": [{"name": "should_be_dropped"}]}]}]}`)
	rec := s.postOTLPMetrics(c, "test1-not-enrolled", "application/json", body)
	c.Assert(rec.Code, Equals, http.StatusOK)
	c.Check(rec.Body.String(), Equals, "{}")

	target.checkNoMetricsReceived(c)
}

func (s *apiSuite) TestOTLPMetricsUnsupportedContentType(c *C) {
	writeTestLayer(s.pebbleDir, otlpMetricsTestLayer)
	s.daemon(c)
	s.startOverlord()
	s.startAndWaitService(c, "test1")

	rec := s.postOTLPMetrics(c, "test1", "text/plain", []byte("nope"))
	c.Assert(rec.Code, Equals, http.StatusUnsupportedMediaType)
}

func (s *apiSuite) TestOTLPMetricsMalformedBody(c *C) {
	writeTestLayer(s.pebbleDir, otlpMetricsTestLayer)
	s.daemon(c)
	s.startOverlord()
	s.startAndWaitService(c, "test1")

	rec := s.postOTLPMetrics(c, "test1", "application/json", []byte("not json"))
	c.Assert(rec.Code, Equals, http.StatusBadRequest)

	rec = s.postOTLPMetrics(c, "test1", "application/x-protobuf", []byte{0xff, 0xff, 0xff})
	c.Assert(rec.Code, Equals, http.StatusBadRequest)
}
