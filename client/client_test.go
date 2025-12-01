// Copyright (c) 2014-2020 Canonical Ltd
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

package client_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type clientSuite struct {
	cli          *client.Client
	req          *http.Request
	reqs         []*http.Request
	serverIdCert *x509.Certificate
	rsp          string
	rsps         []string
	err          error
	doCalls      int
	header       http.Header
	status       int
	tmpDir       string
	socketPath   string
	restore      func()
}

var _ = Suite(&clientSuite{})

func (cs *clientSuite) SetUpTest(c *C) {
	var err error
	cs.cli, err = client.New(nil)
	c.Assert(err, IsNil)
	cs.cli.SetDoer(cs)
	cs.err = nil
	cs.req = nil
	cs.reqs = nil
	cs.rsp = ""
	cs.rsps = nil
	cs.req = nil
	cs.header = nil
	cs.status = 200
	cs.doCalls = 0

	cs.tmpDir = c.MkDir()
	cs.socketPath = filepath.Join(cs.tmpDir, "pebble.socket")

	cs.restore = client.FakeDoRetry(time.Millisecond, 10*time.Millisecond)
}

func (cs *clientSuite) TearDownTest(c *C) {
	cs.restore()
}

// FakeTLSServer results in the inclusion of TLS certificates in the
// HTTP response.
func (cs *clientSuite) FakeTLSServer(idCert *x509.Certificate) {
	cs.serverIdCert = idCert
}

func (cs *clientSuite) Do(req *http.Request) (*http.Response, error) {
	cs.req = req
	cs.reqs = append(cs.reqs, req)
	body := cs.rsp
	if cs.doCalls < len(cs.rsps) {
		body = cs.rsps[cs.doCalls]
	}
	rsp := &http.Response{
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     cs.header,
		StatusCode: cs.status,
	}
	if cs.serverIdCert != nil {
		// Pretend this is a HTTPS connection.
		rsp.TLS = &tls.ConnectionState{
			PeerCertificates: []*x509.Certificate{
				// TLS certificate.
				&x509.Certificate{},
				// ID Certificate.
				cs.serverIdCert,
			},
		}
	}
	cs.doCalls++
	return rsp, cs.err
}

func (cs *clientSuite) TestNewBaseURLError(c *C) {
	_, err := client.New(&client.Config{BaseURL: ":"})
	c.Assert(err, ErrorMatches, `cannot parse base URL: parse ":": missing protocol scheme`)
}

func (cs *clientSuite) TestClientDoReportsErrors(c *C) {
	cs.err = errors.New("ouchie")
	_, err := cs.cli.Requester().Do(context.Background(), &client.RequestOptions{
		Type:   client.RawRequest,
		Method: "GET",
		Path:   "/",
	})
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, "cannot communicate with server: ouchie")
	if cs.doCalls < 2 {
		c.Fatalf("do did not retry")
	}
}

func (cs *clientSuite) TestContextCancellation(c *C) {
	cs.err = errors.New("ouchie")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel it right away
	_, err := cs.cli.Requester().Do(ctx, &client.RequestOptions{
		Type:   client.SyncRequest,
		Method: "GET",
		Path:   "/",
	})
	c.Check(err, ErrorMatches, "cannot communicate with server: ouchie")

	// This would be 10 if context wasn't respected, due to timeout
	c.Assert(cs.doCalls, Equals, 1)
}

func (cs *clientSuite) TestClientWorks(c *C) {
	var v []int
	cs.rsp = `[1,2]`
	reqBody := io.NopCloser(strings.NewReader(""))
	resp, err := cs.cli.Requester().Do(context.Background(), &client.RequestOptions{
		Type:   client.RawRequest,
		Method: "GET",
		Path:   "/this",
		Body:   reqBody,
	})
	c.Check(err, IsNil)
	dec := json.NewDecoder(resp.Body)
	err = dec.Decode(&v)
	c.Check(err, IsNil)
	c.Check(v, DeepEquals, []int{1, 2})
	c.Assert(cs.req, NotNil)
	c.Assert(cs.req.URL, NotNil)
	c.Check(cs.req.Method, Equals, "GET")
	c.Check(cs.req.Body, Equals, reqBody)
	c.Check(cs.req.URL.Path, Equals, "/this")
}

