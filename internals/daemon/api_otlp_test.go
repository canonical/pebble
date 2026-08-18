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
	"net/http"
	"net/http/httptest"
	"time"

	"google.golang.org/protobuf/proto"
	. "gopkg.in/check.v1"

	commonpb "github.com/canonical/pebble/internals/otlp/common/v1"
	logspb "github.com/canonical/pebble/internals/otlp/logs/v1"
	"github.com/canonical/pebble/internals/overlord/servstate"
)

var otlpTestLayer = `
services:
    test1:
        override: replace
        command: sleep 10

log-targets:
    otel-logs:
        override: replace
        type: opentelemetry
        location: http://localhost:4318
        services: [test1]
`

func (s *apiSuite) startAndWaitService(c *C, name string) {
	payload := bytes.NewBufferString(`{"action": "start", "services": ["` + name + `"]}`)
	req, err := http.NewRequest("POST", "/v1/services", payload)
	c.Assert(err, IsNil)
	rsp := v1PostServices(apiCmd("/v1/services"), req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Check(rec.Result().StatusCode, Equals, 202)

	serviceMgr := s.d.overlord.ServiceManager()
	for i := 0; ; i++ {
		if i > 50 {
			c.Fatalf("timed out waiting for service %q to start", name)
		}
		services, err := serviceMgr.Services([]string{name})
		c.Assert(err, IsNil)
		if len(services) == 1 && services[0].Current == servstate.StatusActive {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (s *apiSuite) postOTLPLogs(c *C, service, contentType string, body []byte) *httptest.ResponseRecorder {
	s.vars = map[string]string{"name": service}
	req, err := http.NewRequest("POST", "/v1/services/"+service+"/otlp/v1/logs", bytes.NewReader(body))
	c.Assert(err, IsNil)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	rec := httptest.NewRecorder()
	v1PostOTLPLogs(apiCmd("/v1/services/{name}/otlp/v1/logs"), req, nil).ServeHTTP(rec, req)
	return rec
}

func (s *apiSuite) TestOTLPLogsJSON(c *C) {
	writeTestLayer(s.pebbleDir, otlpTestLayer)
	s.daemon(c)
	s.startOverlord()
	s.startAndWaitService(c, "test1")

	body := []byte(`{
		"resourceLogs": [{
			"scopeLogs": [{
				"logRecords": [
					{
						"timeUnixNano": "1700000000000000000",
						"body": {"stringValue": "hello from otlp"}
					},
					{
						"timeUnixNano": "1700000001000000000",
						"body": {"stringValue": "second line"}
					}
				]
			}]
		}]
	}`)
	rec := s.postOTLPLogs(c, "test1", "application/json", body)
	c.Assert(rec.Code, Equals, http.StatusOK)
	c.Check(rec.Header().Get("Content-Type"), Equals, "application/json")
	c.Check(rec.Body.String(), Equals, "{}")

	logs := s.getServiceLogs(c, "test1")
	c.Assert(len(logs) >= 2, Equals, true)
	last := logs[len(logs)-2:]
	checkLog(c, last[0], "test1", "hello from otlp")
	checkLog(c, last[1], "test1", "second line")
	c.Check(last[0].Time.Equal(time.Unix(0, 1700000000000000000).UTC()), Equals, true)
}

func (s *apiSuite) TestOTLPLogsProtobuf(c *C) {
	writeTestLayer(s.pebbleDir, otlpTestLayer)
	s.daemon(c)
	s.startOverlord()
	s.startAndWaitService(c, "test1")

	message := "hello from protobuf"
	data := &logspb.LogsData{
		ResourceLogs: []*logspb.ResourceLogs{{
			ScopeLogs: []*logspb.ScopeLogs{{
				LogRecords: []*logspb.LogRecord{{
					TimeUnixNano: 1700000002000000000,
					Body: &commonpb.AnyValue{
						Value: &commonpb.AnyValue_StringValue{StringValue: message},
					},
				}},
			}},
		}},
	}
	body, err := proto.Marshal(data)
	c.Assert(err, IsNil)

	rec := s.postOTLPLogs(c, "test1", "application/x-protobuf", body)
	c.Assert(rec.Code, Equals, http.StatusOK)
	c.Check(rec.Header().Get("Content-Type"), Equals, "application/x-protobuf")
	c.Check(rec.Body.Bytes(), HasLen, 0)

	logs := s.getServiceLogs(c, "test1")
	c.Assert(len(logs) >= 1, Equals, true)
	checkLog(c, logs[len(logs)-1], "test1", message)
}

func (s *apiSuite) TestOTLPLogsUnknownServiceDiscarded(c *C) {
	writeTestLayer(s.pebbleDir, otlpTestLayer)
	s.daemon(c)
	s.startOverlord()
	s.startAndWaitService(c, "test1")

	body := []byte(`{"resourceLogs": [{"scopeLogs": [{"logRecords": [{"body": {"stringValue": "should be dropped"}}]}]}]}`)
	rec := s.postOTLPLogs(c, "test1-not-enrolled", "application/json", body)
	c.Assert(rec.Code, Equals, http.StatusOK)
	c.Check(rec.Body.String(), Equals, "{}")
}

func (s *apiSuite) TestOTLPLogsUnsupportedContentType(c *C) {
	writeTestLayer(s.pebbleDir, otlpTestLayer)
	s.daemon(c)
	s.startOverlord()
	s.startAndWaitService(c, "test1")

	rec := s.postOTLPLogs(c, "test1", "text/plain", []byte("nope"))
	c.Assert(rec.Code, Equals, http.StatusUnsupportedMediaType)
}

func (s *apiSuite) TestOTLPLogsMalformedBody(c *C) {
	writeTestLayer(s.pebbleDir, otlpTestLayer)
	s.daemon(c)
	s.startOverlord()
	s.startAndWaitService(c, "test1")

	rec := s.postOTLPLogs(c, "test1", "application/json", []byte("not json"))
	c.Assert(rec.Code, Equals, http.StatusBadRequest)

	rec = s.postOTLPLogs(c, "test1", "application/x-protobuf", []byte{0xff, 0xff, 0xff})
	c.Assert(rec.Code, Equals, http.StatusBadRequest)
}

// getServiceLogs fetches parsed log entries for the named service via
// GET /v1/logs.
func (s *apiSuite) getServiceLogs(c *C, name string) []testLogEntry {
	req, err := http.NewRequest("GET", "/v1/logs?n=-1&services="+name, nil)
	c.Assert(err, IsNil)
	rsp := v1GetLogs(apiCmd("/v1/logs"), req, nil)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Assert(rec.Code, Equals, http.StatusOK)
	return decodeLogs(c, rec.Body)
}
