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
	"io"
	"net/http"
	"net/url"
	"time"

	"gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
)

func (cs *clientSuite) TestLogsNoOptions(c *check.C) {
	cs.rsp = `
{"time":"2021-05-03T03:55:49.360994155Z","service":"thing","stream":"stdout","length":6}
log 1
{"time":"2021-05-03T03:55:49.654334232Z","service":"snappass","stream":"stderr","length":8}
log two
{"time":"2021-05-03T03:55:50.076800988Z","service":"thing","stream":"stdout","length":10}
the third
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
2021-05-03T03:55:49Z thing stdout (6): log 1
2021-05-03T03:55:49Z snappass stderr (8): log two
2021-05-03T03:55:50Z thing stdout (10): the third
`[1:])
}

func (cs *clientSuite) TestLogsServices(c *check.C) {
	cs.rsp = `
{"time":"2021-05-03T03:55:49.654334232Z","service":"snappass","stream":"stderr","length":8}
log two
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
2021-05-03T03:55:49Z snappass stderr (8): log two
`[1:])
}

func (cs *clientSuite) TestLogsN(c *check.C) {
	cs.rsp = `
{"time":"2021-05-03T03:55:49.360994155Z","service":"thing","stream":"stdout","length":6}
log 1
{"time":"2021-05-03T03:55:49.654334232Z","service":"snappass","stream":"stderr","length":8}
log two
`[1:]
	out, writeLog := makeLogWriter()
	n := 2
	err := cs.cli.Logs(&client.LogsOptions{
		WriteLog: writeLog,
		NumLogs:  &n,
	})
	c.Assert(err, check.IsNil)
	c.Check(cs.req.Method, check.Equals, "GET")
	c.Check(cs.req.URL.Path, check.Equals, "/v1/logs")
	c.Check(cs.req.URL.Query(), check.DeepEquals, url.Values{
		"n": []string{"2"},
	})
	c.Check(out.String(), check.Equals, `
2021-05-03T03:55:49Z thing stdout (6): log 1
2021-05-03T03:55:49Z snappass stderr (8): log two
`[1:])
}

func (cs *clientSuite) TestFollowLogs(c *check.C) {
	readsChan := make(chan string)
	cli := client.New(nil)
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
		readsChan <- `{"time":"2021-05-03T03:55:49.360994155Z","service":"thing","stream":"stdout","length":6}` + "\n"
		readsChan <- "log 1\n"
		readsChan <- `{"time":"2021-05-03T03:55:49.654334232Z","service":"snappass","stream":"stderr","length":8}` + "\n"
		readsChan <- "log two\n"
		readsChan <- ""
	}()
	out, writeLog := makeLogWriter()

	err := cli.FollowLogs(context.Background(), &client.LogsOptions{
		WriteLog: writeLog,
	})
	c.Assert(err, check.IsNil)
	c.Check(out.String(), check.Equals, `
2021-05-03T03:55:49Z thing stdout (6): log 1
2021-05-03T03:55:49Z snappass stderr (8): log two
`[1:])
}

func (cs *clientSuite) TestLogsBadWriteLogFunc(c *check.C) {
	cs.rsp = `
{"time":"2021-05-03T03:55:49.360994155Z","service":"thing","stream":"stdout","length":6}
log 1
`[1:]
	err := cs.cli.Logs(&client.LogsOptions{
		WriteLog: func(_ time.Time, _ string, _ client.LogStream, _ int, _ io.Reader) error {
			return nil
		},
	})
	c.Assert(err, check.ErrorMatches, "WriteLog must read entire message")
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

func makeLogWriter() (*bytes.Buffer, client.WriteLogFunc) {
	var out bytes.Buffer
	writeLog := func(timestamp time.Time, service string, stream client.LogStream, length int, message io.Reader) error {
		fmt.Fprintf(&out, "%s %s %s (%d): ",
			timestamp.Format(time.RFC3339), service, stream, length)
		io.Copy(&out, message)
		return nil
	}
	return &out, writeLog
}
