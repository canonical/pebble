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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"

	"gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/restart"
	"github.com/canonical/pebble/internals/reaper"
)

var _ = check.Suite(&apiSuite{})

type apiSuite struct {
	d *Daemon

	pebbleDir string

	vars map[string]string

	restoreMuxVars  func()
	overlordStarted bool
}

func (s *apiSuite) SetUpTest(c *check.C) {
	err := reaper.Start()
	if err != nil {
		c.Fatalf("cannot start reaper: %v", err)
	}

	s.restoreMuxVars = FakeMuxVars(s.muxVars)
	s.pebbleDir = c.MkDir()
}

func (s *apiSuite) TearDownTest(c *check.C) {
	if s.overlordStarted {
		s.d.Overlord().Stop()
		s.overlordStarted = false
	}
	s.d = nil
	s.pebbleDir = ""
	s.restoreMuxVars()

	err := reaper.Stop()
	if err != nil {
		c.Fatalf("cannot stop reaper: %v", err)
	}
}

func (s *apiSuite) muxVars(*http.Request) map[string]string {
	return s.vars
}

func (s *apiSuite) daemon(c *check.C) *Daemon {
	if s.d != nil {
		panic("called daemon() twice")
	}
	d, err := New(&Options{Dir: s.pebbleDir})
	c.Assert(err, check.IsNil)
	d.addRoutes()

	c.Assert(d.overlord.StartUp(), check.IsNil)

	s.d = d
	return d
}

func (s *apiSuite) startOverlord() {
	s.overlordStarted = true
	s.d.overlord.Loop()
}

func apiCmd(path string) *Command {
	for _, cmd := range API {
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

	rec := httptest.NewRecorder()

	d := s.daemon(c)
	d.Version = "42b1"
	state := d.overlord.State()
	state.Lock()
	_, err := restart.Manager(state, "ffffffff-ffff-ffff-ffff-ffffffffffff", nil)
	state.Unlock()
	c.Assert(err, check.IsNil)

	sysInfoCmd.GET(sysInfoCmd, nil, nil).ServeHTTP(rec, nil)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.Result().Header.Get("Content-Type"), check.Equals, "application/json")

	expected := map[string]any{
		"version": "42b1",
		"boot-id": "ffffffff-ffff-ffff-ffff-ffffffffffff",
	}
	var rsp resp
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), check.IsNil)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.DeepEquals, expected)
}

func fakeEnv(key, value string) (restore func()) {
	oldEnv, envWasSet := os.LookupEnv(key)
	err := os.Setenv(key, value)
	if err != nil {
		panic(err)
	}
	return func() {
		var err error
		if envWasSet {
			err = os.Setenv(key, oldEnv)
		} else {
			err = os.Unsetenv(key)
		}
		if err != nil {
			panic(err)
		}
	}
}
