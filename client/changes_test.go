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
	"fmt"
	"io"
	"time"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/client"
)

func (cs *clientSuite) TestClientChange(c *tc.C) {
	cs.rsp = `{"type": "sync", "result": {
  "id":   "uno",
  "kind": "foo",
  "summary": "...",
  "status": "Do",
  "ready": false,
  "spawn-time": "2016-04-21T01:02:03Z",
  "ready-time": "2016-04-21T01:02:04Z",
  "tasks": [{"kind": "bar", "summary": "...", "status": "Do", "progress": {"done": 0, "total": 1}, "spawn-time": "2016-04-21T01:02:03Z", "ready-time": "2016-04-21T01:02:04Z"}]
}}`

	chg, err := cs.cli.Change("uno")
	c.Assert(err, tc.ErrorIsNil)
	c.Check(chg, tc.DeepEquals, &client.Change{
		ID:      "uno",
		Kind:    "foo",
		Summary: "...",
		Status:  "Do",
		Tasks: []*client.Task{{
			Kind:      "bar",
			Summary:   "...",
			Status:    "Do",
			Progress:  client.TaskProgress{Done: 0, Total: 1},
			SpawnTime: time.Date(2016, 4, 21, 1, 2, 3, 0, time.UTC),
			ReadyTime: time.Date(2016, 4, 21, 1, 2, 4, 0, time.UTC),
		}},

		SpawnTime: time.Date(2016, 4, 21, 1, 2, 3, 0, time.UTC),
		ReadyTime: time.Date(2016, 4, 21, 1, 2, 4, 0, time.UTC),
	})
}

func (cs *clientSuite) TestClientWaitChange(c *tc.C) {
	cs.rsp = `{"type": "sync", "result": {
  "id":   "uno",
  "kind": "foo",
  "summary": "...",
  "status": "Do",
  "ready": false,
  "spawn-time": "2016-04-21T01:02:03Z",
  "ready-time": "2016-04-21T01:02:04Z",
  "tasks": [{"kind": "bar", "summary": "...", "status": "Do", "progress": {"done": 0, "total": 1}, "spawn-time": "2016-04-21T01:02:03Z", "ready-time": "2016-04-21T01:02:04Z"}]
}}`

	chg, err := cs.cli.WaitChange("foo", nil)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(cs.req.URL.String(), tc.Equals, "http://localhost/v1/changes/foo/wait")
	c.Check(chg, tc.DeepEquals, &client.Change{
		ID:      "uno",
		Kind:    "foo",
		Summary: "...",
		Status:  "Do",
		Tasks: []*client.Task{{
			Kind:      "bar",
			Summary:   "...",
			Status:    "Do",
			Progress:  client.TaskProgress{Done: 0, Total: 1},
			SpawnTime: time.Date(2016, 4, 21, 1, 2, 3, 0, time.UTC),
			ReadyTime: time.Date(2016, 4, 21, 1, 2, 4, 0, time.UTC),
		}},
		SpawnTime: time.Date(2016, 4, 21, 1, 2, 3, 0, time.UTC),
		ReadyTime: time.Date(2016, 4, 21, 1, 2, 4, 0, time.UTC),
	})
}

func (cs *clientSuite) TestClientWaitChangeTimeout(c *tc.C) {
	cs.err = fmt.Errorf(`timed out waiting for change`)
	opts := &client.WaitChangeOptions{
		Timeout: 30 * time.Second,
	}
	_, err := cs.cli.WaitChange("bar", opts)
	c.Assert(cs.req.URL.String(), tc.Equals, "http://localhost/v1/changes/bar/wait?timeout=30s")
	c.Assert(err, tc.ErrorMatches, `.*timed out waiting for change.*`)
}

func (cs *clientSuite) TestClientChangeData(c *tc.C) {
	cs.rsp = `{"type": "sync", "result": {
  "id":   "uno",
  "kind": "foo",
  "summary": "...",
  "status": "Do",
  "ready": false,
  "data": {"n": 42}
}}`

	chg, err := cs.cli.Change("uno")
	c.Assert(err, tc.ErrorIsNil)
	var n int
	err = chg.Get("n", &n)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(n, tc.Equals, 42)

	err = chg.Get("missing", &n)
	c.Assert(err, tc.Equals, client.ErrNoData)
}

func (cs *clientSuite) TestClientChangeRestartingState(c *tc.C) {
	cs.rsp = `{"type": "sync", "result": {
  "id":   "uno",
  "kind": "foo",
  "summary": "...",
  "status": "Do",
  "ready": false
},
 "maintenance": {"kind": "system-restart", "message": "system is restarting"}
}`

	chg, err := cs.cli.Change("uno")
	c.Assert(err, tc.ErrorIsNil)
	c.Check(chg, tc.NotNil)
	c.Check(chg.ID, tc.Equals, "uno")
	c.Check(cs.cli.Maintenance(), tc.ErrorMatches, `system is restarting`)
}

