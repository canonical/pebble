// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package cli_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/cli"
)

var fakeChangeJSON = `{"type": "sync", "result": {
  "id":   "uno",
  "kind": "foo",
  "summary": "...",
  "status": "Do",
  "ready": false,
  "spawn-time": "2016-04-21T01:02:03Z",
  "tasks": [{"kind": "bar", "summary": "some summary", "status": "Do", "progress": {"done": 0, "total": 1}, "spawn-time": "2016-04-21T01:02:03Z"}]
}}`

var fakeChangesJSON = `{"type": "sync", "result": [
  {
    "id":   "four",
    "kind": "install",
    "summary": "...",
    "status": "Do",
    "ready": false,
    "spawn-time": "2015-02-21T01:02:03Z",
    "ready-time": "2015-02-21T01:02:04Z",
    "tasks": [{"kind": "bar", "summary": "some summary", "status": "Do", "progress": {"done": 0, "total": 1}, "spawn-time": "2015-02-21T01:02:03Z", "ready-time": "2015-02-21T01:02:04Z"}]
  },
  {
    "id":   "one",
    "kind": "remove",
    "summary": "...",
    "status": "Do",
    "ready": false,
    "spawn-time": "2016-03-21T01:02:03Z",
    "ready-time": "2016-03-21T01:02:04Z",
    "tasks": [{"kind": "bar", "summary": "some summary", "status": "Do", "progress": {"done": 0, "total": 1}, "spawn-time": "2016-03-21T01:02:03Z", "ready-time": "2016-03-21T01:02:04Z"}]
  },
  {
    "id":   "two",
    "kind": "install",
    "summary": "...",
    "status": "Do",
    "ready": false,
    "spawn-time": "2016-04-21T01:02:03Z",
    "ready-time": "2016-04-21T01:02:04Z",
    "tasks": [{"kind": "bar", "summary": "some summary", "status": "Do", "progress": {"done": 0, "total": 1}, "spawn-time": "2016-04-21T01:02:03Z", "ready-time": "2016-04-21T01:02:04Z"}]
  },
  {
    "id":   "three",
    "kind": "install",
    "summary": "...",
    "status": "Do",
    "ready": false,
    "spawn-time": "2016-01-21T01:02:03Z",
    "tasks": [{"kind": "bar", "summary": "some summary", "status": "Do", "progress": {"done": 0, "total": 1}, "spawn-time": "2016-01-21T01:02:03Z", "ready-time": "2016-01-21T01:02:04Z"}]
  }
]}`

var fakeChangeInProgressJSON = `{"type": "sync", "result": {
  "id":   "uno",
  "kind": "foo",
  "summary": "...",
  "status": "Do",
  "ready": false,
  "spawn-time": "2016-04-21T01:02:03Z",
  "ready-time": "2016-04-21T01:02:04Z",
  "tasks": [{"kind": "bar", "summary": "some summary", "status": "Doing", "progress": {"done": 50, "total": 100}, "spawn-time": "2016-04-21T01:02:03Z", "ready-time": "2016-04-21T01:02:04Z", "log": ["a", "b", "c"]}]
}}`

