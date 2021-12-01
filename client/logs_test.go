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

package client_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"

	"gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
)

func (cs *clientSuite) TestLogsNoOptions(c *check.C) {
	cs.rsp = `
{"time":"2021-05-03T03:55:49.36Z","service":"thing","message":"log 1\n"}
{"time":"2021-05-03T03:55:49.654123Z","service":"snappass","message":"log two\n"}
{"time":"2021-05-03T03:55:50.076Z","service":"thing","message":"the third\n"}
`[1:]
	out, writeLog := makeLogWriter()
	err := cs.cli.Logs(&client.LogsOptions{
		WriteLog: writeLog,
	})
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v1/logs")
	c.Check(cs.req.URL.Query(), check.HasLen, 0)
	c.Check(out.String(), check.Equals, `
2021-05-03T03:55:49.360Z [thing] log 1
2021-05-03T03:55:49.654Z [snappass] log two
2021-05-03T03:55:50.076Z [thing] the third
`[1:])
}

func (cs *clientSuite) TestLogsServices(c *check.C) {
	cs.rsp = `
{"time":"2021-05-03T03:55:49.654334232Z","service":"snappass","message":"log two\n"}
`[1:]
	out, writeLog := makeLogWriter()
	err := cs.cli.Logs(&client.LogsOptions{
		WriteLog: writeLog,
		Services: []string{"snappass"},
	})
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v1/logs")
	c.Check(cs.req.URL.Query(), check.DeepEquals, url.Values{
		"services": []string{"snappass"},
	})
	c.Check(out.String(), check.Equals, `
2021-05-03T03:55:49.654Z [snappass] log two
`[1:])
}

func (cs *clientSuite) TestLogsAll(c *check.C) {
	cs.rsp = `
{"time":"2021-05-03T03:55:49.360994155Z","service":"thing","message":"log 1\n"}
{"time":"2021-05-03T03:55:49.654334232Z","service":"snappass","message":"log two\n"}
`[1:]
	out, writeLog := makeLogWriter()
	err := cs.cli.Logs(&client.LogsOptions{
		WriteLog: writeLog,
		N:        -1,
	})
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v1/logs")
	c.Check(cs.req.URL.Query(), check.DeepEquals, url.Values{
		"n": []string{"-1"},
	})
	c.Check(out.String(), check.Equals, `
2021-05-03T03:55:49.360Z [thing] log 1
2021-05-03T03:55:49.654Z [snappass] log two
`[1:])
}

func (cs *clientSuite) TestFollowLogs(c *check.C) {
	readsChan := make(chan string)
	cli, err := client.New(nil)
	c.Assert(err, check.IsNil)
	cli.SetDoer(doerFunc(func(req *http.Request) (*http.Response, error) {
		c.Check(req.Method, check.Equals, "GET")
		c.Check(req.URL.Path, check.Equals, "/v1/logs")
		c.Check(req.URL.Query(), check.DeepEquals, url.Values{
			"follow": []string{"true"},
		})
		rsp := &http.Response{
			Body:       &followReader{readsChan},
			Header:     make(http.Header),
			StatusCode: http.StatusOK,
		}
		return rsp, nil
	}))

	go func() {
		readsChan <- `{"time":"2021-05-03T03:55:49.360994155Z","service":"thing","message":"log 1\n"}` + "\n"
		readsChan <- `{"time":"2021-05-03T03:55:49.654334232Z","service":"snappass","message":"log two\n"}` + "\n"
		readsChan <- ""
	}()
	out, writeLog := makeLogWriter()

	err = cli.FollowLogs(context.Background(), &client.LogsOptions{
		WriteLog: writeLog,
	})
	c.Assert(err, check.IsNil)
	c.Check(out.String(), check.Equals, `
2021-05-03T03:55:49.360Z [thing] log 1
2021-05-03T03:55:49.654Z [snappass] log two
`[1:])
}

func (cs *clientSuite) TestLogsWriteLogError(c *check.C) {
	cs.rsp = `{"time":"2021-05-03T03:55:49.360994155Z","service":"thing","message":"log 1\n"}` + "\n"
	err := cs.cli.Logs(&client.LogsOptions{
		WriteLog: func(entry client.LogEntry) error {
			return fmt.Errorf("ERROR!")
		},
	})
	c.Assert(err, check.ErrorMatches, "cannot output log: ERROR!")
}

type doerFunc func(*http.Request) (*http.Response, error)

func (f doerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

type followReader struct {
	readsChan chan string
}

func (r *followReader) Read(b []byte) (int, error) {
	this := <-r.readsChan
	if this == "" {
		return 0, context.Canceled
	}
	n := copy(b, this)
	return n, nil
}

func (r *followReader) Close() error {
	return nil
}

func makeLogWriter() (*bytes.Buffer, func(entry client.LogEntry) error) {
	var out bytes.Buffer
	writeLog := func(entry client.LogEntry) error {
		fmt.Fprintf(&out, "%s [%s] %s",
			entry.Time.Format("2006-01-02T15:04:05.000Z07:00"), entry.Service, entry.Message)
		return nil
	}
	return &out, writeLog
}
