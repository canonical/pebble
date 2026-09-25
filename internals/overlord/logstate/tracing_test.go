// Copyright (c) 2026 Canonical Ltd
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
	"sync"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/servicelog"
	"github.com/canonical/pebble/internals/tracing"
	"github.com/canonical/pebble/internals/tracing/tracingtest"
)

type tracingSuite struct {
	recorder *tracingtest.Recorder
}

var _ = Suite(&tracingSuite{})

func (s *tracingSuite) SetUpTest(c *C) {
	s.recorder = tracingtest.NewRecorder()
}

func (s *tracingSuite) TearDownTest(c *C) {
	s.recorder.Restore()
}

// spanClient is a testClient that records the span it's flushed within.
type spanClient struct {
	*testClient
	mu    sync.Mutex
	spans []tracing.SpanContext
}

func (c *spanClient) Flush(ctx context.Context) error {
	c.mu.Lock()
	c.spans = append(c.spans, tracing.SpanContextFromContext(ctx))
	c.mu.Unlock()
	return c.testClient.Flush(ctx)
}

func (s *tracingSuite) TestFlushSpan(c *C) {
	received := make(chan []servicelog.Entry, 1)
	client := &spanClient{testClient: &testClient{bufferSize: 2, sendCh: received}}
	g, err := newLogGathererInternal(&plan.LogTarget{Name: "tgt1", Type: plan.LokiTarget}, &logGathererOptions{
		maxBufferedEntries: 2,
		newClient: func(target *plan.LogTarget) (logClient, error) {
			return client, nil
		},
	})
	c.Assert(err, IsNil)

	testSvc := newTestService("svc1")
	g.ServiceStarted(testSvc.config, testSvc.ringBuffer)
	testSvc.writeLog("log line #1")
	testSvc.writeLog("log line #2")
	select {
	case <-time.After(time.Second):
		c.Fatalf("timeout waiting for logs")
	case <-received:
	}
	// Stopping waits for the flush (and its span) to finish.
	c.Assert(testSvc.stop(), IsNil)
	g.Stop()

	var flushSpan tracingtest.ReadOnlySpan
	for _, span := range s.recorder.Ended() {
		if span.Name() == "flush logs tgt1" && spanAttrs(span)["pebble.log-target.entries"] == int64(2) {
			flushSpan = span
		}
	}
	c.Assert(flushSpan, NotNil)
	c.Check(flushSpan.Parent().IsValid(), Equals, false)
	c.Check(flushSpan.Status().Code, Equals, tracing.StatusUnset)
	attrs := spanAttrs(flushSpan)
	c.Check(attrs["pebble.log-target.name"], Equals, "tgt1")
	c.Check(attrs["pebble.log-target.type"], Equals, "loki")

	// The client is flushed within the span.
	client.mu.Lock()
	defer client.mu.Unlock()
	c.Assert(client.spans, Not(HasLen), 0)
	c.Check(client.spans[0].SpanID(), Equals, flushSpan.SpanContext().SpanID())
}

func spanAttrs(span tracingtest.ReadOnlySpan) map[string]any {
	attrs := make(map[string]any)
	for _, kv := range span.Attributes() {
		attrs[string(kv.Key)] = kv.Value.AsInterface()
	}
	return attrs
}
