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
	"net/http"
	"net/http/httptest"
	"time"

	"gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/state"
)

func setupChanges(st *state.State) []string {
	chg1 := st.NewChange("install", "install...")
	chg1.Set("service-names", []string{"funky-service-name"})
	t1 := st.NewTask("download", "1...")
	t2 := st.NewTask("activate", "2...")
	chg1.AddAll(state.NewTaskSet(t1, t2))
	t1.Logf("l11")
	t1.Logf("l12")
	chg2 := st.NewChange("remove", "remove..")
	t3 := st.NewTask("unlink", "1...")
	chg2.AddTask(t3)
	t3.SetStatus(state.ErrorStatus)
	t3.Errorf("rm failed")

	return []string{chg1.ID(), chg2.ID(), t1.ID(), t2.ID(), t3.ID()}
}

func (s *apiSuite) TestStateChangesDefaultToInProgress(c *check.C) {
	restore := state.FakeTime(time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC))
	defer restore()

	// Setup
	d := s.daemon(c)
	st := d.overlord.State()
	st.Lock()
	setupChanges(st)
	st.Unlock()

	stateChangesCmd := apiCmd("/v1/changes")

	// Execute
	req, err := http.NewRequest("GET", "/v1/changes", nil)
	c.Assert(err, check.IsNil)
	rsp := v1GetChanges(stateChangesCmd, req, nil).(*resp)

	// Verify
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.HasLen, 1)

	res, err := rsp.MarshalJSON()
	c.Assert(err, check.IsNil)

	c.Check(string(res), check.Matches, `.*{"id":"\w+","kind":"install","summary":"install...","status":"Do","tasks":\[{"id":"\w+","kind":"download","summary":"1...","status":"Do","log":\["2016-04-21T01:02:03Z INFO l11","2016-04-21T01:02:03Z INFO l12"],"progress":{"label":"","done":0,"total":1},"spawn-time":"2016-04-21T01:02:03Z"}.*`)
}

func (s *apiSuite) TestStateChangesInProgress(c *check.C) {
	restore := state.FakeTime(time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC))
	defer restore()

	// Setup
	d := s.daemon(c)
	st := d.overlord.State()
	st.Lock()
	setupChanges(st)
	st.Unlock()

	stateChangesCmd := apiCmd("/v1/changes")

	// Execute
	req, err := http.NewRequest("GET", "/v1/changes?select=in-progress", nil)
	c.Assert(err, check.IsNil)
	rsp := v1GetChanges(stateChangesCmd, req, nil).(*resp)

	// Verify
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.HasLen, 1)

	res, err := rsp.MarshalJSON()
	c.Assert(err, check.IsNil)

	c.Check(string(res), check.Matches, `.*{"id":"\w+","kind":"install","summary":"install...","status":"Do","tasks":\[{"id":"\w+","kind":"download","summary":"1...","status":"Do","log":\["2016-04-21T01:02:03Z INFO l11","2016-04-21T01:02:03Z INFO l12"],"progress":{"label":"","done":0,"total":1},"spawn-time":"2016-04-21T01:02:03Z"}.*],"ready":false,"spawn-time":"2016-04-21T01:02:03Z"}.*`)
}

