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
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/servstate"
	"github.com/canonical/pebble/internals/servicelog"
)

var _ = Suite(&logsSuite{})

type logsSuite struct{}

type testLogEntry struct {
	Time    time.Time
	Service string
	Message string
}

type testServiceManager struct {
	buffers        map[string]*servicelog.RingBuffer
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
	checkError(c, rec.Body.Bytes(), http.StatusBadRequest, `n must be -1, 0, or a positive integer`)

	rec = s.recordResponse(c, "/v1/logs?n=-2", nil)
	c.Assert(rec.Code, Equals, http.StatusBadRequest)
	checkError(c, rec.Body.Bytes(), http.StatusBadRequest, `n must be -1, 0, or a positive integer`)
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
	rb := servicelog.NewRingBuffer(4096)
	lw := servicelog.NewFormatWriter(rb, "nginx")
	for i := 0; i < 32; i++ {
		fmt.Fprintf(lw, "message %d\n", i)
	}
	fmt.Fprintf(lw, "truncated")

	svcMgr := testServiceManager{
		buffers: map[string]*servicelog.RingBuffer{
			"nginx": rb,
		},
	}
	rec := s.recordResponse(c, "/v1/logs", svcMgr)
	c.Assert(rec.Code, Equals, http.StatusOK)

	logs := decodeLogs(c, rec.Body)
	c.Assert(logs, HasLen, 30)
	for i := 0; i < 29; i++ {
		checkLog(c, logs[i], "nginx", fmt.Sprintf("message %d", i+3))
	}
	c.Check(logs[29].Time, Not(Equals), time.Time{})
	c.Check(logs[29].Service, Equals, "nginx")
	c.Check(logs[29].Message, Equals, "truncated")
}

func (s *logsSuite) TestOneServiceWithN(c *C) {
	rb := servicelog.NewRingBuffer(4096)
	lw := servicelog.NewFormatWriter(rb, "nginx")
	for i := 0; i < 20; i++ {
		fmt.Fprintf(lw, "message %d\n", i)
	}

	svcMgr := testServiceManager{
		buffers: map[string]*servicelog.RingBuffer{
			"nginx": rb,
		},
	}
	rec := s.recordResponse(c, "/v1/logs?n=3", svcMgr)
	c.Assert(rec.Code, Equals, http.StatusOK)

	logs := decodeLogs(c, rec.Body)
	c.Assert(logs, HasLen, 3)
	for i := 0; i < 3; i++ {
		checkLog(c, logs[i], "nginx", fmt.Sprintf("message %d", i+17))
	}
}

func (s *logsSuite) TestOneServiceAllLogs(c *C) {
	exampleLog := "2021-05-20T16:55:00.000Z [nginx] message 00\n"
	rb := servicelog.NewRingBuffer(len(exampleLog) * 20)
	lw := servicelog.NewFormatWriter(rb, "nginx")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(lw, "message %02d\n", i)
	}

	svcMgr := testServiceManager{
		buffers: map[string]*servicelog.RingBuffer{
			"nginx": rb,
		},
	}
	rec := s.recordResponse(c, "/v1/logs?n=-1", svcMgr)
	c.Assert(rec.Code, Equals, http.StatusOK)

	logs := decodeLogs(c, rec.Body)
	c.Assert(logs, HasLen, 20)
	for i := 0; i < 20; i++ {
		checkLog(c, logs[i], "nginx", fmt.Sprintf("message %d", i+20))
	}
}

func (s *logsSuite) TestOneServiceOutOfTwo(c *C) {
	rb := servicelog.NewRingBuffer(4096)
	lw := servicelog.NewFormatWriter(rb, "nginx")
	for i := 0; i < 20; i++ {
		fmt.Fprintf(lw, "message %d\n", i)
	}

	svcMgr := testServiceManager{
		buffers: map[string]*servicelog.RingBuffer{
			"nginx":  rb,
			"unused": nil,
		},
	}
	rec := s.recordResponse(c, "/v1/logs?n=3&services=nginx", svcMgr)
	c.Assert(rec.Code, Equals, http.StatusOK)

	logs := decodeLogs(c, rec.Body)
	c.Assert(logs, HasLen, 3)
	for i := 0; i < 3; i++ {
		checkLog(c, logs[i], "nginx", fmt.Sprintf("message %d", i+17))
	}
}

func (s *logsSuite) TestNoLogs(c *C) {
	svcMgr := testServiceManager{
		buffers: map[string]*servicelog.RingBuffer{
			"foo": servicelog.NewRingBuffer(1),
			"bar": servicelog.NewRingBuffer(1),
		},
	}
	for _, url := range []string{"/v1/logs", "/v1/logs?n=0"} {
		rec := s.recordResponse(c, url, svcMgr)
		c.Assert(rec.Code, Equals, http.StatusOK)
		logs := decodeLogs(c, rec.Body)
		c.Assert(logs, HasLen, 0)
	}
}

