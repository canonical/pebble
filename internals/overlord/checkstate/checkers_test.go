// Copyright (c) 2021 Canonical Ltd
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

package checkstate

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/user"
	"strconv"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/reaper"
)

type CheckersSuite struct{}

var _ = Suite(&CheckersSuite{})

func (s *CheckersSuite) TestHTTP(c *C) {
	var path string
	var headers http.Header
	var response string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, Equals, "GET")
		path = r.URL.Path
		headers = r.Header
		status, err := strconv.Atoi(r.URL.Path[1:])
		if err == nil {
			w.WriteHeader(status)
		}
		if response != "" {
			fmt.Fprint(w, response)
		} else {
			fmt.Fprintf(w, "%s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	// Good 200 URL works
	chk := &httpChecker{url: server.URL + "/foo/bar"}
	err := chk.check(context.Background())
	c.Assert(err, IsNil)
	c.Assert(path, Equals, "/foo/bar")

	// Custom headers are sent through
	chk = &httpChecker{
		url:     server.URL + "/foo/bar",
		headers: map[string]string{"X-Name": "Bob Smith", "User-Agent": "pebble-test"},
	}
	err = chk.check(context.Background())
	c.Assert(err, IsNil)
	c.Assert(path, Equals, "/foo/bar")
	c.Assert(headers.Get("X-Name"), Equals, "Bob Smith")
	c.Assert(headers.Get("User-Agent"), Equals, "pebble-test")

	// Non-2xx status code returns error
	chk = &httpChecker{url: server.URL + "/404"}
	err = chk.check(context.Background())
	c.Assert(err, ErrorMatches, "non-2xx status code 404")
	c.Assert(path, Equals, "/404")

	// In case of non-2xx status, short response body is fully included in error details
	response = "error details"
	chk = &httpChecker{url: server.URL + "/500"}
	err = chk.check(context.Background())
	c.Assert(err, ErrorMatches, "non-2xx status code 500")
	detailsErr, ok := err.(*detailsError)
	c.Assert(ok, Equals, true)
	c.Assert(detailsErr.Details(), Equals, "error details")

	// But only first 5 lines of long response body are included in error details
	var output bytes.Buffer
	for i := 1; i <= 7; i++ {
		fmt.Fprintf(&output, "line %d\n", i)
	}
	response = output.String()
	chk = &httpChecker{url: server.URL + "/500"}
	err = chk.check(context.Background())
	c.Assert(err, ErrorMatches, "non-2xx status code 500")
	detailsErr, ok = err.(*detailsError)
	c.Assert(ok, Equals, true)
	c.Assert(detailsErr.Details(), Matches, `(?s)line 1\n.*line 5\n\(\.\.\.\)`)

	// Cancelled context returns error
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	chk = &httpChecker{url: server.URL}
	err = chk.check(ctx)
	c.Assert(err, ErrorMatches, ".* context canceled")

	// After server closed, should get a network dial error
	server.Close()
	chk = &httpChecker{url: server.URL}
	err = chk.check(context.Background())
	c.Assert(err, ErrorMatches, ".* connection refused")
}

func (s *CheckersSuite) TestTCP(c *C) {
	listener, err := net.Listen("tcp", "localhost:")
	c.Assert(err, IsNil)
	port := listener.Addr().(*net.TCPAddr).Port
	defer listener.Close()

	// Correct port works
	chk := &tcpChecker{port: port}
	err = chk.check(context.Background())
	c.Assert(err, IsNil)

	// Invalid port fails
	chk = &tcpChecker{port: 12345}
	err = chk.check(context.Background())
	c.Assert(err, ErrorMatches, ".* connection refused")

	// Invalid host fails
	chk = &tcpChecker{port: port, host: "badhost"}
	err = chk.check(context.Background())
	c.Assert(err, ErrorMatches, ".* lookup badhost.*")

	// Cancelled context returns error
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	chk = &tcpChecker{port: port}
	err = chk.check(ctx)
	c.Assert(err, ErrorMatches, ".* operation was canceled")

	// After listener closed returns error
	listener.Close()
	chk = &tcpChecker{port: port}
	err = chk.check(context.Background())
	c.Assert(err, ErrorMatches, ".* connection refused")
}

func (s *CheckersSuite) TestExec(c *C) {
	err := reaper.Start()
	c.Assert(err, IsNil)
	defer reaper.Stop()

	// Valid command succeeds
	chk := &execChecker{command: "echo foo"}
	err = chk.check(context.Background())
	c.Assert(err, IsNil)

	// Un-parseable command fails
	chk = &execChecker{command: "'"}
	err = chk.check(context.Background())
	c.Assert(err, ErrorMatches, "cannot parse command: .*")

	// Non-zero exit status fails
	chk = &execChecker{command: "sleep x"}
	err = chk.check(context.Background())
	c.Assert(err, ErrorMatches, "exit status 1")
	detailsErr, ok := err.(*detailsError)
	c.Assert(ok, Equals, true)
	c.Assert(detailsErr.Details(), Matches, `(?s)sleep: invalid time interval.*`)

	// Long output on failure provides last 5 lines of output
	var output bytes.Buffer
	for i := 1; i <= 7; i++ {
		fmt.Fprintf(&output, "echo line %d\n", i)
	}
	chk = &execChecker{command: "/bin/sh -c '" + output.String() + "\nexit 1'"}
	err = chk.check(context.Background())
	c.Assert(err, ErrorMatches, "exit status 1")
	detailsErr, ok = err.(*detailsError)
	c.Assert(ok, Equals, true)
	c.Assert(detailsErr.Details(), Matches, `(?s)\(\.\.\.\)\nline 3\n.*line 7`)

	// Environment variables are passed through
	chk = &execChecker{
		command:     "/bin/sh -c 'echo $FOO $BAR; exit 1'",
		environment: map[string]string{"FOO": "Foo,", "BAR": "meet Bar."},
	}
	err = chk.check(context.Background())
	c.Assert(err, ErrorMatches, "exit status 1")
	detailsErr, ok = err.(*detailsError)
	c.Assert(ok, Equals, true)
	c.Assert(detailsErr.Details(), Equals, "Foo, meet Bar.")

	// Inherits environment when no environment vars set
	os.Setenv("PEBBLE_TEST_CHECKERS_EXEC", "parent")
	chk = &execChecker{
		command: "/bin/sh -c 'echo $PEBBLE_TEST_CHECKERS_EXEC; exit 1'",
	}
	err = chk.check(context.Background())
	c.Assert(err, ErrorMatches, "exit status 1")
	detailsErr, ok = err.(*detailsError)
	c.Assert(ok, Equals, true)
	c.Assert(detailsErr.Details(), Equals, "parent")

	// Inherits environment when some environment vars set
	os.Setenv("PEBBLE_TEST_CHECKERS_EXEC", "parent")
	chk = &execChecker{
		command:     "/bin/sh -c 'echo FOO=$FOO test=$PEBBLE_TEST_CHECKERS_EXEC; exit 1'",
		environment: map[string]string{"FOO": "foo"},
	}
	err = chk.check(context.Background())
	c.Assert(err, ErrorMatches, "exit status 1")
	detailsErr, ok = err.(*detailsError)
	c.Assert(ok, Equals, true)
	c.Assert(detailsErr.Details(), Equals, "FOO=foo test=parent")

	// Working directory is passed through
	workingDir := c.MkDir()
	chk = &execChecker{
		command:    "/bin/sh -c 'pwd; exit 1'",
		workingDir: workingDir,
	}
	err = chk.check(context.Background())
	c.Assert(err, ErrorMatches, "exit status 1")
	detailsErr, ok = err.(*detailsError)
	c.Assert(ok, Equals, true)
	c.Assert(detailsErr.Details(), Equals, workingDir)

	// Cancelled context returns error
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	chk = &execChecker{command: "echo foo"}
	err = chk.check(ctx)
	c.Assert(err, ErrorMatches, "context canceled")

	// Can run as current user and group
	currentUser, err := user.Current()
	c.Assert(err, IsNil)
	group, err := user.LookupGroupId(currentUser.Gid)
	c.Assert(err, IsNil)
	chk = &execChecker{
		command: "/bin/sh -c 'id -n -u; exit 1'",
		user:    currentUser.Username,
		group:   group.Name,
	}
	err = chk.check(context.Background())
	c.Assert(err, ErrorMatches, "exit status 1")
	detailsErr, ok = err.(*detailsError)
	c.Assert(ok, Equals, true)
	c.Assert(detailsErr.Details(), Equals, currentUser.Username)
}

func (s *CheckersSuite) TestNewChecker(c *C) {
	chk := newChecker(&plan.Check{
		Name: "http",
		HTTP: &plan.HTTPCheck{
			URL:     "https://example.com/foo",
			Headers: map[string]string{"k": "v"},
		},
	})
	http, ok := chk.(*httpChecker)
	c.Assert(ok, Equals, true)
	c.Check(http.name, Equals, "http")
	c.Check(http.url, Equals, "https://example.com/foo")
	c.Check(http.headers, DeepEquals, map[string]string{"k": "v"})

	chk = newChecker(&plan.Check{
		Name: "tcp",
		TCP: &plan.TCPCheck{
			Port: 80,
			Host: "localhost",
		},
	})
	tcp, ok := chk.(*tcpChecker)
	c.Assert(ok, Equals, true)
	c.Check(tcp.name, Equals, "tcp")
	c.Check(tcp.port, Equals, 80)
	c.Check(tcp.host, Equals, "localhost")

	userID, groupID := 100, 200
	chk = newChecker(&plan.Check{
		Name: "exec",
		Exec: &plan.ExecCheck{
			Command:     "sleep 1",
			Environment: map[string]string{"k": "v"},
			UserID:      &userID,
			User:        "user",
			GroupID:     &groupID,
			Group:       "group",
			WorkingDir:  "/working/dir",
		},
	})
	exec, ok := chk.(*execChecker)
	c.Assert(ok, Equals, true)
	c.Assert(exec.name, Equals, "exec")
	c.Assert(exec.command, Equals, "sleep 1")
	c.Assert(exec.environment, DeepEquals, map[string]string{"k": "v"})
	c.Assert(exec.userID, Equals, &userID)
	c.Assert(exec.user, Equals, "user")
	c.Assert(exec.groupID, Equals, &groupID)
	c.Assert(exec.workingDir, Equals, "/working/dir")
}

func (s *CheckersSuite) TestExecContextNoOverride(c *C) {
	svcUserID, svcGroupID := 10, 20
	config := mergeServiceContext(&plan.Plan{Services: map[string]*plan.Service{
		"svc1": {
			Name:        "svc1",
			Environment: map[string]string{"k": "x", "a": "1"},
			UserID:      &svcUserID,
			User:        "svcuser",
			GroupID:     &svcGroupID,
			Group:       "svcgroup",
			WorkingDir:  "/working/svc",
		},
	}}, &plan.Check{
		Name: "exec",
		Exec: &plan.ExecCheck{
			Command:        "sleep 1",
			ServiceContext: "svc1",
		},
	})
	chk := newChecker(config)
	exec, ok := chk.(*execChecker)
	c.Assert(ok, Equals, true)
	c.Check(exec.name, Equals, "exec")
	c.Check(exec.command, Equals, "sleep 1")
	c.Check(exec.environment, DeepEquals, map[string]string{"k": "x", "a": "1"})
	c.Check(exec.userID, DeepEquals, &svcUserID)
	c.Check(exec.user, Equals, "svcuser")
	c.Check(exec.groupID, DeepEquals, &svcGroupID)
	c.Check(exec.workingDir, Equals, "/working/svc")
}

func (s *CheckersSuite) TestExecContextOverride(c *C) {
	userID, groupID := 100, 200
	svcUserID, svcGroupID := 10, 20
	config := mergeServiceContext(&plan.Plan{Services: map[string]*plan.Service{
		"svc1": {
			Name:        "svc1",
			Environment: map[string]string{"k": "x", "a": "1"},
			UserID:      &svcUserID,
			User:        "svcuser",
			GroupID:     &svcGroupID,
			Group:       "svcgroup",
			WorkingDir:  "/working/svc",
		},
	}}, &plan.Check{
		Name: "exec",
		Exec: &plan.ExecCheck{
			Command:        "sleep 1",
			ServiceContext: "svc1",
			Environment:    map[string]string{"k": "v"},
			UserID:         &userID,
			User:           "user",
			GroupID:        &groupID,
			Group:          "group",
			WorkingDir:     "/working/dir",
		},
	})
	chk := newChecker(config)
	exec, ok := chk.(*execChecker)
	c.Assert(ok, Equals, true)
	c.Check(exec.name, Equals, "exec")
	c.Check(exec.command, Equals, "sleep 1")
	c.Check(exec.environment, DeepEquals, map[string]string{"k": "v", "a": "1"})
	c.Check(exec.userID, DeepEquals, &userID)
	c.Check(exec.user, Equals, "user")
	c.Check(exec.groupID, DeepEquals, &groupID)
	c.Check(exec.workingDir, Equals, "/working/dir")
}
