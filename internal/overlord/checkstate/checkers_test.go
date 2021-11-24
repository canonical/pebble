// Test the individual checker types

package checkstate

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internal/plan"
)

func Test(t *testing.T) {
	TestingT(t)
}

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

func (s *CheckersSuite) TestNewChecker(c *C) {
	chk := newChecker(&plan.Check{
		Name: "http",
		HTTP: &plan.HTTPCheckConfig{
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
		TCP: &plan.TCPCheckConfig{
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
		Exec: &plan.ExecCheckConfig{
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
