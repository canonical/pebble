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
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"

	. "gopkg.in/check.v1"
)

type CheckersSuite struct{}

var _ = Suite(&CheckersSuite{})

func (s *CheckersSuite) TestHTTP(c *C) {
	var path string
	var headers http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.Method, Equals, "GET")
		path = r.URL.Path
		headers = r.Header
		status, err := strconv.Atoi(r.URL.Path[1:])
		if err == nil {
			w.WriteHeader(status)
		}
		fmt.Fprintf(w, "%s %s", r.Method, r.URL.Path)
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

	// Non-20x status code returns error
	chk = &httpChecker{url: server.URL + "/404"}
	err = chk.check(context.Background())
	c.Assert(err, ErrorMatches, "received non-20x status code 404")
	c.Assert(path, Equals, "/404")

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
	// Valid command succeeds
	chk := &execChecker{command: "echo foo"}
	err := chk.check(context.Background())
	c.Assert(err, IsNil)

	// Un-parseable command fails
	chk = &execChecker{command: "'"}
	err = chk.check(context.Background())
	c.Assert(err, ErrorMatches, "cannot parse check command: .*")

	// Non-zero exit status fails
	chk = &execChecker{command: "sleep x"}
	err = chk.check(context.Background())
	c.Assert(err, ErrorMatches, "exit status 1")
	outErr, ok := err.(*outputError)
	c.Assert(ok, Equals, true)
	c.Assert(outErr.output(), Matches, `(?s)sleep: invalid time interval.*`)

	// Long output on failure is truncated to 1024 bytes
	chk = &execChecker{command: "/bin/sh -c 'echo " + strings.Repeat("x", 1100) + "; exit 1'"}
	err = chk.check(context.Background())
	c.Assert(err, ErrorMatches, "exit status 1")
	outErr, ok = err.(*outputError)
	c.Assert(ok, Equals, true)
	c.Assert(outErr.output(), Equals, strings.Repeat("x", 1024)+"...")

	// Environment variables are passed through
	chk = &execChecker{
		command:     "/bin/sh -c 'echo $FOO $BAR; exit 1'",
		environment: map[string]string{"FOO": "Foo,", "BAR": "meet Bar."},
	}
	err = chk.check(context.Background())
	c.Assert(err, ErrorMatches, "exit status 1")
	outErr, ok = err.(*outputError)
	c.Assert(ok, Equals, true)
	c.Assert(outErr.output(), Equals, "Foo, meet Bar.\n")

	// Working directory is passed through
	workingDir := c.MkDir()
	chk = &execChecker{
		command:    "/bin/sh -c 'pwd; exit 1'",
		workingDir: workingDir,
	}
	err = chk.check(context.Background())
	c.Assert(err, ErrorMatches, "exit status 1")
	outErr, ok = err.(*outputError)
	c.Assert(ok, Equals, true)
	c.Assert(outErr.output(), Equals, workingDir+"\n")

	// Cancelled context returns error
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	chk = &execChecker{command: "echo foo"}
	err = chk.check(ctx)
	c.Assert(err, ErrorMatches, "context canceled")
}
