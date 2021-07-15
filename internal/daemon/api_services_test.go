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
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/canonical/pebble/internal/overlord/state"

	. "gopkg.in/check.v1"
)

var servicesLayer = `
services:
    test1:
        override: replace
        command: /bin/sh -c "echo test1 >> %s; sleep 300"
        startup: enabled
        requires:
            - test2
        before:
            - test2

    test2:
        override: replace
        command: /bin/sh -c "echo test2 >> %s; sleep 300"

    test3:
        override: replace
        command: some-bad-command
        after:
            - test2

    test4:
        override: replace
        command: just-idling-here
`

func writeTestLayer(pebbleDir, layerYAML string) {
	err := os.Mkdir(filepath.Join(pebbleDir, "layers"), 0755)
	if err == nil {
		err = ioutil.WriteFile(filepath.Join(pebbleDir, "layers", "001-base.yaml"), []byte(layerYAML), 0644)
	}
	if err != nil {
		panic(err)
	}
}

func (s *apiSuite) TestServicesStart(c *C) {
	// Setup
	writeTestLayer(s.pebbleDir, servicesLayer)
	d := s.daemon(c)
	st := d.overlord.State()

	soon := 0
	restore := FakeStateEnsureBefore(func(st *state.State, d time.Duration) {
		soon++
	})
	defer restore()

	servicesCmd := apiCmd("/v1/services")

	payload := bytes.NewBufferString(`{"action": "start", "services": ["test3", "test1"]}`)

	// Execute
	req, err := http.NewRequest("POST", "/v1/services", payload)
	c.Assert(err, IsNil)
	rsp := v1PostServices(servicesCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)

	// Verify
	c.Check(rec.Code, Equals, 202)
	c.Check(rsp.Status, Equals, 202)
	c.Check(rsp.Type, Equals, ResponseTypeAsync)
	c.Check(rsp.Result, IsNil)

	st.Lock()
	defer st.Unlock()

	chg := st.Change(rsp.Change)
	c.Assert(chg, NotNil)
	c.Assert(chg.Summary(), Equals, `Start service "test3" and 2 more`)

	c.Check(chg.Kind(), Equals, "start")

	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 3)

	// In the proper order, with dependencies.
	c.Assert(tasks[0].Summary(), Equals, `Start service "test1"`)
	c.Assert(tasks[1].Summary(), Equals, `Start service "test2"`)
	c.Assert(tasks[2].Summary(), Equals, `Start service "test3"`)
}

func (s *apiSuite) TestServicesStop(c *C) {
	// Setup
	writeTestLayer(s.pebbleDir, servicesLayer)
	d := s.daemon(c)
	st := d.overlord.State()

	soon := 0
	restore := FakeStateEnsureBefore(func(st *state.State, d time.Duration) {
		soon++
	})
	defer restore()

	servicesCmd := apiCmd("/v1/services")

	payload := bytes.NewBufferString(`{"action": "stop", "services": ["test2", "test3"]}`)

	// Execute
	req, err := http.NewRequest("POST", "/v1/services", payload)
	c.Assert(err, IsNil)
	rsp := v1PostServices(servicesCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)

	// Verify
	c.Check(rec.Code, Equals, 202)
	c.Check(rsp.Status, Equals, 202)
	c.Check(rsp.Type, Equals, ResponseTypeAsync)
	c.Check(rsp.Result, IsNil)

	st.Lock()
	defer st.Unlock()

	chg := st.Change(rsp.Change)
	c.Assert(chg, NotNil)
	c.Assert(chg.Summary(), Equals, `Stop service "test2" and 2 more`)

	c.Check(chg.Kind(), Equals, "stop")

	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 3)

	// In the proper order, with dependencies.
	c.Assert(tasks[0].Summary(), Equals, `Stop service "test3"`)
	c.Assert(tasks[1].Summary(), Equals, `Stop service "test2"`)
	c.Assert(tasks[2].Summary(), Equals, `Stop service "test1"`)
}