func (s *PebbleSuite) TestChangesExtraArgs(c *tc.C) {
	rest, err := cli.ParserForTest().ParseArgs([]string{"changes", "extra", "args"})
	c.Assert(err, tc.Equals, cli.ErrExtraArgs)
	c.Check(rest, tc.HasLen, 1)
	c.Check(s.Stdout(), tc.Equals, "")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestChangesAllDigitsSuggestion(c *tc.C) {
	rest, err := cli.ParserForTest().ParseArgs([]string{"changes", "42"})
	c.Assert(err, tc.ErrorMatches, `'pebble changes' command expects a service name, try 'pebble tasks 42'`)
	c.Check(rest, tc.HasLen, 1)
	c.Check(s.Stdout(), tc.Equals, "")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestNoChanges(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/changes")
		c.Check(r.URL.Query(), tc.DeepEquals, url.Values{"for": {"svc1"}, "select": {"all"}})
		fmt.Fprintln(w, `{"type":"sync", "result": []}"`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"changes", "svc1"})
	c.Assert(err, tc.IsNil)
	c.Check(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, "")
	c.Check(s.Stderr(), tc.Equals, "no changes found\n")
}

func (s *PebbleSuite) TestGetChangesFails(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/changes")
		c.Check(r.URL.Query(), tc.DeepEquals, url.Values{"for": {"svc1"}, "select": {"all"}})
		fmt.Fprintln(w, `{"type":"error", "result": {"message": "could not foo"}}"`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"changes", "svc1"})
	c.Assert(err, tc.ErrorMatches, "could not foo")
	c.Check(rest, tc.HasLen, 1)
	c.Check(s.Stdout(), tc.Equals, "")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestChanges(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/changes")
		c.Check(r.URL.Query(), tc.DeepEquals, url.Values{"select": {"all"}})
		fmt.Fprintln(w, fakeChangesJSON)
	})

	// The [12] is to allow date 21 or 22, so the tests succeed on timezones
	// east of UTC and west of UTC (timeutil.Human uses time.Local).
	expectedChanges := `
(?ms)ID +Status +Spawn +Ready +Summary
four +Do +2015-02-21 +2015-02-2[12] +...
three +Do +2016-01-2[12] +- +...
one +Do +2016-03-21 +2016-03-2[12] +...
two +Do +2016-04-21 +2016-04-2[12] +...
`[1:]

	rest, err := cli.ParserForTest().ParseArgs([]string{"changes"})
	c.Assert(err, tc.IsNil)
	c.Check(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Matches, expectedChanges)
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestChangesJSON(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/changes")
		c.Check(r.URL.Query(), tc.DeepEquals, url.Values{"select": {"all"}})
		fmt.Fprintln(w, fakeChangesJSON)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"changes", "--format", "json"})
	c.Assert(err, tc.IsNil)
	c.Check(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, `{"changes":[{"id":"four","kind":"install","summary":"...","status":"Do","tasks":[{"id":"","kind":"bar","summary":"some summary","status":"Do","progress":{"label":"","done":0,"total":1},"spawn-time":"2015-02-21T01:02:03Z","ready-time":"2015-02-21T01:02:04Z"}],"ready":false,"spawn-time":"2015-02-21T01:02:03Z","ready-time":"2015-02-21T01:02:04Z"},{"id":"three","kind":"install","summary":"...","status":"Do","tasks":[{"id":"","kind":"bar","summary":"some summary","status":"Do","progress":{"label":"","done":0,"total":1},"spawn-time":"2016-01-21T01:02:03Z","ready-time":"2016-01-21T01:02:04Z"}],"ready":false,"spawn-time":"2016-01-21T01:02:03Z"},{"id":"one","kind":"remove","summary":"...","status":"Do","tasks":[{"id":"","kind":"bar","summary":"some summary","status":"Do","progress":{"label":"","done":0,"total":1},"spawn-time":"2016-03-21T01:02:03Z","ready-time":"2016-03-21T01:02:04Z"}],"ready":false,"spawn-time":"2016-03-21T01:02:03Z","ready-time":"2016-03-21T01:02:04Z"},{"id":"two","kind":"install","summary":"...","status":"Do","tasks":[{"id":"","kind":"bar","summary":"some summary","status":"Do","progress":{"label":"","done":0,"total":1},"spawn-time":"2016-04-21T01:02:03Z","ready-time":"2016-04-21T01:02:04Z"}],"ready":false,"spawn-time":"2016-04-21T01:02:03Z","ready-time":"2016-04-21T01:02:04Z"}]}`+"\n")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestChangesYAML(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/changes")
		c.Check(r.URL.Query(), tc.DeepEquals, url.Values{"select": {"all"}})
		fmt.Fprintln(w, fakeChangesJSON)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"changes", "--format", "yaml"})
	c.Assert(err, tc.IsNil)
	c.Check(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, `
changes:
    - id: four
      kind: install
      summary: '...'
      status: Do
      tasks:
        - id: ""
          kind: bar
          summary: some summary
          status: Do
          progress:
            label: ""
            done: 0
            total: 1
          spawn-time: 2015-02-21T01:02:03Z
          ready-time: 2015-02-21T01:02:04Z
      ready: false
      spawn-time: 2015-02-21T01:02:03Z
      ready-time: 2015-02-21T01:02:04Z
    - id: three
      kind: install
      summary: '...'
      status: Do
      tasks:
        - id: ""
          kind: bar
          summary: some summary
          status: Do
          progress:
            label: ""
            done: 0
            total: 1
          spawn-time: 2016-01-21T01:02:03Z
          ready-time: 2016-01-21T01:02:04Z
      ready: false
      spawn-time: 2016-01-21T01:02:03Z
    - id: one
      kind: remove
      summary: '...'
      status: Do
      tasks:
        - id: ""
          kind: bar
          summary: some summary
          status: Do
          progress:
            label: ""
            done: 0
            total: 1
          spawn-time: 2016-03-21T01:02:03Z
          ready-time: 2016-03-21T01:02:04Z
      ready: false
      spawn-time: 2016-03-21T01:02:03Z
      ready-time: 2016-03-21T01:02:04Z
    - id: two
      kind: install
      summary: '...'
      status: Do
      tasks:
        - id: ""
          kind: bar
          summary: some summary
          status: Do
          progress:
            label: ""
            done: 0
            total: 1
          spawn-time: 2016-04-21T01:02:03Z
          ready-time: 2016-04-21T01:02:04Z
      ready: false
      spawn-time: 2016-04-21T01:02:03Z
      ready-time: 2016-04-21T01:02:04Z
`[1:])
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestNoChangesJSON(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/changes")
		c.Check(r.URL.Query(), tc.DeepEquals, url.Values{"select": {"all"}})
		fmt.Fprintln(w, `{"type":"sync", "result": []}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"changes", "--format", "json"})
	c.Assert(err, tc.IsNil)
	c.Check(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, `{"changes":[]}`+"\n")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestNoChangesYAML(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/changes")
		c.Check(r.URL.Query(), tc.DeepEquals, url.Values{"select": {"all"}})
		fmt.Fprintln(w, `{"type":"sync", "result": []}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"changes", "--format", "yaml"})
	c.Assert(err, tc.IsNil)
	c.Check(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, "changes: []\n")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestChangesInvalidFormat(c *tc.C) {
	_, err := cli.ParserForTest().ParseArgs([]string{"changes", "--format", "foobar"})
	c.Assert(err, tc.ErrorMatches, "Invalid value.*for option.*--format.*")
}

