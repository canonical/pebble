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

package daemon

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"time"

	"github.com/canonical/pebble/internal/overlord/state"

	"gopkg.in/check.v1"
)

type apiSuite struct {
	d *Daemon
}

var _ = check.Suite(&apiSuite{})

func (s *apiSuite) TearDownTest(c *check.C) {
	s.d = nil
}

func (s *apiSuite) daemon(c *check.C) *Daemon {
	if s.d != nil {
		panic("called daemon() twice")
	}
	d, err := New(c.MkDir()+"/state.json")
	c.Assert(err, check.IsNil)
	d.addRoutes()
	s.d = d
	return d
}

func apiCmd(path string) *Command {
	for _, cmd := range api {
		if cmd.Path == path {
			return cmd
		}
	}
	panic("no command with path " + path)
}

func (s *apiSuite) TestSysInfo(c *check.C) {
	sysInfoCmd := apiCmd("/v1/system-info")
	c.Assert(sysInfoCmd.GET, check.NotNil)
	c.Check(sysInfoCmd.PUT, check.IsNil)
	c.Check(sysInfoCmd.POST, check.IsNil)
	c.Check(sysInfoCmd.DELETE, check.IsNil)

	rec := httptest.NewRecorder()

	d := s.daemon(c)
	d.Version = "42b1"

	sysInfoCmd.GET(sysInfoCmd, nil, nil).ServeHTTP(rec, nil)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.HeaderMap.Get("Content-Type"), check.Equals, "application/json")

	expected := map[string]interface{}{
		"version": "42b1",
	}
	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), check.IsNil)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.DeepEquals, expected)
}

func (s *apiSuite) testWarnings(c *check.C, all bool, body io.Reader) (calls string, result interface{}) {
	s.daemon(c)

	oldOK := stateOkayWarnings
	oldAll := stateAllWarnings
	oldPending := statePendingWarnings
	stateOkayWarnings = func(*state.State, time.Time) int { calls += "ok"; return 0 }
	stateAllWarnings = func(*state.State) []*state.Warning { calls += "all"; return nil }
	statePendingWarnings = func(*state.State) ([]*state.Warning, time.Time) { calls += "show"; return nil, time.Time{} }
	defer func() {
		stateOkayWarnings = oldOK
		stateAllWarnings = oldAll
		statePendingWarnings = oldPending
	}()

	warningsCmd := apiCmd("/v1/warnings")

	method := "GET"
	f := warningsCmd.GET
	if body != nil {
		method = "POST"
		f = warningsCmd.POST
	}
	q := url.Values{}
	if all {
		q.Set("select", "all")
	}
	req, err := http.NewRequest(method, "/v2/warnings?"+q.Encode(), body)
	c.Assert(err, check.IsNil)

	rsp, ok := f(warningsCmd, req, nil).(*resp)
	c.Assert(ok, check.Equals, true)

	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.NotNil)
	return calls, rsp.Result
}

func (s *apiSuite) TestAllWarnings(c *check.C) {
	calls, result := s.testWarnings(c, true, nil)
	c.Check(calls, check.Equals, "all")
	c.Check(result, check.DeepEquals, []state.Warning{})
}

func (s *apiSuite) TestSomeWarnings(c *check.C) {
	calls, result := s.testWarnings(c, false, nil)
	c.Check(calls, check.Equals, "show")
	c.Check(result, check.DeepEquals, []state.Warning{})
}

func (s *apiSuite) TestAckWarnings(c *check.C) {
	calls, result := s.testWarnings(c, false, bytes.NewReader([]byte(`{"action": "okay", "timestamp": "2006-01-02T15:04:05Z"}`)))
	c.Check(calls, check.Equals, "ok")
	c.Check(result, check.DeepEquals, 0)
}
