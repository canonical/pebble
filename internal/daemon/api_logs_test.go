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

package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internal/overlord/servstate"
	"github.com/canonical/pebble/internal/servicelog"
)

var _ = Suite(&logsSuite{})

type logsSuite struct{}

type logEntry struct {
	Time    time.Time
	Service string
	Stream  string
	Length  int
	message string
}

type testServiceManager struct {
	logsFunc serviceLogsFunc
}

type serviceLogsFunc func(services []string, last int) (map[string]servicelog.Iterator, error)

func (m testServiceManager) Services(names []string) ([]*servstate.ServiceInfo, error) {
	if len(names) > 0 {
		panic("/v1/logs shouldn't call Services with names specified")
	}
	infos := []*servstate.ServiceInfo{
		{Name: "nginx"},
		{Name: "redis"},
		{Name: "postgresql"},
	}
	return infos, nil
}

func (m testServiceManager) ServiceLogs(services []string, last int) (map[string]servicelog.Iterator, error) {
	return m.logsFunc(services, last)
}

func (s *logsSuite) TestInvalidFollow(c *C) {
	rec := s.recordResponse(c, "/v1/logs?follow=invalid", nil)
	c.Assert(rec.Code, Equals, http.StatusBadRequest)
	checkError(c, rec.Body.Bytes(), http.StatusBadRequest, `follow parameter must be "true" or "false"`)
}

func (s *logsSuite) TestInvalidN(c *C) {
	rec := s.recordResponse(c, "/v1/logs?n=nan", nil)
	c.Assert(rec.Code, Equals, http.StatusBadRequest)
	checkError(c, rec.Body.Bytes(), http.StatusBadRequest, `n must be a valid integer`)
}

func (s *logsSuite) TestOneService(c *C) {
	wb := servicelog.NewWriteBuffer(20, 1024)
	stdout := wb.StreamWriter(servicelog.Stdout)
	stderr := wb.StreamWriter(servicelog.Stderr)
	for i := 0; i < 12; i++ {
		if i%2 == 0 {
			fmt.Fprintf(stdout, "stdout message %d\n", i)
		} else {
			fmt.Fprintf(stderr, "stderr message %d\n", i)
		}
	}

	svcMgr := testServiceManager{
		logsFunc: func(services []string, last int) (map[string]servicelog.Iterator, error) {
			its := map[string]servicelog.Iterator{
				"nginx": getIterator(wb, last),
			}
			return its, nil
		},
	}

	rec := s.recordResponse(c, "/v1/logs", svcMgr)
	c.Assert(rec.Code, Equals, http.StatusOK)

	logs := decodeLogs(c, rec.Body)
	c.Assert(logs, HasLen, 10)
	for i := 0; i < 10; i++ {
		if i%2 == 0 {
			checkLog(c, logs[i], "nginx", "stdout", fmt.Sprintf("stdout message %d\n", i+2))
		} else {
			checkLog(c, logs[i], "nginx", "stderr", fmt.Sprintf("stderr message %d\n", i+2))
		}
	}
}

func (s *logsSuite) recordResponse(c *C, url string, svcMgr serviceManager) *httptest.ResponseRecorder {
	req, err := http.NewRequest("GET", url, nil)
	c.Assert(err, IsNil)
	rsp := logsResponse{svcMgr: svcMgr}
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	return rec
}

func getIterator(wb *servicelog.WriteBuffer, last int) servicelog.Iterator {
	if last >= 0 {
		return wb.HeadIterator(last)
	}
	return wb.TailIterator()
}

func decodeLogs(c *C, r io.Reader) []logEntry {
	var entries []logEntry
	for {
		// Read log metadata JSON
		var entry logEntry
		decoder := json.NewDecoder(r)
		err := decoder.Decode(&entry)
		if errors.Is(err, io.EOF) {
			break
		}
		c.Assert(err, IsNil)

		// Read newline separator
		buffered := decoder.Buffered()
		var newline [1]byte
		n, err := buffered.Read(newline[:])
		c.Assert(err, IsNil)
		c.Assert(n, Equals, 1)
		c.Assert(newline[0], Equals, byte('\n'))

		// Concatenate remaining buffer with rest of bytes from reader, and use
		// that to read length message bytes.
		r = io.MultiReader(buffered, r)
		message, err := ioutil.ReadAll(io.LimitReader(r, int64(entry.Length)))
		c.Assert(err, IsNil)
		entry.message = string(message)
		entries = append(entries, entry)
	}
	return entries
}

func checkError(c *C, body []byte, status int, errorMatch string) {
	var rsp struct {
		Type       string
		StatusCode int `json:"status-code"`
		Status     string
		Result     struct {
			Message string
		}
	}
	err := json.Unmarshal(body, &rsp)
	c.Check(err, IsNil)
	c.Check(rsp.Type, Equals, "error")
	c.Check(rsp.StatusCode, Equals, status)
	c.Check(rsp.Status, Equals, http.StatusText(status))
	c.Check(rsp.Result.Message, Matches, errorMatch)
}

func checkLog(c *C, l logEntry, service, stream, message string) {
	c.Check(l.Time, Not(Equals), time.Time{})
	c.Check(l.Service, Equals, "nginx")
	c.Check(l.Stream, Equals, stream)
	c.Check(l.Length, Equals, len(message))
	c.Check(l.message, Equals, message)
}
