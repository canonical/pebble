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

package main_test

import (
	"fmt"
	"net/http"
	"strings"

	"gopkg.in/check.v1"

	pebble "github.com/canonical/pebble/cmd/pebble"
)

var fakeChangeJSON = `{"type": "sync", "result": {
  "id":   "uno",
  "kind": "foo",
  "summary": "...",
  "status": "Do",
  "ready": false,
  "spawn-time": "2016-04-21T01:02:03Z",
  "ready-time": "2016-04-21T01:02:04Z",
  "tasks": [{"kind": "bar", "summary": "some summary", "status": "Do", "progress": {"done": 0, "total": 1}, "spawn-time": "2016-04-21T01:02:03Z", "ready-time": "2016-04-21T01:02:04Z"}]
}}`

func (s *PebbleSuite) TestChangeSimple(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		if n < 2 {
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v1/changes/42")
			fmt.Fprintln(w, fakeChangeJSON)
		} else {
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	expectedChange := `(?ms)Status +Spawn +Ready +Summary
Do +2016-04-21T01:02:03Z +2016-04-21T01:02:04Z +some summary
`
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"tasks", "--abs-time", "42"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, expectedChange)
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *PebbleSuite) TestChangeSimpleRebooting(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		if n < 2 {
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v1/changes/42")
			fmt.Fprintln(w, strings.Replace(fakeChangeJSON, `"type": "sync"`, `"type": "sync", "maintenance": {"kind": "system-restart", "message": "system is restarting"}`, 1))
		} else {
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})

	_, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"tasks", "42"})
	c.Assert(err, check.IsNil)
	c.Check(s.Stderr(), check.Equals, "WARNING: pebble is about to reboot the system\n")
}

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
    "kind": "remove-snap",
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
    "ready-time": "2016-01-21T01:02:04Z",
    "tasks": [{"kind": "bar", "summary": "some summary", "status": "Do", "progress": {"done": 0, "total": 1}, "spawn-time": "2016-01-21T01:02:03Z", "ready-time": "2016-01-21T01:02:04Z"}]
  }
]}`

func (s *PebbleSuite) TestTasksLast(c *check.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, check.Equals, "GET")
		if r.URL.Path == "/v1/changes" {
			fmt.Fprintln(w, fakeChangesJSON)
			return
		}
		c.Assert(r.URL.Path, check.Equals, "/v1/changes/two")
		fmt.Fprintln(w, fakeChangeJSON)
	})
	expectedChange := `(?ms)Status +Spawn +Ready +Summary
Do +2016-04-21T01:02:03Z +2016-04-21T01:02:04Z +some summary
`
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"tasks", "--abs-time", "--last=install"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, expectedChange)
	c.Check(s.Stderr(), check.Equals, "")

	_, err = pebble.Parser(pebble.Client()).ParseArgs([]string{"tasks", "--abs-time", "--last=foobar"})
	c.Assert(err, check.NotNil)
	c.Assert(err, check.ErrorMatches, `no changes of type "foobar" found`)
}

func (s *PebbleSuite) TestTasksLastQuestionmark(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		n++
		c.Check(r.Method, check.Equals, "GET")
		c.Assert(r.URL.Path, check.Equals, "/v1/changes")
		switch n {
		case 1, 2:
			fmt.Fprintln(w, `{"type": "sync", "result": []}`)
		case 3, 4:
			fmt.Fprintln(w, fakeChangesJSON)
		default:
			c.Errorf("expected 4 calls, now on %d", n)
		}
	})
	for i := 0; i < 2; i++ {
		rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"tasks", "--last=foobar?"})
		c.Assert(err, check.IsNil)
		c.Assert(rest, check.DeepEquals, []string{})
		c.Check(s.Stdout(), check.Matches, "")
		c.Check(s.Stderr(), check.Equals, "")

		_, err = pebble.Parser(pebble.Client()).ParseArgs([]string{"tasks", "--last=foobar"})
		if i == 0 {
			c.Assert(err, check.ErrorMatches, `no changes found`)
		} else {
			c.Assert(err, check.ErrorMatches, `no changes of type "foobar" found`)
		}
	}

	c.Check(n, check.Equals, 4)
}

func (s *PebbleSuite) TestTasksSyntaxError(c *check.C) {
	_, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"tasks", "--abs-time", "--last=install", "42"})
	c.Assert(err, check.NotNil)
	c.Assert(err, check.ErrorMatches, `cannot use change ID and type together`)

	_, err = pebble.Parser(pebble.Client()).ParseArgs([]string{"tasks"})
	c.Assert(err, check.NotNil)
	c.Assert(err, check.ErrorMatches, `please provide change ID or type with --last=<type>`)
}

var fakeChangeInProgressJSON = `{"type": "sync", "result": {
  "id":   "uno",
  "kind": "foo",
  "summary": "...",
  "status": "Do",
  "ready": false,
  "spawn-time": "2016-04-21T01:02:03Z",
  "ready-time": "2016-04-21T01:02:04Z",
  "tasks": [{"kind": "bar", "summary": "some summary", "status": "Doing", "progress": {"done": 50, "total": 100}, "spawn-time": "2016-04-21T01:02:03Z", "ready-time": "2016-04-21T01:02:04Z"}]
}}`

func (s *PebbleSuite) TestChangeProgress(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.Method, check.Equals, "GET")
			c.Check(r.URL.Path, check.Equals, "/v1/changes/42")
			fmt.Fprintln(w, fakeChangeInProgressJSON)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}

		n++
	})
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"tasks", "--abs-time", "42"})
	c.Assert(err, check.IsNil)
	c.Assert(rest, check.DeepEquals, []string{})
	c.Check(s.Stdout(), check.Matches, `(?ms)Status +Spawn +Ready +Summary
Doing +2016-04-21T01:02:03Z +2016-04-21T01:02:04Z +some summary \(50.00%\)
`)
	c.Check(s.Stderr(), check.Equals, "")
}