func (cs *clientSuite) TestClientChangeError(c *tc.C) {
	cs.rsp = `{"type": "sync", "result": {
  "id":   "uno",
  "kind": "foo",
  "summary": "...",
  "status": "Error",
  "ready": true,
  "tasks": [{"kind": "bar", "summary": "...", "status": "Error", "progress": {"done": 1, "total": 1}, "log": ["ERROR: something broke"]}],
  "err": "error message"
}}`

	chg, err := cs.cli.Change("uno")
	c.Assert(err, tc.ErrorIsNil)
	c.Check(chg, tc.DeepEquals, &client.Change{
		ID:      "uno",
		Kind:    "foo",
		Summary: "...",
		Status:  "Error",
		Tasks: []*client.Task{{
			Kind:     "bar",
			Summary:  "...",
			Status:   "Error",
			Progress: client.TaskProgress{Done: 1, Total: 1},
			Log:      []string{"ERROR: something broke"},
		}},
		Err:   "error message",
		Ready: true,
	})
}

func (cs *clientSuite) TestClientChangesString(c *tc.C) {
	for k, v := range map[client.ChangeSelector]string{
		client.ChangesAll:        "all",
		client.ChangesReady:      "ready",
		client.ChangesInProgress: "in-progress",
	} {
		c.Check(k.String(), tc.Equals, v)
	}
}

func (cs *clientSuite) TestClientChanges(c *tc.C) {
	cs.rsp = `{"type": "sync", "result": [{
  "id":   "uno",
  "kind": "foo",
  "summary": "...",
  "status": "Do",
  "ready": false,
  "tasks": [{"kind": "bar", "summary": "...", "status": "Do", "progress": {"done": 0, "total": 1}}]
}]}`

	for _, i := range []*client.ChangesOptions{
		{Selector: client.ChangesAll},
		{Selector: client.ChangesReady},
		{Selector: client.ChangesInProgress},
		{ServiceName: "foo"},
		nil,
	} {
		chg, err := cs.cli.Changes(i)
		c.Assert(err, tc.ErrorIsNil)
		c.Check(chg, tc.DeepEquals, []*client.Change{{
			ID:      "uno",
			Kind:    "foo",
			Summary: "...",
			Status:  "Do",
			Tasks:   []*client.Task{{Kind: "bar", Summary: "...", Status: "Do", Progress: client.TaskProgress{Done: 0, Total: 1}}},
		}})
		if i == nil {
			c.Check(cs.req.URL.RawQuery, tc.Equals, "")
		} else {
			if i.Selector != 0 {
				c.Check(cs.req.URL.RawQuery, tc.Equals, "select="+i.Selector.String())
			} else {
				c.Check(cs.req.URL.RawQuery, tc.Equals, "for="+i.ServiceName)
			}
		}
	}

}

func (cs *clientSuite) TestClientChangesData(c *tc.C) {
	cs.rsp = `{"type": "sync", "result": [{
  "id":   "uno",
  "kind": "foo",
  "summary": "...",
  "status": "Do",
  "ready": false,
  "data": {"n": 42}
}]}`

	chgs, err := cs.cli.Changes(&client.ChangesOptions{Selector: client.ChangesAll})
	c.Assert(err, tc.ErrorIsNil)

	chg := chgs[0]
	var n int
	err = chg.Get("n", &n)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(n, tc.Equals, 42)

	err = chg.Get("missing", &n)
	c.Assert(err, tc.Equals, client.ErrNoData)
}

func (cs *clientSuite) TestClientAbort(c *tc.C) {
	cs.rsp = `{"type": "sync", "result": {
  "id":   "uno",
  "kind": "foo",
  "summary": "...",
  "status": "Hold",
  "ready": true,
  "spawn-time": "2016-04-21T01:02:03Z",
  "ready-time": "2016-04-21T01:02:04Z"
}}`

	chg, err := cs.cli.Abort("uno")
	c.Assert(err, tc.ErrorIsNil)
	c.Check(cs.req.Method, tc.Equals, "POST")
	c.Check(chg, tc.DeepEquals, &client.Change{
		ID:      "uno",
		Kind:    "foo",
		Summary: "...",
		Status:  "Hold",
		Ready:   true,

		SpawnTime: time.Date(2016, 4, 21, 1, 2, 3, 0, time.UTC),
		ReadyTime: time.Date(2016, 4, 21, 1, 2, 4, 0, time.UTC),
	})

	body, err := io.ReadAll(cs.req.Body)
	c.Assert(err, tc.ErrorIsNil)

	c.Assert(string(body), tc.Equals, "{\"action\":\"abort\"}\n")
}

func (cs *clientSuite) TestChangeInvalidID(c *tc.C) {
	_, err := cs.cli.Change("select * from users;")
	c.Assert(err, tc.ErrorMatches, "invalid change ID.*")
}

func (cs *clientSuite) TestAbortInvalidID(c *tc.C) {
	_, err := cs.cli.Abort("<foo>")
	c.Assert(err, tc.ErrorMatches, "invalid change ID.*")
}

func (cs *clientSuite) TestWaitChangeInvalidID(c *tc.C) {
	_, err := cs.cli.WaitChange("$bar", nil)
	c.Assert(err, tc.ErrorMatches, "invalid change ID.*")
}
