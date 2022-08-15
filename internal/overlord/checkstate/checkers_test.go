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
	"strconv"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internal/reaper"
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

	// But only first 20 lines of long response body are included in error details
	var output bytes.Buffer
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&output, "line %d\n", i)
	}
	response = output.String()
	chk = &httpChecker{url: server.URL + "/500"}
	err = chk.check(context.Background())
	c.Assert(err, ErrorMatches, "non-2xx status code 500")
	detailsErr, ok = err.(*detailsError)
	c.Assert(ok, Equals, true)
	c.Assert(detailsErr.Details(), Matches, `(?s)line 1\n.*line 20\n\(\.\.\.\)`)

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
	c.Assert(err, ErrorMatches, "cannot parse check command: .*")

	// Non-zero exit status fails
	chk = &execChecker{command: "sleep x"}
	err = chk.check(context.Background())
	c.Assert(err, ErrorMatches, "exit status 1")
	detailsErr, ok := err.(*detailsError)
	c.Assert(ok, Equals, true)
	c.Assert(detailsErr.Details(), Matches, `(?s)sleep: invalid time interval.*`)

	// Long output on failure provides last 20 lines of output
	var output bytes.Buffer
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&output, "echo line %d\n", i)
	}
	chk = &execChecker{command: "/bin/sh -c '" + output.String() + "\nexit 1'"}
	err = chk.check(context.Background())
	c.Assert(err, ErrorMatches, "exit status 1")
	detailsErr, ok = err.(*detailsError)
	c.Assert(ok, Equals, true)
	c.Assert(detailsErr.Details(), Matches, `(?s)\(\.\.\.\)\nline 11\n.*line 30`)

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

	// Does not inherit environment when no environment vars set
	os.Setenv("PEBBLE_TEST_CHECKERS_EXEC", "parent")
	chk = &execChecker{
		command: "/bin/sh -c 'echo $PEBBLE_TEST_CHECKERS_EXEC; exit 1'",
	}
	err = chk.check(context.Background())
	c.Assert(err, ErrorMatches, "exit status 1")
	detailsErr, ok = err.(*detailsError)
	c.Assert(ok, Equals, true)
	c.Assert(detailsErr.Details(), Equals, "")

	// Does not inherit environment when some environment vars set
	os.Setenv("PEBBLE_TEST_CHECKERS_EXEC", "parent")
	chk = &execChecker{
		command:     "/bin/sh -c 'echo FOO=$FOO test=$PEBBLE_TEST_CHECKERS_EXEC; exit 1'",
		environment: map[string]string{"FOO": "foo"},
	}
	err = chk.check(context.Background())
	c.Assert(err, ErrorMatches, "exit status 1")
	detailsErr, ok = err.(*detailsError)
	c.Assert(ok, Equals, true)
	c.Assert(detailsErr.Details(), Equals, "FOO=foo test=")

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
}
