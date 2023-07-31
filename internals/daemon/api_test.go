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
)

var _ = check.Suite(&apiSuite{})

type apiSuite struct {
	d *Daemon

	pebbleDir string

	vars map[string]string

	restoreMuxVars func()
}

func (s *apiSuite) SetUpTest(c *check.C) {
	s.restoreMuxVars = FakeMuxVars(s.muxVars)
	s.pebbleDir = c.MkDir()
}

func (s *apiSuite) TearDownTest(c *check.C) {
	s.d = nil
	s.pebbleDir = ""
	s.restoreMuxVars()
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
	s.d = d
	return d
}

func apiCmd(path string) *Command {
	for _, cmd := range Api {
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
	state := d.overlord.State()
	state.Lock()
	restart.Init(state, "ffffffff-ffff-ffff-ffff-ffffffffffff", nil)
	state.Unlock()

	sysInfoCmd.GET(sysInfoCmd, nil, nil).ServeHTTP(rec, nil)
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.HeaderMap.Get("Content-Type"), check.Equals, "application/json")

	expected := map[string]interface{}{
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
