// Copyright (c) 2023 Canonical Ltd
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

package logstate

import (
	"fmt"
	"strings"
	"time"

	"github.com/canonical/pebble/internal/servicelog"
	. "gopkg.in/check.v1"
)

type forwarderSuite struct{}

var _ = Suite(&forwarderSuite{})

func (s *forwarderSuite) TestForwarder(c *C) {
	serviceName := "foobar"
	rb := servicelog.NewRingBuffer(1024)
	it := rb.HeadIterator(0)
	w := servicelog.NewFormatWriter(rb, serviceName)

	cl := &fakeLogClient{make(chan []string, 1)}
	f := newLogForwarderForTest(serviceName, it, cl, 10*time.Millisecond)

	forwardStopped := make(chan struct{})
	go func() {
		f.forward()
		close(forwardStopped)
	}()

	writeLog := func(logLine string) {
		_, err := fmt.Fprintln(w, logLine)
		c.Assert(err, IsNil)
	}
	expectLogs := func(request []string) {
		select {
		case req := <-cl.requests:
			c.Assert(req, DeepEquals, request)
		case <-time.After(1 * time.Second):
			c.Fatalf("timed out waiting for request %q", request)
		}
	}

	writeLog("log line #1")
	writeLog("log line #2")
	writeLog("log line #3")
	expectLogs([]string{"log line #1", "log line #2", "log line #3"})

	writeLog("log line #4")
	expectLogs([]string{"log line #4"})

	writeLog("log line #5")
	writeLog("log line #6")
	expectLogs([]string{"log line #5", "log line #6"})

	writeLog("log line #7")
	f.stop()
	// Check that f.forward() has returned
	select {
	case <-forwardStopped:
	case <-time.After(1 * time.Second):
		c.Fatalf("timed out waiting for f.forward() to return")
	}

	expectLogs([]string{"log line #7"})
}

func (s *forwarderSuite) TestBufferFull(c *C) {
	serviceName := "foobar"
	rb := servicelog.NewRingBuffer(1024)
	it := rb.HeadIterator(0)
	w := servicelog.NewFormatWriter(rb, serviceName)

	cl := &fakeLogClient{make(chan []string)}
	f := newLogForwarderForTest(serviceName, it, cl, 1*time.Second)
	go f.forward()

	writeLog := func(logLine string) {
		_, err := fmt.Fprintln(w, logLine)
		c.Assert(err, IsNil)
	}

	writeLog("log line #1")
	writeLog("log line #2")
	writeLog("log line #3")
	writeLog("log line #4")
	writeLog("log line #5")

	select {
	case req := <-cl.requests:
		c.Assert(req, DeepEquals, []string{"log line #1", "log line #2", "log line #3", "log line #4", "log line #5"})
	case <-time.After(100 * time.Millisecond):
		c.Fatalf("timed out waiting for request")
	}
}

type fakeLogClient struct {
	requests chan []string
}

func (c *fakeLogClient) Send(entries []servicelog.Entry) error {
	request := make([]string, 0, len(entries))
	for _, e := range entries {
		request = append(request, strings.TrimRight(e.Message, "\n"))
	}
	c.requests <- request
	return nil
}

func (c *fakeLogClient) Close() error {
	return nil
}

func newLogForwarderForTest(
	service string, iterator servicelog.Iterator,
	client logClient, delay time.Duration,
) *logForwarder {
	return newLogForwarderInternal(service, nil, iterator, client, 5, delay)
}