func (s *apiSuite) TestServicesAutoStart(c *C) {
	// Setup
	writeTestLayer(s.pebbleDir, servicesLayer)
	d := s.daemon(c)
	st := d.overlord.State()

	soon := 0
	restore := FakeStateEnsureBefore(func(st *state.State, d time.Duration) {
		soon++
	})
	defer restore()

	servicesCmd := apiCmd("/v1/services")

	payload := bytes.NewBufferString(`{"action": "autostart"}`)

	// Execute
	req, err := http.NewRequest("POST", "/v1/services", payload)
	c.Assert(err, IsNil)
	rsp := v1PostServices(servicesCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)

	// Verify
	c.Check(rec.Code, Equals, 202)
	c.Check(rsp.Status, Equals, 202)
	c.Check(rsp.Type, Equals, ResponseTypeAsync)
	c.Check(rsp.Result, IsNil)

	st.Lock()
	defer st.Unlock()

	chg := st.Change(rsp.Change)
	c.Assert(chg, NotNil)

	c.Check(chg.Kind(), Equals, "autostart")

	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 2)
	c.Assert(tasks[0].Summary(), Equals, `Start service "test1"`)
	c.Assert(tasks[1].Summary(), Equals, `Start service "test2"`)
}

func (s *apiSuite) TestServicesGet(c *C) {
	// Setup
	writeTestLayer(s.pebbleDir, servicesLayer)
	s.daemon(c)

	// Execute
	req, err := http.NewRequest("GET", "/v1/services", nil)
	c.Assert(err, IsNil)
	rsp := v1GetServices(apiCmd("/v1/services"), req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)

	// Verify
	c.Check(rec.Code, Equals, 200)
	c.Check(rsp.Status, Equals, 200)
	c.Check(rsp.Type, Equals, ResponseTypeSync)
	c.Check(rsp.Result, NotNil)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, IsNil)
	c.Check(body["result"], DeepEquals, []interface{}{
		map[string]interface{}{"startup": "enabled", "name": "test1", "current": "inactive"},
		map[string]interface{}{"startup": "disabled", "name": "test2", "current": "inactive"},
		map[string]interface{}{"startup": "disabled", "name": "test3", "current": "inactive"},
		map[string]interface{}{"startup": "disabled", "name": "test4", "current": "inactive"},
	})
}

func (s *apiSuite) TestServicesRestart(c *C) {
	// Setup
	writeTestLayer(s.pebbleDir, servicesLayer)
	d := s.daemon(c)
	st := d.overlord.State()

	soon := 0
	restore := FakeStateEnsureBefore(func(st *state.State, d time.Duration) {
		soon++
	})
	defer restore()

	servicesCmd := apiCmd("/v1/services")

	payload := bytes.NewBufferString(`{"action": "restart", "services": ["test3", "test1"]}`)

	// Execute
	req, err := http.NewRequest("POST", "/v1/services", payload)
	c.Assert(err, IsNil)
	rsp := v1PostServices(servicesCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)

	// Verify
	c.Check(rec.Code, Equals, 202)
	c.Check(rsp.Status, Equals, 202)
	c.Check(rsp.Type, Equals, ResponseTypeAsync)
	c.Check(rsp.Result, IsNil)

	st.Lock()
	defer st.Unlock()

	chg := st.Change(rsp.Change)
	c.Assert(chg, NotNil)
	c.Assert(chg.Summary(), Equals, `Restart service "test3" and 2 more`)

	c.Check(chg.Kind(), Equals, "restart")

	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 5)

	// In the proper order, with dependencies.
	c.Assert(tasks[0].Summary(), Equals, `Stop service "test3"`)
	c.Assert(tasks[1].Summary(), Equals, `Stop service "test1"`)
	c.Assert(tasks[2].Summary(), Equals, `Start service "test1"`)
	c.Assert(tasks[3].Summary(), Equals, `Start service "test2"`)
	c.Assert(tasks[4].Summary(), Equals, `Start service "test3"`)
}
