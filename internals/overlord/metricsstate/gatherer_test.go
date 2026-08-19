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

package metricsstate

import (
	"context"
	"sync"
	"time"

	. "gopkg.in/check.v1"

	metricspb "github.com/canonical/pebble/internals/otlp/metrics/v1"
	"github.com/canonical/pebble/internals/plan"
)

type gathererSuite struct{}

var _ = Suite(&gathererSuite{})

// testClient is a fake metricsClient implementation used for testing the
// gatherer's buffering and flushing behaviour.
type testClient struct {
	mu      sync.Mutex
	added   []metricsBatch
	sendCh  chan []metricsBatch
	flushed int
}

func (t *testClient) Add(service, instanceID string, labels map[string]string, resourceMetrics []*metricspb.ResourceMetrics) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.added = append(t.added, metricsBatch{
		service:         service,
		instanceID:      instanceID,
		labels:          labels,
		resourceMetrics: resourceMetrics,
	})
	return nil
}

func (t *testClient) Flush(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.flushed++
	if t.sendCh != nil && len(t.added) > 0 {
		batches := t.added
		t.added = nil
		select {
		case t.sendCh <- batches:
		default:
		}
	}
	return nil
}

func someResourceMetrics() []*metricspb.ResourceMetrics {
	return []*metricspb.ResourceMetrics{{}}
}

func (s *gathererSuite) TestGatherer(c *C) {
	received := make(chan []metricsBatch, 1)
	options := metricsGathererOptions{
		maxBufferedBatches: 2,
		newClient: func(target *plan.MetricsTarget) (metricsClient, error) {
			return &testClient{sendCh: received}, nil
		},
	}

	g, err := newMetricsGathererInternal(&plan.MetricsTarget{Name: "tgt1"}, &options)
	c.Assert(err, IsNil)
	defer g.Stop()

	g.Add("svc1", "instance-1", nil, someResourceMetrics())
	g.Add("svc1", "instance-1", nil, someResourceMetrics())

	select {
	case batches := <-received:
		c.Assert(batches, HasLen, 2)
		c.Check(batches[0].service, Equals, "svc1")
	case <-time.After(2 * time.Second):
		c.Fatal("timed out waiting for gatherer to flush")
	}
}

func (s *gathererSuite) TestGathererTimeout(c *C) {
	received := make(chan []metricsBatch, 1)
	options := metricsGathererOptions{
		bufferTimeout: 1 * time.Millisecond,
		newClient: func(target *plan.MetricsTarget) (metricsClient, error) {
			return &testClient{sendCh: received}, nil
		},
	}

	g, err := newMetricsGathererInternal(&plan.MetricsTarget{Name: "tgt1"}, &options)
	c.Assert(err, IsNil)
	defer g.Stop()

	g.Add("svc1", "instance-1", nil, someResourceMetrics())

	select {
	case batches := <-received:
		c.Assert(batches, HasLen, 1)
	case <-time.After(2 * time.Second):
		c.Fatal("timed out waiting for gatherer to flush on timeout")
	}
}

func (s *gathererSuite) TestGathererShutdown(c *C) {
	received := make(chan []metricsBatch, 1)
	fakeClient := &testClient{sendCh: received}
	options := metricsGathererOptions{
		bufferTimeout: 1 * time.Minute, // long enough that the timer won't fire
		newClient: func(target *plan.MetricsTarget) (metricsClient, error) {
			return fakeClient, nil
		},
	}

	g, err := newMetricsGathererInternal(&plan.MetricsTarget{Name: "tgt1"}, &options)
	c.Assert(err, IsNil)

	g.Add("svc1", "instance-1", nil, someResourceMetrics())

	// Give the main loop a chance to process the Add before we shut down.
	time.Sleep(10 * time.Millisecond)

	g.Stop()

	select {
	case batches := <-received:
		c.Assert(batches, HasLen, 1)
	default:
		c.Fatal("expected final flush to have sent buffered batch")
	}
}

func (s *gathererSuite) TestGathererDropsWhenBufferFull(c *C) {
	// blockFlush blocks until told to proceed, allowing us to fill up the
	// gatherer's queue before any flush can drain it.
	unblock := make(chan struct{})
	blockedClient := &blockingTestClient{unblock: unblock}

	options := metricsGathererOptions{
		queueSize: 1,
		newClient: func(target *plan.MetricsTarget) (metricsClient, error) {
			return blockedClient, nil
		},
	}

	g, err := newMetricsGathererInternal(&plan.MetricsTarget{Name: "tgt1"}, &options)
	c.Assert(err, IsNil)
	defer g.Stop()

	// The first Add will be consumed by the main loop immediately (blocking
	// on Add of the fake client), the second will sit in the queue (size 1),
	// and the third should be dropped since the queue is full.
	g.Add("svc1", "", nil, someResourceMetrics())
	time.Sleep(10 * time.Millisecond)
	g.Add("svc1", "", nil, someResourceMetrics())
	g.Add("svc1", "", nil, someResourceMetrics())

	close(unblock)
	time.Sleep(10 * time.Millisecond)

	blockedClient.mu.Lock()
	defer blockedClient.mu.Unlock()
	c.Check(len(blockedClient.added) < 3, Equals, true)
}

// blockingTestClient blocks the first call to Add until unblock is closed,
// simulating a slow consumer so we can test the gatherer's queue-full
// behaviour.
type blockingTestClient struct {
	mu      sync.Mutex
	added   []metricsBatch
	unblock chan struct{}
	once    sync.Once
}

func (b *blockingTestClient) Add(service, instanceID string, labels map[string]string, resourceMetrics []*metricspb.ResourceMetrics) error {
	b.once.Do(func() {
		<-b.unblock
	})
	b.mu.Lock()
	defer b.mu.Unlock()
	b.added = append(b.added, metricsBatch{service: service})
	return nil
}

func (b *blockingTestClient) Flush(ctx context.Context) error {
	return nil
}
