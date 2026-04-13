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

package cli_test

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/cli"
)

type warningSuite struct {
	BasePebbleSuite
}

var _ = check.Suite(&warningSuite{})

const testWarnings = `
{
	"result": [{
		"id": "1",
        "type": "warning",
		"key": "hello world number one",
		"first-occurred": "2018-09-19T12:41:18.505007495Z",
		"last-occurred": "2018-09-19T12:41:18.505007495Z",
		"last-repeated": "2018-09-19T12:41:18.505007495Z",
		"expire-after": "672h0m0s",
		"repeat-after": "24h0m0s"
	}, {
		"id": "2",
        "type": "warning",
		"key": "hello world number two",
		"expire-after": "672h0m0s",
		"first-occurred": "2018-09-19T12:44:19.680362867Z",
		"last-occurred": "2018-09-19T12:44:19.680362867Z",
		"last-repeated": "2018-09-19T12:44:19.680362867Z",
		"repeat-after": "24h0m0s"
	}, {
		"id": "3",
        "type": "warning",
		"key": "hello world number three",
		"expire-after": "672h0m0s",
		"first-occurred": "2018-09-19T12:44:30.680362867Z",
		"last-occurred": "2018-09-19T12:44:30.680362867Z",
		"last-repeated": "2018-09-19T12:44:50.680362867Z",
		"repeat-after": "24h0m0s"
	}],
	"status": "OK",
	"status-code": 200,
	"type": "sync"
}`

func mkWarningsFakeHandler(c *check.C, body string) func(w http.ResponseWriter, r *http.Request) {
	var called bool
	return func(w http.ResponseWriter, r *http.Request) {
		if called {
			c.Fatalf("expected a single request")
		}
		called = true
		c.Check(r.URL.Path, check.Equals, "/v1/notices")
		query := r.URL.Query()
		c.Check(query["types"], check.DeepEquals, []string{"warning"})

		buf, err := io.ReadAll(r.Body)
		c.Assert(err, check.IsNil)
		c.Check(string(buf), check.Equals, "")
		c.Check(r.Method, check.Equals, "GET")
		w.WriteHeader(200)
		fmt.Fprintln(w, body)
	}
}