func (s *PebbleSuite) TestChangesUnknownMaintenance(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/changes")
		c.Check(r.URL.Query(), tc.DeepEquals, url.Values{"select": {"all"}})
		json := strings.Replace(fakeChangesJSON, `"type": "sync"`, `"type": "sync", "maintenance": {"kind": "dachshund", "message": "unknown maintenance reason"}`, 1)
		fmt.Fprintln(w, json)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"changes"})
	c.Assert(err, tc.ErrorMatches, "unknown maintenance reason")
	c.Check(rest, tc.HasLen, 1)
	c.Check(s.Stdout(), tc.Matches, "")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestChangeSimple(c *tc.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		if n < 2 {
			c.Check(r.Method, tc.Equals, "GET")
			c.Check(r.URL.Path, tc.Equals, "/v1/changes/42")
			fmt.Fprintln(w, fakeChangeJSON)
		} else {
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	expectedChange := `(?ms)Status +Spawn +Ready +Summary
Do +2016-04-21T01:02:03Z +- +some summary
`
	rest, err := cli.ParserForTest().ParseArgs([]string{"tasks", "--abs-time", "42"})
	c.Assert(err, tc.IsNil)
	c.Assert(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Matches, expectedChange)
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestChangeSimpleFails(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/changes/42")
		fmt.Fprintln(w, `{"type": "error", "result": {"message": "could not bar"}}`)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"tasks", "--abs-time", "42"})
	c.Assert(err, tc.ErrorMatches, "could not bar")
	c.Assert(rest, tc.HasLen, 1)
	c.Check(s.Stdout(), tc.Matches, "")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestChangeSimpleRebooting(c *tc.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		if n < 2 {
			c.Check(r.Method, tc.Equals, "GET")
			c.Check(r.URL.Path, tc.Equals, "/v1/changes/42")
			fmt.Fprintln(w, strings.Replace(fakeChangeJSON, `"type": "sync"`, `"type": "sync", "maintenance": {"kind": "system-restart", "message": "system is restarting"}`, 1))
		} else {
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})

	_, err := cli.ParserForTest().ParseArgs([]string{"tasks", "42"})
	c.Assert(err, tc.IsNil)
	c.Check(s.Stderr(), tc.Equals, "WARNING: Pebble is about to reboot the system\n")
}

func (s *PebbleSuite) TestChangeSimpleUnknownMaintenance(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/changes/42")
		fmt.Fprintln(w, strings.Replace(fakeChangeJSON, `"type": "sync"`, `"type": "sync", "maintenance": {"kind": "dachshund", "message": "unknown maintenance reason"}`, 1))
	})

	_, err := cli.ParserForTest().ParseArgs([]string{"tasks", "42"})
	c.Assert(err, tc.ErrorMatches, "unknown maintenance reason")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestTasksLast(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		if r.URL.Path == "/v1/changes" {
			fmt.Fprintln(w, fakeChangesJSON)
			return
		}
		c.Assert(r.URL.Path, tc.Equals, "/v1/changes/two")
		fmt.Fprintln(w, fakeChangeJSON)
	})
	expectedChange := `(?ms)Status +Spawn +Ready +Summary
Do +2016-04-21T01:02:03Z +- +some summary
`
	rest, err := cli.ParserForTest().ParseArgs([]string{"tasks", "--abs-time", "--last=install"})
	c.Assert(err, tc.IsNil)
	c.Assert(rest, tc.DeepEquals, []string{})
	c.Check(s.Stdout(), tc.Matches, expectedChange)
	c.Check(s.Stderr(), tc.Equals, "")

	_, err = cli.ParserForTest().ParseArgs([]string{"tasks", "--abs-time", "--last=foobar"})
	c.Assert(err, tc.NotNil)
	c.Assert(err, tc.ErrorMatches, `no changes of type "foobar" found`)
}