func (cs *clientSuite) TestClientDefaultsToNoAuthorization(c *C) {
	_, _ = cs.cli.Requester().Do(context.Background(), &client.RequestOptions{
		Type:   client.RawRequest,
		Method: "GET",
		Path:   "/this",
	})
	c.Assert(cs.req, NotNil)
	authorization := cs.req.Header.Get("Authorization")
	c.Check(authorization, Equals, "")
}

func (cs *clientSuite) TestClientSysInfo(c *C) {
	cs.rsp = `{"type": "sync", "result": {"version": "1"}}`
	sysInfo, err := cs.cli.SysInfo()
	c.Check(err, IsNil)
	c.Check(sysInfo, DeepEquals, &client.SysInfo{Version: "1"})
}

func (cs *clientSuite) TestClientReportsOpError(c *C) {
	cs.rsp = `{"type": "error", "status": "potatoes"}`
	_, err := cs.cli.SysInfo()
	c.Check(err, ErrorMatches, `.*server error: "potatoes"`)
}

func (cs *clientSuite) TestClientReportsOpErrorStr(c *C) {
	cs.rsp = `{
		"result": {},
		"status": "Bad Request",
		"status-code": 400,
		"type": "error"
	}`
	_, err := cs.cli.SysInfo()
	c.Check(err, ErrorMatches, `.*server error: "Bad Request"`)
}

func (cs *clientSuite) TestClientReportsBadType(c *C) {
	cs.rsp = `{"type": "what"}`
	_, err := cs.cli.SysInfo()
	c.Check(err, ErrorMatches, `.*expected sync response, got "what"`)
}

func (cs *clientSuite) TestClientReportsOuterJSONError(c *C) {
	cs.rsp = "this isn't really json is it"
	_, err := cs.cli.SysInfo()
	c.Check(err, ErrorMatches, `.*invalid character .*`)
}

func (cs *clientSuite) TestClientReportsInnerJSONError(c *C) {
	cs.rsp = `{"type": "sync", "result": "this isn't really json is it"}`
	_, err := cs.cli.SysInfo()
	c.Check(err, ErrorMatches, `.*cannot unmarshal.*`)
}

func (cs *clientSuite) TestClientAsync(c *C) {
	cs.rsp = `{"type":"async", "status-code": 202, "change": "42"}`
	changeId, err := cs.cli.FakeAsyncRequest()
	c.Assert(err, IsNil)
	c.Assert(changeId, Equals, "42")
}

func (cs *clientSuite) TestClientMaintenance(c *C) {
	cs.rsp = `{"type":"sync", "result":{"series":"42"}, "maintenance": {"kind": "system-restart", "message": "system is restarting"}}`
	_, err := cs.cli.SysInfo()
	c.Assert(err, IsNil)
	c.Check(cs.cli.Maintenance().(*client.Error), DeepEquals, &client.Error{
		Kind:    client.ErrorKindSystemRestart,
		Message: "system is restarting",
	})

	cs.rsp = `{"type":"sync", "result":{"series":"42"}}`
	_, err = cs.cli.SysInfo()
	c.Assert(err, IsNil)
	c.Check(cs.cli.Maintenance(), Equals, error(nil))
}

func (cs *clientSuite) TestClientAsyncOpMaintenance(c *C) {
	cs.rsp = `{"type":"async", "status-code": 202, "change": "42", "maintenance": {"kind": "system-restart", "message": "system is restarting"}}`
	_, err := cs.cli.FakeAsyncRequest()
	c.Assert(err, IsNil)
	c.Check(cs.cli.Maintenance().(*client.Error), DeepEquals, &client.Error{
		Kind:    client.ErrorKindSystemRestart,
		Message: "system is restarting",
	})

	cs.rsp = `{"type":"async", "status-code": 202, "change": "42"}`
	_, err = cs.cli.FakeAsyncRequest()
	c.Assert(err, IsNil)
	c.Check(cs.cli.Maintenance(), Equals, error(nil))
}