func (s *logsSuite) TestMultipleServicesAll(c *C) {
	rb1 := servicelog.NewRingBuffer(4096)
	rb2 := servicelog.NewRingBuffer(4096)
	lw1 := servicelog.NewFormatWriter(rb1, "one")
	lw2 := servicelog.NewFormatWriter(rb2, "two")
	for i := 0; i < 10; i++ {
		fmt.Fprintf(lw1, "message1 %d\n", i)
		time.Sleep(time.Millisecond)
		fmt.Fprintf(lw2, "message2 %d\n", i)
		time.Sleep(time.Millisecond)
	}

	svcMgr := testServiceManager{
		buffers: map[string]*servicelog.RingBuffer{
			"one": rb1,
			"two": rb2,
		},
	}
	rec := s.recordResponse(c, "/v1/logs?n=-1", svcMgr)
	c.Assert(rec.Code, Equals, http.StatusOK)

	logs := decodeLogs(c, rec.Body)
	c.Assert(logs, HasLen, 20)
	for i := 0; i < 10; i++ {
		checkLog(c, logs[i*2], "one", fmt.Sprintf("message1 %d", i))
		checkLog(c, logs[i*2+1], "two", fmt.Sprintf("message2 %d", i))
	}
}

func (s *logsSuite) TestMultipleServicesN(c *C) {
	rb1 := servicelog.NewRingBuffer(4096)
	rb2 := servicelog.NewRingBuffer(4096)
	lw1 := servicelog.NewFormatWriter(rb1, "one")
	lw2 := servicelog.NewFormatWriter(rb2, "two")
	for i := 0; i < 30; i++ {
		fmt.Fprintf(lw1, "message1 %d\n", i)
		time.Sleep(time.Millisecond)
		fmt.Fprintf(lw2, "message2 %d\n", i)
		time.Sleep(time.Millisecond)
	}

	svcMgr := testServiceManager{
		buffers: map[string]*servicelog.RingBuffer{
			"one": rb1,
			"two": rb2,
		},
	}
	rec := s.recordResponse(c, "/v1/logs", svcMgr)
	c.Assert(rec.Code, Equals, http.StatusOK)

	logs := decodeLogs(c, rec.Body)
	c.Assert(logs, HasLen, 30)
	for i := 0; i < 15; i++ {
		checkLog(c, logs[i*2], "one", fmt.Sprintf("message1 %d", 15+i))
		checkLog(c, logs[i*2+1], "two", fmt.Sprintf("message2 %d", 15+i))
	}
}

func (s *logsSuite) TestMultipleServicesNFewLogs(c *C) {
	rb1 := servicelog.NewRingBuffer(4096)
	rb2 := servicelog.NewRingBuffer(4096)
	lw1 := servicelog.NewFormatWriter(rb1, "one")
	lw2 := servicelog.NewFormatWriter(rb2, "two")
	fmt.Fprintf(lw1, "message1 1\n")
	time.Sleep(time.Millisecond)
	fmt.Fprintf(lw2, "message2 1\n")
	time.Sleep(time.Millisecond)

	svcMgr := testServiceManager{
		buffers: map[string]*servicelog.RingBuffer{
			"one": rb1,
			"two": rb2,
		},
	}
	rec := s.recordResponse(c, "/v1/logs", svcMgr)
	c.Assert(rec.Code, Equals, http.StatusOK)

	logs := decodeLogs(c, rec.Body)
	c.Assert(logs, HasLen, 2)
	checkLog(c, logs[0], "one", "message1 1")
	checkLog(c, logs[1], "two", "message2 1")
}

func (s *logsSuite) TestLoggingTooFast(c *C) {
	rb := servicelog.NewRingBuffer(1024)
	lw := servicelog.NewFormatWriter(rb, "svc")

	// We should only receive these first three logs
	for i := 0; i < 3; i++ {
		fmt.Fprintf(lw, "message %d\n", i)
	}

	firstWrite := make(chan struct{}, 20)
	go func() {
		<-firstWrite                      // wait till after first log written
		time.Sleep(10 * time.Millisecond) // ensure timestamp changes
		for i := 3; i < 10; i++ {
			fmt.Fprintf(lw, "message %d\n", i)
		}
	}()

	svcMgr := testServiceManager{
		buffers: map[string]*servicelog.RingBuffer{
			"svc": rb,
		},
	}
	req, err := http.NewRequest("GET", "/v1/logs", nil)
	c.Assert(err, IsNil)
	rsp := logsResponse{svcMgr: svcMgr}
	rec := &responseRecorder{onWrite: func() {
		firstWrite <- struct{}{}
	}}
	rsp.ServeHTTP(rec, req)
	c.Assert(rec.status, Equals, http.StatusOK)

	logs := decodeLogs(c, bytes.NewReader(rec.buf.Bytes()))
	c.Assert(len(logs), Equals, 3)
	for i := 0; i < 3; i++ {
		checkLog(c, logs[i], "svc", fmt.Sprintf("message %d", i))
	}
}