func (s *PebbleSuite) TestTasksLastQuestionmark(c *tc.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		n++
		c.Check(r.Method, tc.Equals, "GET")
		c.Assert(r.URL.Path, tc.Equals, "/v1/changes")
		switch n {
		case 1, 2:
			fmt.Fprintln(w, `{"type": "sync", "result": []}`)
		case 3, 4:
			fmt.Fprintln(w, fakeChangesJSON)
		default:
			c.Errorf("expected 4 calls, now on %d", n)
		}
	})
	for i := range 2 {
		rest, err := cli.ParserForTest().ParseArgs([]string{"tasks", "--last=foobar?"})
		c.Assert(err, tc.IsNil)
		c.Assert(rest, tc.DeepEquals, []string{})
		c.Check(s.Stdout(), tc.Matches, "")
		c.Check(s.Stderr(), tc.Equals, "")

		_, err = cli.ParserForTest().ParseArgs([]string{"tasks", "--last=foobar"})
		if i == 0 {
			c.Assert(err, tc.ErrorMatches, `no changes found`)
		} else {
			c.Assert(err, tc.ErrorMatches, `no changes of type "foobar" found`)
		}
	}

	c.Check(n, tc.Equals, 4)
}

func (s *PebbleSuite) TestTasksSyntaxError(c *tc.C) {
	_, err := cli.ParserForTest().ParseArgs([]string{"tasks", "--abs-time", "--last=install", "42"})
	c.Assert(err, tc.NotNil)
	c.Assert(err, tc.ErrorMatches, `cannot use change ID and type together`)

	_, err = cli.ParserForTest().ParseArgs([]string{"tasks"})
	c.Assert(err, tc.NotNil)
	c.Assert(err, tc.ErrorMatches, `please provide change ID or type with --last=<type>`)
}

func (s *PebbleSuite) TestTasksJSON(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/changes/42")
		fmt.Fprintln(w, fakeChangeJSON)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"tasks", "--format", "json", "42"})
	c.Assert(err, tc.IsNil)
	c.Check(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, `{"id":"uno","kind":"foo","summary":"...","status":"Do","tasks":[{"id":"","kind":"bar","summary":"some summary","status":"Do","progress":{"label":"","done":0,"total":1},"spawn-time":"2016-04-21T01:02:03Z"}],"ready":false,"spawn-time":"2016-04-21T01:02:03Z"}`+"\n")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestTasksYAML(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, tc.Equals, "GET")
		c.Check(r.URL.Path, tc.Equals, "/v1/changes/42")
		fmt.Fprintln(w, fakeChangeJSON)
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"tasks", "--format", "yaml", "42"})
	c.Assert(err, tc.IsNil)
	c.Check(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, `
id: uno
kind: foo
summary: '...'
status: Do
tasks:
    - id: ""
      kind: bar
      summary: some summary
      status: Do
      progress:
        label: ""
        done: 0
        total: 1
      spawn-time: 2016-04-21T01:02:03Z
ready: false
spawn-time: 2016-04-21T01:02:03Z
`[1:])
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestTasksInvalidFormat(c *tc.C) {
	_, err := cli.ParserForTest().ParseArgs([]string{"tasks", "--format", "foobar", "42"})
	c.Assert(err, tc.ErrorMatches, "Invalid value.*for option.*--format.*")
}

func (s *PebbleSuite) TestChangeProgress(c *tc.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, tc.Equals, "GET")
			c.Check(r.URL.Path, tc.Equals, "/v1/changes/42")
			fmt.Fprintln(w, fakeChangeInProgressJSON)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := cli.ParserForTest().ParseArgs([]string{"tasks", "--abs-time", "42"})
	c.Assert(err, tc.IsNil)
	c.Assert(rest, tc.DeepEquals, []string{})
	c.Check(s.Stdout(), tc.Matches, `(?ms)Status +Spawn +Ready +Summary
Doing +2016-04-21T01:02:03Z +2016-04-21T01:02:04Z +some summary \(50.00%\)
`)
	c.Check(s.Stderr(), tc.Equals, "")
}