func (s *warningSuite) TestNoWarningsEver(c *check.C) {
	s.RedirectClientToTestServer(mkWarningsFakeHandler(c, `{"type": "sync", "status-code": 200, "result": []}`))

	rest, err := cli.ParserForTest().ParseArgs([]string{"warnings"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "No warnings.\n")
	c.Check(s.Stdout(), check.Equals, "")
}

func (s *warningSuite) TestNoFurtherWarnings(c *check.C) {
	s.writeCLIState(c, map[string]any{
		"warnings-last-listed": time.Date(2023, 9, 6, 15, 6, 0, 0, time.UTC),
		"warnings-last-okayed": time.Date(2023, 9, 6, 15, 6, 0, 0, time.UTC),
	})

	s.RedirectClientToTestServer(mkWarningsFakeHandler(c, `{"type": "sync", "status-code": 200, "result": []}`))

	rest, err := cli.ParserForTest().ParseArgs([]string{"warnings"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "No further warnings.\n")
	c.Check(s.Stdout(), check.Equals, "")
}

func (s *warningSuite) TestWarnings(c *check.C) {
	s.RedirectClientToTestServer(mkWarningsFakeHandler(c, testWarnings))

	rest, err := cli.ParserForTest().ParseArgs([]string{"warnings", "--abs-time", "--unicode=never"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, `
last-occurrence:  2018-09-19T12:41:18Z
warning: |
  hello world number one
---
last-occurrence:  2018-09-19T12:44:19Z
warning: |
  hello world number two
---
last-occurrence:  2018-09-19T12:44:30Z
warning: |
  hello world number three
`[1:])
}

func (s *warningSuite) TestVerboseWarnings(c *check.C) {
	s.RedirectClientToTestServer(mkWarningsFakeHandler(c, testWarnings))

	rest, err := cli.ParserForTest().ParseArgs([]string{"warnings", "--abs-time", "--verbose", "--unicode=never"})
	c.Assert(err, check.IsNil)
	c.Check(rest, check.HasLen, 0)
	c.Check(s.Stderr(), check.Equals, "")
	c.Check(s.Stdout(), check.Equals, `
first-occurrence:  2018-09-19T12:41:18Z
last-occurrence:   2018-09-19T12:41:18Z
last-repeated:     2018-09-19T12:41:18Z
repeats-after:     1d00h
expires-after:     28d0h
warning: |
  hello world number one
---
first-occurrence:  2018-09-19T12:44:19Z
last-occurrence:   2018-09-19T12:44:19Z
last-repeated:     2018-09-19T12:44:19Z
repeats-after:     1d00h
expires-after:     28d0h
warning: |
  hello world number two
---
first-occurrence:  2018-09-19T12:44:30Z
last-occurrence:   2018-09-19T12:44:30Z
last-repeated:     2018-09-19T12:44:50Z
repeats-after:     1d00h
expires-after:     28d0h
warning: |
  hello world number three
`[1:])
}

func (s *warningSuite) TestCommandWithWarnings(c *check.C) {
	var responseTimestamp time.Time

	timesCalled := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		timesCalled++
		c.Check(r.URL.Path, check.Equals, "/v1/system-info")
		c.Check(r.URL.Query(), check.HasLen, 0)

		buf, err := io.ReadAll(r.Body)
		c.Assert(err, check.IsNil)
		c.Check(string(buf), check.Equals, "")
		c.Check(r.Method, check.Equals, "GET")
		w.WriteHeader(200)
		latestWarningStr := ""
		if !responseTimestamp.IsZero() {
			latestWarningStr = fmt.Sprintf(`, "latest-warning": "%s"`, responseTimestamp.Format(time.RFC3339Nano))
		}
		fmt.Fprintf(w, `{
				"result": {},
				"status": "OK",
				"status-code": 200,
				"type": "sync"
                %s
			}\n`, latestWarningStr)
	})

	client := cli.Client()
	expectedWarnings := map[int]string{
		0: "",
		1: "WARNING: There are new warnings. See 'pebble warnings'.\n",
		2: "WARNING: There are new warnings. See 'pebble warnings'.\n",
	}

	for expectedCount, expectedWarning := range expectedWarnings {
		if expectedCount == 0 {
			responseTimestamp = time.Time{}
		} else {
			responseTimestamp = time.Date(2018, 9, 19, 12, 44, 19, 680362867, time.UTC)
		}
		runOpts := cli.RunOptionsForTest()
		rest, err := cli.Parser(&cli.ParserOptions{
			Client:     client,
			SocketPath: runOpts.ClientConfig.Socket,
			PebbleDir:  runOpts.PebbleDir,
		}).ParseArgs([]string{"version"})
		c.Assert(err, check.IsNil)

		latest := client.LatestWarningTime()
		if expectedCount == 0 {
			c.Check(latest, check.Equals, time.Time{})
		} else {
			c.Check(latest, check.Equals, responseTimestamp)
		}

		cli.MaybePresentWarnings(time.Time{}, latest)

		c.Check(rest, check.HasLen, 0)
		c.Check(s.Stdout(), check.Matches, `(?s)client.*server.*`)
		c.Check(s.Stderr(), check.Equals, expectedWarning)
		s.ResetStdStreams()
	}

	c.Check(timesCalled, check.Equals, len(expectedWarnings))
}

func (s *warningSuite) TestExtraArgs(c *check.C) {
	rest, err := cli.ParserForTest().ParseArgs([]string{"warnings", "extra", "args"})
	c.Assert(err, check.Equals, cli.ErrExtraArgs)
	c.Check(rest, check.HasLen, 1)

	rest, err = cli.ParserForTest().ParseArgs([]string{"okay", "extra", "invalid arg"})
	c.Assert(err, check.Equals, cli.ErrExtraArgs)
	c.Check(rest, check.HasLen, 1)
}