func (s *apiSuite) TestStateChangesAll(c *check.C) {
	restore := state.FakeTime(time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC))
	defer restore()

	// Setup
	d := s.daemon(c)
	st := d.overlord.State()
	st.Lock()
	setupChanges(st)
	st.Unlock()

	stateChangesCmd := apiCmd("/v1/changes")

	// Execute
	req, err := http.NewRequest("GET", "/v1/changes?select=all", nil)
	c.Assert(err, check.IsNil)
	rsp := v1GetChanges(stateChangesCmd, req, nil).(*resp)

	// Verify
	c.Check(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.HasLen, 2)

	res, err := rsp.MarshalJSON()
	c.Assert(err, check.IsNil)

	c.Check(string(res), check.Matches, `.*{"id":"\w+","kind":"install","summary":"install...","status":"Do","tasks":\[{"id":"\w+","kind":"download","summary":"1...","status":"Do","log":\["2016-04-21T01:02:03Z INFO l11","2016-04-21T01:02:03Z INFO l12"],"progress":{"label":"","done":0,"total":1},"spawn-time":"2016-04-21T01:02:03Z"}.*],"ready":false,"spawn-time":"2016-04-21T01:02:03Z"}.*`)
	c.Check(string(res), check.Matches, `.*{"id":"\w+","kind":"remove","summary":"remove..","status":"Error","tasks":\[{"id":"\w+","kind":"unlink","summary":"1...","status":"Error","log":\["2016-04-21T01:02:03Z ERROR rm failed"],"progress":{"label":"","done":1,"total":1},"spawn-time":"2016-04-21T01:02:03Z","ready-time":"2016-04-21T01:02:03Z"}.*],"ready":true,"err":"[^"]+".*`)
}

func (s *apiSuite) TestStateChangesReady(c *check.C) {
	restore := state.FakeTime(time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC))
	defer restore()

	// Setup
	d := s.daemon(c)
	st := d.overlord.State()
	st.Lock()
	setupChanges(st)
	st.Unlock()

	stateChangesCmd := apiCmd("/v1/changes")

	// Execute
	req, err := http.NewRequest("GET", "/v1/changes?select=ready", nil)
	c.Assert(err, check.IsNil)
	rsp := v1GetChanges(stateChangesCmd, req, nil).(*resp)

	// Verify
	c.Check(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.HasLen, 1)

	res, err := rsp.MarshalJSON()
	c.Assert(err, check.IsNil)

	c.Check(string(res), check.Matches, `.*{"id":"\w+","kind":"remove","summary":"remove..","status":"Error","tasks":\[{"id":"\w+","kind":"unlink","summary":"1...","status":"Error","log":\["2016-04-21T01:02:03Z ERROR rm failed"],"progress":{"label":"","done":1,"total":1},"spawn-time":"2016-04-21T01:02:03Z","ready-time":"2016-04-21T01:02:03Z"}.*],"ready":true,"err":"[^"]+".*`)
}

func (s *apiSuite) TestStateChangesForServiceName(c *check.C) {
	restore := state.FakeTime(time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC))
	defer restore()

	// Setup
	d := s.daemon(c)
	st := d.overlord.State()
	st.Lock()
	setupChanges(st)
	st.Unlock()

	stateChangesCmd := apiCmd("/v1/changes")

	// Execute
	req, err := http.NewRequest("GET", "/v1/changes?for=funky-service-name&select=all", nil)
	c.Assert(err, check.IsNil)
	rsp := v1GetChanges(stateChangesCmd, req, nil).(*resp)

	// Verify
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.FitsTypeOf, []*changeInfo(nil))

	res := rsp.Result.([]*changeInfo)
	c.Assert(res, check.HasLen, 1)
	c.Check(res[0].Kind, check.Equals, `install`)

	_, err = rsp.MarshalJSON()
	c.Assert(err, check.IsNil)
}

func (s *apiSuite) TestStateChange(c *check.C) {
	restore := state.FakeTime(time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC))
	defer restore()

	// Setup
	d := s.daemon(c)
	st := d.overlord.State()
	st.Lock()
	ids := setupChanges(st)
	chg := st.Change(ids[0])
	chg.Set("api-data", map[string]int{"n": 42})
	task := chg.Tasks()[0]
	task.Set("api-data", map[string]string{"foo": "bar"})
	st.Unlock()
	s.vars = map[string]string{"id": ids[0]}

	stateChangeCmd := apiCmd("/v1/changes/{id}")

	// Execute
	req, err := http.NewRequest("POST", "/v1/change/"+ids[0], nil)
	c.Assert(err, check.IsNil)
	rsp := v1GetChange(stateChangeCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)

	// Verify
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.NotNil)

	var body map[string]any
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body["result"], check.DeepEquals, map[string]any{
		"id":         ids[0],
		"kind":       "install",
		"summary":    "install...",
		"status":     "Do",
		"ready":      false,
		"spawn-time": "2016-04-21T01:02:03Z",
		"tasks": []any{
			map[string]any{
				"id":         ids[2],
				"kind":       "download",
				"summary":    "1...",
				"status":     "Do",
				"log":        []any{"2016-04-21T01:02:03Z INFO l11", "2016-04-21T01:02:03Z INFO l12"},
				"progress":   map[string]any{"label": "", "done": 0., "total": 1.},
				"spawn-time": "2016-04-21T01:02:03Z",
				"data": map[string]any{
					"foo": "bar",
				},
			},
			map[string]any{
				"id":         ids[3],
				"kind":       "activate",
				"summary":    "2...",
				"status":     "Do",
				"progress":   map[string]any{"label": "", "done": 0., "total": 1.},
				"spawn-time": "2016-04-21T01:02:03Z",
			},
		},
		"data": map[string]any{
			"n": float64(42),
		},
	})
}