func (cs *clientSuite) TestParseError(c *C) {
	resp := &http.Response{
		Status: "404 Not Found",
	}
	err := client.ParseErrorInTest(resp)
	c.Check(err, ErrorMatches, `server error: "404 Not Found"`)

	h := http.Header{}
	h.Add("Content-Type", "application/json")
	resp = &http.Response{
		Status: "400 Bad Request",
		Header: h,
		Body: io.NopCloser(strings.NewReader(`{
			"status-code": 400,
			"type": "error",
			"result": {
				"message": "invalid"
			}
		}`)),
	}
	err = client.ParseErrorInTest(resp)
	c.Check(err, ErrorMatches, "invalid")

	resp = &http.Response{
		Status: "400 Bad Request",
		Header: h,
		Body:   io.NopCloser(strings.NewReader("{}")),
	}
	err = client.ParseErrorInTest(resp)
	c.Check(err, ErrorMatches, `server error: "400 Bad Request"`)
}

func (cs *clientSuite) TestUserAgent(c *C) {
	cli, err := client.New(&client.Config{UserAgent: "some-agent/9.87"})
	c.Assert(err, IsNil)
	cli.SetDoer(cs)

	resp, err := cli.Requester().Do(context.Background(), &client.RequestOptions{
		Type:   client.RawRequest,
		Method: "GET",
		Path:   "/",
	})
	c.Assert(err, IsNil)
	var v string
	err = resp.DecodeResult(&v)
	c.Assert(err, NotNil)
	c.Check(cs.req.Header.Get("User-Agent"), Equals, "some-agent/9.87")
}

func (cs *clientSuite) TestContentType(c *C) {
	cli, err := client.New(&client.Config{})
	c.Assert(err, IsNil)
	cli.SetDoer(cs)

	resp, err := cli.Requester().Do(context.Background(), &client.RequestOptions{
		Type:   client.RawRequest,
		Method: "GET",
		Path:   "/",
	})
	c.Assert(err, IsNil)
	var v string
	err = resp.DecodeResult(&v)
	c.Assert(err, NotNil)
	c.Check(cs.req.Header.Get("Content-Type"), Equals, "application/json")
}

func (cs *clientSuite) TestClientJSONError(c *C) {
	cs.rsp = `some non-json error message`
	_, err := cs.cli.SysInfo()
	c.Assert(err, ErrorMatches, `cannot obtain system details: cannot decode "some non-json error message": invalid char.*`)
}

func (cs *clientSuite) TestDebugPost(c *C) {
	cs.rsp = `{"type": "sync", "result":["res1","res2"]}`

	var result []string
	err := cs.cli.DebugPost("do-something", []string{"param1", "param2"}, &result)
	c.Check(err, IsNil)
	c.Check(result, DeepEquals, []string{"res1", "res2"})
	c.Check(cs.reqs, HasLen, 1)
	c.Check(cs.reqs[0].Method, Equals, "POST")
	c.Check(cs.reqs[0].URL.Path, Equals, "/v1/debug")
	data, err := io.ReadAll(cs.reqs[0].Body)
	c.Assert(err, IsNil)
	c.Check(string(data), DeepEquals, `{"action":"do-something","params":["param1","param2"]}`)
}

func (cs *clientSuite) TestDebugGet(c *C) {
	cs.rsp = `{"type": "sync", "result":["res1","res2"]}`

	var result []string
	err := cs.cli.DebugGet("do-something", &result, map[string]string{"foo": "bar"})
	c.Check(err, IsNil)
	c.Check(result, DeepEquals, []string{"res1", "res2"})
	c.Check(cs.reqs, HasLen, 1)
	c.Check(cs.reqs[0].Method, Equals, "GET")
	c.Check(cs.reqs[0].URL.Path, Equals, "/v1/debug")
	c.Check(cs.reqs[0].URL.Query(), DeepEquals, url.Values{"action": []string{"do-something"}, "foo": []string{"bar"}})
}

