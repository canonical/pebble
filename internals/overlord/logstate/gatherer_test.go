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
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/logstate/loki"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/servicelog"
)

type gathererSuite struct{}

var _ = Suite(&gathererSuite{})

func (s *gathererSuite) TestGatherer(c *C) {
	received := make(chan []servicelog.Entry, 1)
	gathererOptions := logGathererOptions{
		maxBufferedEntries: 5,
		newClient: func(target *plan.LogTarget) (logClient, error) {
			return &testClient{
				bufferSize: 5,
				sendCh:     received,
			}, nil
		},
	}

	g, err := newLogGathererInternal(&plan.LogTarget{Name: "tgt1"}, &gathererOptions)
	c.Assert(err, IsNil)

	testSvc := newTestService("svc1")
	g.BufferChanged(testSvc.name, testSvc.ringBuffer)

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
	case <-time.After(1 * time.Second):
		c.Fatalf("timeout waiting for logs")
	case logs := <-received:
		checkLogs(c, logs, []string{"log line #1", "log line #2", "log line #3", "log line #4", "log line #5"})
	}
}

func (s *gathererSuite) TestGathererTimeout(c *C) {
	received := make(chan []servicelog.Entry, 1)
	gathererOptions := logGathererOptions{
		bufferTimeout: 1 * time.Millisecond,
		newClient: func(target *plan.LogTarget) (logClient, error) {
			return &testClient{
				bufferSize: 5,
				sendCh:     received,
			}, nil
		},
	}

	g, err := newLogGathererInternal(&plan.LogTarget{Name: "tgt1"}, &gathererOptions)
	c.Assert(err, IsNil)

	testSvc := newTestService("svc1")
	g.BufferChanged(testSvc.name, testSvc.ringBuffer)

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
	gathererOptions := logGathererOptions{
		bufferTimeout: 1 * time.Microsecond,
		newClient: func(target *plan.LogTarget) (logClient, error) {
			return &testClient{
				bufferSize: 5,
				sendCh:     received,
			}, nil
		},
	}

	g, err := newLogGathererInternal(&plan.LogTarget{Name: "tgt1"}, &gathererOptions)
	c.Assert(err, IsNil)

	testSvc := newTestService("svc1")
	g.BufferChanged(testSvc.name, testSvc.ringBuffer)

	testSvc.writeLog("log line #1")
	err = testSvc.stop()
	c.Assert(err, IsNil)

	hasShutdown := make(chan struct{})
	go func() {
		g.Stop()
		close(hasShutdown)
	}()

	select {
	case <-time.After(1 * time.Second):
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

func (s *gathererSuite) TestRetryLoki(c *C) {
	var handler func(http.ResponseWriter, *http.Request)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler(w, r)
	}))
	defer server.Close()

	g, err := newLogGathererInternal(
		&plan.LogTarget{
			Name:     "tgt1",
			Location: server.URL,
		},
		&logGathererOptions{
			bufferTimeout:      1 * time.Millisecond,
			maxBufferedEntries: 5,
			newClient: func(target *plan.LogTarget) (logClient, error) {
				return loki.NewClientWithOptions(target, &loki.ClientOptions{
					MaxRequestEntries: 5,
				}), nil
			},
		},
	)
	c.Assert(err, IsNil)

	testSvc := newTestService("svc1")
	g.BufferChanged(testSvc.name, testSvc.ringBuffer)

	reqReceived := make(chan struct{})
	// First attempt: server should return a retryable error
	handler = func(w http.ResponseWriter, _ *http.Request) {
		close(reqReceived)
		w.WriteHeader(http.StatusTooManyRequests)
	}

	testSvc.writeLog("log line #1")
	testSvc.writeLog("log line #2")
	testSvc.writeLog("log line #3")
	testSvc.writeLog("log line #4")
	testSvc.writeLog("log line #5")

	// Check that request was received
	select {
	case <-reqReceived:
	case <-time.After(1 * time.Second):
		c.Fatalf("timed out waiting for request")
	}

	reqReceived = make(chan struct{})
	// Second attempt: check that logs were held over from last time
	handler = func(w http.ResponseWriter, r *http.Request) {
		close(reqReceived)
		reqBody, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)

		expected := `{"streams":\[{"stream":{"pebble_service":"svc1"},"values":\[` +
			// First two log lines should have been truncated
			`\["\d+","log line #3"\],` +
			`\["\d+","log line #4"\],` +
			`\["\d+","log line #5"\],` +
			`\["\d+","log line #6"\],` +
			`\["\d+","log line #7"\]` +
			`\]}\]}`
		c.Assert(string(reqBody), Matches, expected)
	}

	testSvc.writeLog("log line #6")
	testSvc.writeLog("log line #7")
	// Wait for flush timeout to elapse

	// Check that request was received
	select {
	case <-reqReceived:
	case <-time.After(1 * time.Second):
		c.Fatalf("timed out waiting for request")
	}
}