func (s *apiSuite) TestStateChangeAbort(c *check.C) {
	restore := state.FakeTime(time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC))
	defer restore()

	soon := 0
	restore = FakeStateEnsureBefore(func(st *state.State, d time.Duration) {
		soon++
	})
	defer restore()

	// Setup
	d := s.daemon(c)
	st := d.overlord.State()
	st.Lock()
	ids := setupChanges(st)
	st.Unlock()
	s.vars = map[string]string{"id": ids[0]}

	buf := bytes.NewBufferString(`{"action": "abort"}`)

	stateChangeCmd := apiCmd("/v1/changes/{id}")

	// Execute
	req, err := http.NewRequest("POST", "/v1/changes/"+ids[0], buf)
	c.Assert(err, check.IsNil)
	rsp := v1PostChange(stateChangeCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)

	// Ensure scheduled
	c.Check(soon, check.Equals, 1)

	// Verify
	c.Check(rec.Code, check.Equals, 200)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.NotNil)

	var body map[string]any
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body["result"], check.DeepEquals, map[string]any{
		"id":         ids[0],
		"kind":       "install",
		"summary":    "install...",
		"status":     "Hold",
		"ready":      true,
		"spawn-time": "2016-04-21T01:02:03Z",
		"ready-time": "2016-04-21T01:02:03Z",
		"tasks": []any{
			map[string]any{
				"id":         ids[2],
				"kind":       "download",
				"summary":    "1...",
				"status":     "Hold",
				"log":        []any{"2016-04-21T01:02:03Z INFO l11", "2016-04-21T01:02:03Z INFO l12"},
				"progress":   map[string]any{"label": "", "done": 1., "total": 1.},
				"spawn-time": "2016-04-21T01:02:03Z",
				"ready-time": "2016-04-21T01:02:03Z",
			},
			map[string]any{
				"id":         ids[3],
				"kind":       "activate",
				"summary":    "2...",
				"status":     "Hold",
				"progress":   map[string]any{"label": "", "done": 1., "total": 1.},
				"spawn-time": "2016-04-21T01:02:03Z",
				"ready-time": "2016-04-21T01:02:03Z",
			},
		},
	})
}

func (s *apiSuite) TestStateChangeAbortIsReady(c *check.C) {
	restore := state.FakeTime(time.Date(2016, 04, 21, 1, 2, 3, 0, time.UTC))
	defer restore()

	// Setup
	d := s.daemon(c)
	st := d.overlord.State()
	st.Lock()
	ids := setupChanges(st)
	st.Change(ids[0]).SetStatus(state.DoneStatus)
	st.Unlock()
	s.vars = map[string]string{"id": ids[0]}

	buf := bytes.NewBufferString(`{"action": "abort"}`)

	stateChangeCmd := apiCmd("/v1/changes/{id}")

	// Execute
	req, err := http.NewRequest("POST", "/v1/changes/"+ids[0], buf)
	c.Assert(err, check.IsNil)
	rsp := v1PostChange(stateChangeCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)

	// Verify
	c.Check(rec.Code, check.Equals, 400)
	c.Check(rsp.Status, check.Equals, 400)
	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result, check.NotNil)

	var body map[string]any
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body["result"], check.DeepEquals, map[string]any{
		"message": fmt.Sprintf("cannot abort change %s with nothing pending", ids[0]),
	})
}

