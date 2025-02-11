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
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/state"
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
		err = os.WriteFile(filepath.Join(pebbleDir, "layers", "001-base.yaml"), []byte(layerYAML), 0644)
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
	var body map[string]any
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, IsNil)
	c.Check(body["result"], DeepEquals, []any{
		map[string]any{"startup": "enabled", "name": "test1", "current": "inactive"},
		map[string]any{"startup": "disabled", "name": "test2", "current": "inactive"},
		map[string]any{"startup": "disabled", "name": "test3", "current": "inactive"},
		map[string]any{"startup": "disabled", "name": "test4", "current": "inactive"},
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
	c.Assert(tasks[0].Summary(), Equals, `Stop service "test1"`)
	c.Assert(tasks[1].Summary(), Equals, `Stop service "test3"`)
	c.Assert(tasks[2].Summary(), Equals, `Start service "test1"`)
	c.Assert(tasks[3].Summary(), Equals, `Start service "test2"`)
	c.Assert(tasks[4].Summary(), Equals, `Start service "test3"`)
}

func (s *apiSuite) TestServicesReplan(c *C) {
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

	payload := bytes.NewBufferString(`{"action": "replan"}`)

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
	c.Check(chg, NotNil)
	c.Check(chg.Kind(), Equals, "replan")
	c.Check(chg.Summary(), Equals, `Replan service "test1" and 1 more`)
	tasks := chg.Tasks()
	c.Check(tasks, HasLen, 2)
	c.Check(tasks[0].Summary(), Equals, `Start service "test1"`)
	c.Check(tasks[1].Summary(), Equals, `Start service "test2"`)
}

func (s *apiSuite) TestServicesReplanNoServices(c *C) {
	// Setup
	writeTestLayer(s.pebbleDir, `
services:
    test:
        override: replace
        command: sleep 300
`)
	d := s.daemon(c)
	st := d.overlord.State()
	restore := FakeStateEnsureBefore(func(st *state.State, d time.Duration) {})
	defer restore()

	// Execute
	req, err := http.NewRequest("POST", "/v1/services", strings.NewReader(`{"action": "replan"}`))
	c.Assert(err, IsNil)
	rsp := v1PostServices(apiCmd("/v1/services"), req, nil).(*resp)
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
	c.Check(chg, NotNil)
	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.Kind(), Equals, "replan")
	c.Check(chg.Summary(), Equals, "Replan - no services")
	tasks := chg.Tasks()
	c.Check(tasks, HasLen, 0)
}

// Regression test for 3-lock deadlock issue described in
// https://github.com/canonical/pebble/issues/314
func (s *apiSuite) TestDeadlock(c *C) {
	// Set up
	writeTestLayer(s.pebbleDir, `
services:
    test:
        override: replace
        command: sleep 10
`)
	daemon, err := New(&Options{
		Dir:        s.pebbleDir,
		SocketPath: s.pebbleDir + ".pebble.socket",
	})
	c.Assert(err, IsNil)
	err = daemon.Init()
	c.Assert(err, IsNil)
	err = daemon.Start()
	c.Assert(err, IsNil)

	// To try to reproduce the deadlock, call these endpoints in a loop:
	// - GET /v1/services
	// - POST /v1/services with action=start
	// - POST /v1/services with action=stop

	getServices := func(ctx context.Context) {
		req, err := http.NewRequestWithContext(ctx, "GET", "/v1/services", nil)
		c.Assert(err, IsNil)
		rsp := v1GetServices(apiCmd("/v1/services"), req, nil).(*resp)
		rec := httptest.NewRecorder()
		rsp.ServeHTTP(rec, req)
		if rec.Code != 200 {
			panic(fmt.Sprintf("expected 200, got %d", rec.Code))
		}
	}

	serviceAction := func(ctx context.Context, action string) {
		body := `{"action": "` + action + `", "services": ["test"]}`
		req, err := http.NewRequestWithContext(ctx, "POST", "/v1/services", strings.NewReader(body))
		c.Assert(err, IsNil)
		rsp := v1PostServices(apiCmd("/v1/services"), req, nil).(*resp)
		rec := httptest.NewRecorder()
		rsp.ServeHTTP(rec, req)
		if rec.Code != 202 {
			panic(fmt.Sprintf("expected 202, got %d", rec.Code))
		}
	}

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		for ctx.Err() == nil {
			getServices(ctx)
			time.Sleep(time.Duration(rand.Intn(50)) * time.Millisecond)
		}
	}()

	go func() {
		for ctx.Err() == nil {
			serviceAction(ctx, "start")
			time.Sleep(time.Duration(rand.Intn(50)) * time.Millisecond)
		}
	}()

	go func() {
		for ctx.Err() == nil {
			serviceAction(ctx, "stop")
			time.Sleep(time.Duration(rand.Intn(50)) * time.Millisecond)
		}
	}()

	// Wait some time for deadlock to happen (when the bug was present, it
	// normally happened in well under a second).
	time.Sleep(time.Second)
	cancel()

	// Try to hit GET /v1/services once more; if it times out -- deadlock!
	done := make(chan struct{})
	go func() {
		getServices(context.Background())
		done <- struct{}{}
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		c.Fatal("timed out waiting for final request -- deadlock!")
	}

	// Otherwise wait for all changes to be done, then clean up (stop the daemon).
	var readyChans []<-chan struct{}
	daemon.state.Lock()
	for _, change := range daemon.state.Changes() {
		readyChans = append(readyChans, change.Ready())
	}
	daemon.state.Unlock()
	for _, ch := range readyChans {
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			c.Fatal("timed out waiting for ready channel")
		}
	}
	err = daemon.Stop(nil)
	c.Assert(err, IsNil)
}
