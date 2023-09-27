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
	"errors"
	"fmt"
	"io/ioutil"
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
	cli        *client.Client
	req        *http.Request
	reqs       []*http.Request
	rsp        string
	rsps       []string
	err        error
	doCalls    int
	header     http.Header
	status     int
	tmpDir     string
	socketPath string
	restore    func()
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

func (cs *clientSuite) Do(req *http.Request) (*http.Response, error) {
	cs.req = req
	cs.reqs = append(cs.reqs, req)
	body := cs.rsp
	if cs.doCalls < len(cs.rsps) {
		body = cs.rsps[cs.doCalls]
	}
	rsp := &http.Response{
		Body:       ioutil.NopCloser(strings.NewReader(body)),
		Header:     cs.header,
		StatusCode: cs.status,
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
	err := cs.cli.Do("GET", "/", nil, nil, nil)
	c.Check(err, ErrorMatches, "cannot communicate with server: ouchie")
	if cs.doCalls < 2 {
		c.Fatalf("do did not retry")
	}
}

func (cs *clientSuite) TestClientWorks(c *C) {
	var v []int
	cs.rsp = `[1,2]`
	reqBody := ioutil.NopCloser(strings.NewReader(""))
	err := cs.cli.Do("GET", "/this", nil, reqBody, &v)
	c.Check(err, IsNil)
	c.Check(v, DeepEquals, []int{1, 2})
	c.Assert(cs.req, NotNil)
	c.Assert(cs.req.URL, NotNil)
	c.Check(cs.req.Method, Equals, "GET")
	c.Check(cs.req.Body, Equals, reqBody)
	c.Check(cs.req.URL.Path, Equals, "/this")
}

func (cs *clientSuite) TestClientDefaultsToNoAuthorization(c *C) {
	var v string
	_ = cs.cli.Do("GET", "/this", nil, nil, &v)
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

func (cs *clientSuite) TestClientIntegration(c *C) {
	l, err := net.Listen("unix", cs.socketPath)
	if err != nil {
		c.Fatalf("unable to listen on %q: %v", cs.socketPath, err)
	}

	f := func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.URL.Path, Equals, "/v1/system-info")
		c.Check(r.URL.RawQuery, Equals, "")

		fmt.Fprintln(w, `{"type":"sync", "result":{"version":"1"}}`)
	}

	srv := &httptest.Server{
		Listener: l,
		Config:   &http.Server{Handler: http.HandlerFunc(f)},
	}
	srv.Start()
	defer srv.Close()

	cli, err := client.New(&client.Config{Socket: cs.socketPath})
	c.Assert(err, IsNil)
	si, err := cli.SysInfo()
	c.Check(err, IsNil)
	c.Check(si.Version, Equals, "1")
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
		Body: ioutil.NopCloser(strings.NewReader(`{
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
		Body:   ioutil.NopCloser(strings.NewReader("{}")),
	}
	err = client.ParseErrorInTest(resp)
	c.Check(err, ErrorMatches, `server error: "400 Bad Request"`)
}

func (cs *clientSuite) TestUserAgent(c *C) {
	cli, err := client.New(&client.Config{UserAgent: "some-agent/9.87"})
	c.Assert(err, IsNil)
	cli.SetDoer(cs)

	var v string
	_ = cli.Do("GET", "/", nil, nil, &v)
	c.Assert(cs.req, NotNil)
	c.Check(cs.req.Header.Get("User-Agent"), Equals, "some-agent/9.87")
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
	data, err := ioutil.ReadAll(cs.reqs[0].Body)
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