func (cs *clientSuite) TestNonExistentSocketErrors(c *C) {
	cli, err := client.New(&client.Config{Socket: "/tmp/not-the-droids-you-are-looking-for"})
	c.Check(err, IsNil)

	_, err = cli.SysInfo()
	c.Check(err, NotNil)
	var notFoundErr *client.SocketNotFoundError
	c.Check(errors.As(err, &notFoundErr), Equals, true)

	c.Check(notFoundErr.Path, Equals, "/tmp/not-the-droids-you-are-looking-for")
	c.Check(notFoundErr.Err, NotNil)
}

func (cs *clientSuite) TestLatestWarningTime(c *C) {
	cs.rsp = `{
		"result": {
			"version": "1.15.0",
			"boot-id": "BOOTID"
		},
		"status": "OK",
		"status-code": 200,
		"type": "sync",
		"latest-warning": "2018-09-19T12:44:19.680362867Z"
	}`

	info, err := cs.cli.SysInfo()
	c.Assert(err, IsNil)
	c.Check(info, DeepEquals, &client.SysInfo{
		Version: "1.15.0",
		BootID:  "BOOTID",
	})
	c.Check(cs.req.Method, Equals, "GET")
	c.Check(cs.req.URL.Path, Equals, "/v1/system-info")

	// this could be done at the end of any sync method
	latest := cs.cli.LatestWarningTime()
	c.Check(latest, Equals, time.Date(2018, 9, 19, 12, 44, 19, 680362867, time.UTC))
}

func (cs *clientSuite) TestClientIntegrationUnixSocket(c *C) {
	testUsername := "foo"
	testPassword := "bar"
	listener, err := net.Listen("unix", cs.socketPath)
	if err != nil {
		c.Fatalf("unable to listen on %q: %v", cs.socketPath, err)
	}
	defer listener.Close()

	handler := func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.Path, Equals, "/v1/system-info")
		c.Check(r.URL.RawQuery, Equals, "")
		// Basic Auth
		u, p, ok := r.BasicAuth()
		c.Check(ok, Equals, true)
		c.Check(u, Equals, testUsername)
		c.Check(p, Equals, testPassword)

		fmt.Fprintln(w, `{"type":"sync", "result":{"version":"1"}}`)
	}

	srv := &httptest.Server{
		Listener: listener,
		Config:   &http.Server{Handler: http.HandlerFunc(handler)},
	}
	srv.Start()
	defer srv.Close()

	cli, err := client.New(&client.Config{
		Socket:        cs.socketPath,
		BasicUsername: testUsername,
		BasicPassword: testPassword,
	})
	c.Assert(err, IsNil)
	si, err := cli.SysInfo()
	c.Check(err, IsNil)
	c.Check(si.Version, Equals, "1")
}

func (cs *clientSuite) TestClientIntegrationHTTP(c *C) {
	testUsername := "foo"
	testPassword := "bar"
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		c.Assert(err, IsNil)
	}
	defer listener.Close()
	// Get the allocated port.
	testPort := listener.Addr().(*net.TCPAddr).Port

	handler := func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.Path, Equals, "/v1/system-info")
		c.Check(r.URL.RawQuery, Equals, "")
		// Basic Auth
		u, p, ok := r.BasicAuth()
		c.Check(ok, Equals, true)
		c.Check(u, Equals, testUsername)
		c.Check(p, Equals, testPassword)

		fmt.Fprintln(w, `{"type":"sync", "result":{"version":"1"}}`)
	}

	srv := &httptest.Server{
		Listener: listener,
		Config:   &http.Server{Handler: http.HandlerFunc(handler)},
	}
	srv.Start()
	defer srv.Close()

	cli, err := client.New(&client.Config{
		BaseURL:       fmt.Sprintf("http://localhost:%d", testPort),
		BasicUsername: testUsername,
		BasicPassword: testPassword,
	})
	c.Assert(err, IsNil)
	si, err := cli.SysInfo()
	c.Check(err, IsNil)
	c.Check(si.Version, Equals, "1")
}