// Test to catch race conditions in gatherer
func (s *gathererSuite) TestRace(c *C) {
	target := &plan.LogTarget{
		Name:     "tgt1",
		Type:     plan.LokiTarget,
		Services: []string{"all"},
		Labels:   map[string]string{"foo": "bar-$SECRET-$SECRET2", "baz": "foo"},
	}

	g, err := newLogGathererInternal(target, &logGathererOptions{
		maxBufferedEntries: 2,
	})
	c.Assert(err, IsNil)

	svc1 := newTestService("svc1")
	svc2 := newTestService("svc2")
	fakeEnv := map[string]string{
		"SECRET":  "pie",
		"SECRET2": "pizza",
	}
	svc1.env = fakeEnv
	svc2.env = fakeEnv

	serviceData := map[string]*ServiceData{
		svc1.name: svc1.data(),
		svc2.name: svc2.data(),
	}

	wg := sync.WaitGroup{}
	doAsync := func(f func()) {
		wg.Add(1)
		go func() {
			f()
			wg.Done()
		}()
	}

	doAsync(func() {
		g.PlanChanged(&plan.Plan{
			Services: map[string]*plan.Service{
				svc1.name: svc1.config,
			},
			LogTargets: map[string]*plan.LogTarget{
				target.Name: target,
			},
		}, serviceData)
	})

	doAsync(func() {
		g.EnvChanged(svc1.name, svc1.env)
		g.BufferChanged(svc1.name, svc1.ringBuffer)
	})

	doAsync(func() {
		svc1.writeLog("hello")
		svc1.writeLog("goodbye")
	})

	// Simulate a service restart
	doAsync(func() {
		g.EnvChanged(svc1.name, svc1.env)
		g.BufferChanged(svc1.name, svc1.ringBuffer)
	})

	doAsync(func() {
		g.PlanChanged(&plan.Plan{
			Services: map[string]*plan.Service{
				svc2.name: svc2.config,
			},
			LogTargets: map[string]*plan.LogTarget{
				target.Name: target,
			},
		}, serviceData)
	})

	doAsync(func() {
		g.EnvChanged(svc2.name, svc2.env)
		g.BufferChanged(svc2.name, svc2.ringBuffer)
	})

	doAsync(func() {
		svc2.writeLog("hello")
		go svc2.writeLog("goodbye")
	})

	// Wait for everything to finish before calling Stop
	wg.Wait()

	err = svc1.stop()
	c.Assert(err, IsNil)
	err = svc2.stop()
	c.Assert(err, IsNil)
	g.Stop()
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

func (c *testClient) SetLabels(serviceName string, labels map[string]string) {
	// no-op
}

func (c *testClient) Add(entry servicelog.Entry) error {
	c.buffered = append(c.buffered, entry)
	return nil
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
	env        map[string]string
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

func (s *testService) data() *ServiceData {
	return &ServiceData{
		Buffer: s.ringBuffer,
		Env:    s.env,
	}
}