func (s *apiSuite) TestWaitChangeNotFound(c *check.C) {
	s.daemon(c)
	req, err := http.NewRequest("GET", "/v1/changes/x/wait", nil)
	c.Assert(err, check.IsNil)
	rsp := v1GetChangeWait(apiCmd("/v1/changes/{id}/wait"), req, nil).(*resp)
	c.Check(rsp.Status, check.Equals, 404)
}

func (s *apiSuite) TestWaitChangeInvalidTimeout(c *check.C) {
	rec, rsp, _ := s.testWaitChange(context.Background(), c, "?timeout=BAD", nil)
	c.Check(rec.Code, check.Equals, 400)
	c.Check(rsp.Status, check.Equals, 400)
	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	result := rsp.Result.(*errorResult)
	c.Check(result.Message, check.Matches, "invalid timeout.*")
}

func (s *apiSuite) TestWaitChangeSuccess(c *check.C) {
	rec, rsp, changeID := s.testWaitChange(context.Background(), c, "", func(st *state.State, change *state.Change) {
		// Mark change ready after a short interval
		time.Sleep(10 * time.Millisecond)
		st.Lock()
		change.SetStatus(state.DoneStatus)
		st.Unlock()
	})

	c.Check(rec.Code, check.Equals, 200)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.NotNil)

	var body map[string]any
	err := json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	result := body["result"].(map[string]any)
	c.Check(result["id"].(string), check.Equals, changeID)
	c.Check(result["kind"].(string), check.Equals, "exec")
	c.Check(result["ready"].(bool), check.Equals, true)
}

func (s *apiSuite) TestWaitChangeCancel(c *check.C) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	rec, rsp, _ := s.testWaitChange(ctx, c, "", nil)
	c.Check(rec.Code, check.Equals, 500)
	c.Check(rsp.Status, check.Equals, 500)
	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	result := rsp.Result.(*errorResult)
	c.Check(result.Message, check.Equals, "request cancelled")
}

func (s *apiSuite) TestWaitChangeTimeout(c *check.C) {
	rec, rsp, _ := s.testWaitChange(context.Background(), c, "?timeout=10ms", nil)
	c.Check(rec.Code, check.Equals, 504)
	c.Check(rsp.Status, check.Equals, 504)
	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	result := rsp.Result.(*errorResult)
	c.Check(result.Message, check.Matches, "timed out waiting for change .*")
}

func (s *apiSuite) TestWaitChangeTimeoutCancel(c *check.C) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	rec, rsp, _ := s.testWaitChange(ctx, c, "?timeout=20ms", nil)
	c.Check(rec.Code, check.Equals, 500)
	c.Check(rsp.Status, check.Equals, 500)
	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	result := rsp.Result.(*errorResult)
	c.Check(result.Message, check.Equals, "request cancelled")
}

func (s *apiSuite) testWaitChange(ctx context.Context, c *check.C, query string, markReady func(st *state.State, change *state.Change)) (*httptest.ResponseRecorder, *resp, string) {
	// Setup
	d := s.daemon(c)
	st := d.overlord.State()
	st.Lock()
	change := st.NewChange("exec", "Exec")
	task := st.NewTask("exec", "Exec")
	change.AddAll(state.NewTaskSet(task))
	st.Unlock()

	if markReady != nil {
		go markReady(st, change)
	}

	// Execute
	s.vars = map[string]string{"id": change.ID()}
	req, err := http.NewRequestWithContext(ctx, "GET", "/v1/changes/"+change.ID()+"/wait"+query, nil)
	c.Assert(err, check.IsNil)
	rsp := v1GetChangeWait(apiCmd("/v1/changes/{id}/wait"), req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	return rec, rsp, change.ID()
}