type responseRecorder struct {
	onWrite func()
	header  http.Header
	buf     bytes.Buffer
	status  int
}

func (r *responseRecorder) Header() http.Header {
	if r.header == nil {
		r.header = make(http.Header)
	}
	return r.header
}

func (r *responseRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = 200
	}
	r.onWrite()
	return r.buf.Write(p)
}

func (r *responseRecorder) WriteHeader(status int) {
	r.status = status
}

func (s *logsSuite) TestMultipleServicesFollow(c *C) {
	rb1 := servicelog.NewRingBuffer(4096)
	rb2 := servicelog.NewRingBuffer(4096)
	lw1 := servicelog.NewFormatWriter(rb1, "one")
	lw2 := servicelog.NewFormatWriter(rb2, "two")
	fmt.Fprintf(lw1, "message1 1\n")
	time.Sleep(time.Millisecond)
	fmt.Fprintf(lw2, "message2 1\n")
	time.Sleep(time.Millisecond)

	svcMgr := testServiceManager{
		buffers: map[string]*servicelog.RingBuffer{
			"one": rb1,
			"two": rb2,
		},
	}

	// Start a cancellable request
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, "GET", "/v1/logs?follow=true&n=2", nil)
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

	waitLogs := func() []testLogEntry {
		select {
		case logsStr := <-logChan:
			return decodeLogs(c, strings.NewReader(logsStr))
		case <-time.After(1 * time.Second):
			c.Fatalf("timed out waiting for log")
			return nil
		}
	}
	waitLog := func() testLogEntry {
		logs := waitLogs()
		c.Assert(logs, HasLen, 1)
		return logs[0]
	}

	// The two logs written before the request should be there
	logs := waitLogs()
	if len(logs) == 1 {
		logs = append(logs, waitLog())
	}
	c.Check(logs, HasLen, 2)
	checkLog(c, logs[0], "one", "message1 1")
	checkLog(c, logs[1], "two", "message2 1")

	// Then write a bunch more and ensure we can "follow" them
	time.Sleep(10 * time.Millisecond) // ensure we'll be using the notification channel
	fmt.Fprintf(lw1, "message1 2\n")
	checkLog(c, waitLog(), "one", "message1 2")
	fmt.Fprintf(lw2, "message2 2\n")
	checkLog(c, waitLog(), "two", "message2 2")
	fmt.Fprintf(lw2, "message2 3\n")
	checkLog(c, waitLog(), "two", "message2 3")
	fmt.Fprintf(lw1, "message1 3\n")
	checkLog(c, waitLog(), "one", "message1 3")

	// Close request and wait till serve goroutine exits
	cancel()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		c.Fatalf("timed out waiting for request to be finished")
	}
	c.Assert(rec.status, Equals, http.StatusOK)
}

type followRecorder struct {
	logChan chan string
	header  http.Header
	status  int
	written []byte
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
	r.written = append(r.written, p...)
	return len(p), nil
}

func (r *followRecorder) WriteHeader(status int) {
	r.status = status
}

func (r *followRecorder) Flush() {
	r.logChan <- string(r.written)
	r.written = r.written[:0]
}

func (s *logsSuite) recordResponse(c *C, url string, svcMgr serviceManager) *httptest.ResponseRecorder {
	req, err := http.NewRequest("GET", url, nil)
	c.Assert(err, IsNil)
	rsp := logsResponse{svcMgr: svcMgr}
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		c.Assert(rec.Header().Get("Content-Type"), Equals, "application/x-ndjson")
	} else {
		c.Assert(rec.Header().Get("Content-Type"), Equals, "application/json")
	}
	return rec
}

func decodeLog(c *C, reader *bufio.Reader) (testLogEntry, bool) {
	// Read log metadata JSON and newline separator
	logBytes, err := reader.ReadSlice('\n')
	if err == io.EOF {
		return testLogEntry{}, false
	}
	c.Assert(err, IsNil)
	var entry testLogEntry
	err = json.Unmarshal(logBytes, &entry)
	c.Assert(err, IsNil)
	return entry, true
}

func decodeLogs(c *C, r io.Reader) []testLogEntry {
	var entries []testLogEntry
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

func checkLog(c *C, l testLogEntry, service, message string) {
	c.Check(l.Time, Not(Equals), time.Time{})
	c.Check(l.Service, Equals, service)
	c.Check(l.Message, Equals, message)
}
