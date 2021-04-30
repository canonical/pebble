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
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
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
	buffers        map[string]*servicelog.WriteBuffer
	servicesErr    error
	serviceLogsErr error
}

func (m testServiceManager) Services(names []string) ([]*servstate.ServiceInfo, error) {
	if len(names) > 0 {
		panic("/v1/logs shouldn't call Services with names specified")
	}
	if m.servicesErr != nil {
		return nil, m.servicesErr
	}
	infos := make([]*servstate.ServiceInfo, 0, len(m.buffers))
	for name := range m.buffers {
		infos = append(infos, &servstate.ServiceInfo{Name: name})
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name < infos[j].Name
	})
	return infos, nil
}

func (m testServiceManager) ServiceLogs(services []string, last int) (map[string]servicelog.Iterator, error) {
	if m.serviceLogsErr != nil {
		return nil, m.serviceLogsErr
	}
	its := make(map[string]servicelog.Iterator)
	for name, wb := range m.buffers {
		for _, s := range services {
			if name == s {
				if last >= 0 {
					its[name] = wb.HeadIterator(last)
				} else {
					its[name] = wb.TailIterator()
				}
				break
			}
		}
	}
	return its, nil
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

func (s *logsSuite) TestServicesError(c *C) {
	svcMgr := testServiceManager{
		servicesErr: fmt.Errorf("Services error!"),
	}
	rec := s.recordResponse(c, "/v1/logs", svcMgr)
	c.Assert(rec.Code, Equals, http.StatusInternalServerError)
	checkError(c, rec.Body.Bytes(), http.StatusInternalServerError, `cannot fetch services: Services error!`)
}

func (s *logsSuite) TestServiceLogsError(c *C) {
	svcMgr := testServiceManager{
		serviceLogsErr: fmt.Errorf("ServiceLogs error!"),
	}
	rec := s.recordResponse(c, "/v1/logs", svcMgr)
	c.Assert(rec.Code, Equals, http.StatusInternalServerError)
	checkError(c, rec.Body.Bytes(), http.StatusInternalServerError, `cannot fetch log iterators: ServiceLogs error!`)
}

func (s *logsSuite) TestOneServiceDefaults(c *C) {
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
		buffers: map[string]*servicelog.WriteBuffer{
			"nginx": wb,
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

func (s *logsSuite) TestOneServiceWithN(c *C) {
	wb := servicelog.NewWriteBuffer(10, 1024)
	stdout := wb.StreamWriter(servicelog.Stdout)
	for i := 0; i < 20; i++ {
		fmt.Fprintf(stdout, "message %d\n", i)
	}

	svcMgr := testServiceManager{
		buffers: map[string]*servicelog.WriteBuffer{
			"nginx": wb,
		},
	}
	rec := s.recordResponse(c, "/v1/logs?n=3", svcMgr)
	c.Assert(rec.Code, Equals, http.StatusOK)

	logs := decodeLogs(c, rec.Body)
	c.Assert(logs, HasLen, 3)
	for i := 0; i < 3; i++ {
		checkLog(c, logs[i], "nginx", "stdout", fmt.Sprintf("message %d\n", i+17))
	}
}

func (s *logsSuite) TestOneServiceAllLogs(c *C) {
	wb := servicelog.NewWriteBuffer(20, 1024)
	stdout := wb.StreamWriter(servicelog.Stdout)
	for i := 0; i < 40; i++ {
		fmt.Fprintf(stdout, "message %d\n", i)
	}

	svcMgr := testServiceManager{
		buffers: map[string]*servicelog.WriteBuffer{
			"nginx": wb,
		},
	}
	rec := s.recordResponse(c, "/v1/logs?n=-1", svcMgr)
	c.Assert(rec.Code, Equals, http.StatusOK)

	logs := decodeLogs(c, rec.Body)
	c.Assert(logs, HasLen, 20)
	for i := 0; i < 20; i++ {
		checkLog(c, logs[i], "nginx", "stdout", fmt.Sprintf("message %d\n", i+20))
	}
}

func (s *logsSuite) TestOneServiceOutOfTwo(c *C) {
	wb := servicelog.NewWriteBuffer(10, 1024)
	stdout := wb.StreamWriter(servicelog.Stdout)
	for i := 0; i < 20; i++ {
		fmt.Fprintf(stdout, "message %d\n", i)
	}

	svcMgr := testServiceManager{
		buffers: map[string]*servicelog.WriteBuffer{
			"nginx":  wb,
			"unused": nil,
		},
	}
	rec := s.recordResponse(c, "/v1/logs?n=3&services=nginx", svcMgr)
	c.Assert(rec.Code, Equals, http.StatusOK)

	logs := decodeLogs(c, rec.Body)
	c.Assert(logs, HasLen, 3)
	for i := 0; i < 3; i++ {
		checkLog(c, logs[i], "nginx", "stdout", fmt.Sprintf("message %d\n", i+17))
	}
}

func (s *logsSuite) TestNoLogs(c *C) {
	svcMgr := testServiceManager{
		buffers: map[string]*servicelog.WriteBuffer{
			"foo": servicelog.NewWriteBuffer(10, 10),
			"bar": servicelog.NewWriteBuffer(10, 10),
		},
	}
	rec := s.recordResponse(c, "/v1/logs", svcMgr)
	c.Assert(rec.Code, Equals, http.StatusOK)

	logs := decodeLogs(c, rec.Body)
	c.Assert(logs, HasLen, 0)
}

func (s *logsSuite) TestZeroN(c *C) {
	svcMgr := testServiceManager{
		buffers: map[string]*servicelog.WriteBuffer{
			"foo": servicelog.NewWriteBuffer(10, 10),
			"bar": servicelog.NewWriteBuffer(10, 10),
		},
	}
	rec := s.recordResponse(c, "/v1/logs?n=0", svcMgr)
	c.Assert(rec.Code, Equals, http.StatusOK)

	logs := decodeLogs(c, rec.Body)
	c.Assert(logs, HasLen, 0)
}

func (s *logsSuite) TestMultipleServicesAll(c *C) {
	wb1 := servicelog.NewWriteBuffer(10, 1024)
	wb2 := servicelog.NewWriteBuffer(10, 1024)
	stdout1 := wb1.StreamWriter(servicelog.Stdout)
	stdout2 := wb2.StreamWriter(servicelog.Stdout)
	for i := 0; i < 10; i++ {
		fmt.Fprintf(stdout1, "message1 %d\n", i)
		time.Sleep(time.Millisecond)
		fmt.Fprintf(stdout2, "message2 %d\n", i)
		time.Sleep(time.Millisecond)
	}

	svcMgr := testServiceManager{
		buffers: map[string]*servicelog.WriteBuffer{
			"one": wb1,
			"two": wb2,
		},
	}
	rec := s.recordResponse(c, "/v1/logs?n=-1", svcMgr)
	c.Assert(rec.Code, Equals, http.StatusOK)

	logs := decodeLogs(c, rec.Body)
	c.Assert(logs, HasLen, 20)
	for i := 0; i < 10; i++ {
		checkLog(c, logs[i*2], "one", "stdout", fmt.Sprintf("message1 %d\n", i))
		checkLog(c, logs[i*2+1], "two", "stdout", fmt.Sprintf("message2 %d\n", i))
	}
}

func (s *logsSuite) TestMultipleServicesN(c *C) {
	wb1 := servicelog.NewWriteBuffer(10, 1024)
	wb2 := servicelog.NewWriteBuffer(10, 1024)
	stdout1 := wb1.StreamWriter(servicelog.Stdout)
	stdout2 := wb2.StreamWriter(servicelog.Stdout)
	for i := 0; i < 10; i++ {
		fmt.Fprintf(stdout1, "message1 %d\n", i)
		time.Sleep(time.Millisecond)
		fmt.Fprintf(stdout2, "message2 %d\n", i)
		time.Sleep(time.Millisecond)
	}

	svcMgr := testServiceManager{
		buffers: map[string]*servicelog.WriteBuffer{
			"one": wb1,
			"two": wb2,
		},
	}
	rec := s.recordResponse(c, "/v1/logs", svcMgr)
	c.Assert(rec.Code, Equals, http.StatusOK)

	logs := decodeLogs(c, rec.Body)
	c.Assert(logs, HasLen, 10)
	for i := 0; i < 5; i++ {
		checkLog(c, logs[i*2], "one", "stdout", fmt.Sprintf("message1 %d\n", 5+i))
		checkLog(c, logs[i*2+1], "two", "stdout", fmt.Sprintf("message2 %d\n", 5+i))
	}
}

func (s *logsSuite) TestMultipleServicesNFewLogs(c *C) {
	wb1 := servicelog.NewWriteBuffer(10, 1024)
	wb2 := servicelog.NewWriteBuffer(10, 1024)
	stdout1 := wb1.StreamWriter(servicelog.Stdout)
	stdout2 := wb2.StreamWriter(servicelog.Stdout)
	fmt.Fprintf(stdout1, "message1 1\n")
	time.Sleep(time.Millisecond)
	fmt.Fprintf(stdout2, "message2 1\n")
	time.Sleep(time.Millisecond)

	svcMgr := testServiceManager{
		buffers: map[string]*servicelog.WriteBuffer{
			"one": wb1,
			"two": wb2,
		},
	}
	rec := s.recordResponse(c, "/v1/logs", svcMgr)
	c.Assert(rec.Code, Equals, http.StatusOK)

	logs := decodeLogs(c, rec.Body)
	c.Assert(logs, HasLen, 2)
	checkLog(c, logs[0], "one", "stdout", "message1 1\n")
	checkLog(c, logs[1], "two", "stdout", "message2 1\n")
}

func (s *logsSuite) TestMultipleServicesFollow(c *C) {
	wb1 := servicelog.NewWriteBuffer(10, 1024)
	wb2 := servicelog.NewWriteBuffer(10, 1024)
	stdout1 := wb1.StreamWriter(servicelog.Stdout)
	stdout2 := wb2.StreamWriter(servicelog.Stdout)
	fmt.Fprintf(stdout1, "message1 1\n")
	time.Sleep(time.Millisecond)
	fmt.Fprintf(stdout2, "message2 1\n")
	time.Sleep(time.Millisecond)

	svcMgr := testServiceManager{
		buffers: map[string]*servicelog.WriteBuffer{
			"one": wb1,
			"two": wb2,
		},
	}

	// Start a cancellable request
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, "GET", "/v1/logs?follow=true", nil)
	c.Assert(err, IsNil)
	rsp := logsResponse{svcMgr: svcMgr}

	// writeChan is sent to whenever a response write occurs
	logChan := make(chan string)
	rec := &followRecorder{logChan: logChan}

	// Serve the request on a background goroutine
	done := make(chan struct{})
	go func() {
		rsp.ServeHTTP(rec, req)
		done <- struct{}{}
	}()

	waitLog := func() logEntry {
		select {
		case log := <-logChan:
			reader := bufio.NewReader(strings.NewReader(log))
			entry, ok := decodeLog(c, reader)
			c.Check(ok, Equals, true)
			return entry
		case <-time.After(1 * time.Second):
			c.Fatalf("timed out waiting for log")
			return logEntry{}
		}
	}

	// The two logs written before the request should be there
	checkLog(c, waitLog(), "one", "stdout", "message1 1\n")
	checkLog(c, waitLog(), "two", "stdout", "message2 1\n")

	// Then write a bunch more and ensure we can "follow" them
	fmt.Fprintf(stdout1, "message1 2\n")
	checkLog(c, waitLog(), "one", "stdout", "message1 2\n")
	fmt.Fprintf(stdout2, "message2 2\n")
	checkLog(c, waitLog(), "two", "stdout", "message2 2\n")
	fmt.Fprintf(stdout2, "message2 3\n")
	checkLog(c, waitLog(), "two", "stdout", "message2 3\n")
	fmt.Fprintf(stdout1, "message1 3\n")
	checkLog(c, waitLog(), "one", "stdout", "message1 3\n")

	// Close request and wait till serve goroutine exits
	cancel()
	<-done
	c.Assert(rec.status, Equals, http.StatusOK)
}

type followRecorder struct {
	logChan chan string
	header  http.Header
	buf     bytes.Buffer
	status  int
}

func (r *followRecorder) Header() http.Header {
	if r.header == nil {
		r.header = make(http.Header)
	}
	return r.header
}

func (r *followRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = 200
	}
	n, err := r.buf.Write(p)
	if len(p) > 0 && p[0] != '{' {
		// If the message bytes were just written, send complete log bytes
		r.logChan <- r.buf.String()
		r.buf.Reset()
	}
	return n, err
}

func (r *followRecorder) WriteHeader(status int) {
	r.status = status
}

func (s *logsSuite) recordResponse(c *C, url string, svcMgr serviceManager) *httptest.ResponseRecorder {
	req, err := http.NewRequest("GET", url, nil)
	c.Assert(err, IsNil)
	rsp := logsResponse{svcMgr: svcMgr}
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		c.Assert(rec.Header().Get("Content-Type"), Equals, "text/plain; charset=utf-8")
	} else {
		c.Assert(rec.Header().Get("Content-Type"), Equals, "application/json")
	}
	return rec
}

func decodeLog(c *C, reader *bufio.Reader) (logEntry, bool) {
	// Read log metadata JSON and newline separator
	metaBytes, err := reader.ReadSlice('\n')
	if err == io.EOF {
		return logEntry{}, false
	}
	c.Assert(err, IsNil)
	var entry logEntry
	err = json.Unmarshal(metaBytes, &entry)
	c.Assert(err, IsNil)

	// Read message bytes
	message, err := ioutil.ReadAll(io.LimitReader(reader, int64(entry.Length)))
	c.Assert(err, IsNil)
	entry.message = string(message)
	return entry, true
}

func decodeLogs(c *C, r io.Reader) []logEntry {
	var entries []logEntry
	reader := bufio.NewReader(r)
	for {
		entry, ok := decodeLog(c, reader)
		if !ok {
			break
		}
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
	c.Check(l.Service, Equals, service)
	c.Check(l.Stream, Equals, stream)
	c.Check(l.Length, Equals, len(message))
	c.Check(l.message, Equals, message)
}
