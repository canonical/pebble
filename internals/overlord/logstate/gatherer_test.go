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
	"context"
	"fmt"
	"io"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/servicelog"
)

type gathererSuite struct{}

var _ = Suite(&gathererSuite{})

func (s *gathererSuite) TestGatherer(c *C) {
	received := make(chan []servicelog.Entry, 1)
	gathererArgs := logGathererArgs{
		maxBufferedEntries: 5,
		newClient: func(target *plan.LogTarget) (logClient, error) {
			return &testClient{
				bufferSize: 5,
				sendCh:     received,
			}, nil
		},
	}

	g, err := newLogGathererInternal(&plan.LogTarget{Name: "tgt1"}, gathererArgs)
	c.Assert(err, IsNil)

	testSvc := newTestService("svc1")
	g.ServiceStarted(testSvc.config, testSvc.ringBuffer)

	testSvc.writeLog("log line #1")
	testSvc.writeLog("log line #2")
	testSvc.writeLog("log line #3")
	testSvc.writeLog("log line #4")
	select {
	case logs := <-received:
		c.Fatalf("wasn't expecting logs, received %#v", logs)
	default:
	}

	testSvc.writeLog("log line #5")
	select {
	case <-time.After(5 * time.Millisecond):
		c.Fatalf("timeout waiting for logs")
	case logs := <-received:
		checkLogs(c, logs, []string{"log line #1", "log line #2", "log line #3", "log line #4", "log line #5"})
	}
}

func (s *gathererSuite) TestGathererTimeout(c *C) {
	received := make(chan []servicelog.Entry, 1)
	gathererArgs := logGathererArgs{
		bufferTimeout: 1 * time.Millisecond,
		newClient: func(target *plan.LogTarget) (logClient, error) {
			return &testClient{
				bufferSize: 5,
				sendCh:     received,
			}, nil
		},
	}

	g, err := newLogGathererInternal(&plan.LogTarget{Name: "tgt1"}, gathererArgs)
	c.Assert(err, IsNil)

	testSvc := newTestService("svc1")
	g.ServiceStarted(testSvc.config, testSvc.ringBuffer)

	testSvc.writeLog("log line #1")
	select {
	case <-time.After(1 * time.Second):
		c.Fatalf("timeout waiting for logs")
	case logs := <-received:
		checkLogs(c, logs, []string{"log line #1"})
	}
}

func (s *gathererSuite) TestGathererShutdown(c *C) {
	received := make(chan []servicelog.Entry, 1)
	gathererArgs := logGathererArgs{
		bufferTimeout: 1 * time.Microsecond,
		newClient: func(target *plan.LogTarget) (logClient, error) {
			return &testClient{
				bufferSize: 5,
				sendCh:     received,
			}, nil
		},
	}

	g, err := newLogGathererInternal(&plan.LogTarget{Name: "tgt1"}, gathererArgs)
	c.Assert(err, IsNil)

	testSvc := newTestService("svc1")
	g.ServiceStarted(testSvc.config, testSvc.ringBuffer)

	testSvc.writeLog("log line #1")
	err = testSvc.stop()
	c.Assert(err, IsNil)

	hasShutdown := make(chan struct{})
	go func() {
		g.Stop()
		close(hasShutdown)
	}()

	select {
	case <-time.After(100 * time.Millisecond):
		c.Fatalf("timeout waiting for gatherer to tear down")
	case <-hasShutdown:
	}

	// check logs received
	select {
	case logs := <-received:
		checkLogs(c, logs, []string{"log line #1"})
	default:
		c.Fatalf(`no logs were received
logs in client buffer: %v`, len(g.client.(*testClient).buffered))
	}
}

func checkLogs(c *C, received []servicelog.Entry, expected []string) {
	c.Assert(received, HasLen, len(expected))
	for i, entry := range received {
		c.Check(entry.Message, Equals, expected[i]+"\n")
	}
}

// test implementation of a client with buffer
type testClient struct {
	bufferSize int
	buffered   []servicelog.Entry
	sendCh     chan []servicelog.Entry
}

func (c *testClient) Add(entry servicelog.Entry) error {
	c.buffered = append(c.buffered, entry)
	return nil
}

func (c *testClient) NumBuffered() int {
	return len(c.buffered)
}

func (c *testClient) Flush(ctx context.Context) (err error) {
	if len(c.buffered) == 0 {
		return
	}

	select {
	case <-ctx.Done():
		err = fmt.Errorf("timeout flushing, dropping logs")
	case c.sendCh <- c.buffered:
	}

	c.buffered = c.buffered[:0]
	return err
}

// fake "service" - useful for testing
type testService struct {
	name       string
	config     *plan.Service
	ringBuffer *servicelog.RingBuffer
	writer     io.Writer
}

func newTestService(name string) *testService {
	rb := servicelog.NewRingBuffer(1024)
	return &testService{
		name: name,
		config: &plan.Service{
			Name: name,
		},
		ringBuffer: rb,
		writer:     servicelog.NewFormatWriter(rb, "svc1"),
	}
}

func (s *testService) writeLog(log string) {
	_, _ = s.writer.Write([]byte(log + "\n"))
}

func (s *testService) stop() error {
	return s.ringBuffer.Close()
}
