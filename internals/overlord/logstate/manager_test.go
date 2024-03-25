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
	"os"
	"sync"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/servicelog"
)

type managerSuite struct{}

var _ = Suite(&managerSuite{})

func (*managerSuite) SetUpSuite(c *C) {
	// Send logs to stderr, so we can see them when debugging
	l := logger.New(os.Stderr, "[test] ")
	logger.SetLogger(l)
}

func (*managerSuite) TestPlanChange(c *C) {
	gathererOptions := logGathererOptions{
		newClient: func(target *plan.LogTarget) (logClient, error) {
			return &testClient{}, nil
		},
	}
	m := NewLogManager()
	m.newGatherer = func(t *plan.LogTarget) (*logGatherer, error) {
		return newLogGathererInternal(t, &gathererOptions)
	}

	svc1 := newTestService("svc1")
	svc2 := newTestService("svc2")
	svc3 := newTestService("svc3")

	m.PlanChanged(plan.NewCombinedPlan(&plan.Layer{
		Services: map[string]*plan.Service{
			svc1.name: svc1.config,
			svc2.name: svc2.config,
			svc3.name: svc3.config,
		},
		LogTargets: map[string]*plan.LogTarget{
			"tgt1": {Name: "tgt1", Services: []string{"all", "-svc3"}},
			"tgt2": {Name: "tgt2", Services: []string{}},
			"tgt3": {Name: "tgt3", Services: []string{"all"}},
		},
	}))
	m.ServiceStarted(svc1.config, svc1.ringBuffer)
	m.ServiceStarted(svc2.config, svc2.ringBuffer)
	m.ServiceStarted(svc3.config, svc3.ringBuffer)

	checkGatherers(c, m.gatherers, map[string][]string{
		"tgt1": {"svc1", "svc2"},
		"tgt2": {},
		"tgt3": {"svc1", "svc2", "svc3"},
	})
	checkBuffers(c, m.buffers, []string{"svc1", "svc2", "svc3"})

	svc4 := newTestService("svc4")

	m.PlanChanged(plan.NewCombinedPlan(&plan.Layer{
		Services: map[string]*plan.Service{
			svc1.name: svc1.config,
			svc2.name: svc2.config,
			svc4.name: svc4.config,
		},
		LogTargets: map[string]*plan.LogTarget{
			"tgt1": {Name: "tgt1", Services: []string{"svc1"}},
			"tgt2": {Name: "tgt2", Services: []string{"svc1", "svc4"}},
			"tgt4": {Name: "tgt4", Services: []string{"all", "-svc2"}},
		},
	}))
	m.ServiceStarted(svc4.config, svc4.ringBuffer)
	// simulate service restart for svc2
	m.ServiceStarted(svc2.config, svc2.ringBuffer)

	checkGatherers(c, m.gatherers, map[string][]string{
		"tgt1": {"svc1"},
		"tgt2": {"svc1", "svc4"},
		"tgt4": {"svc1", "svc4"},
	})
	// svc3 no longer exists so we should have dropped the reference to its buffer
	checkBuffers(c, m.buffers, []string{"svc1", "svc2", "svc4"})
}

func checkGatherers(c *C, gatherers map[string]*logGatherer, expected map[string][]string) {
	c.Assert(gatherers, HasLen, len(expected))
	for tgtName, svcs := range expected {
		g, ok := gatherers[tgtName]
		c.Assert(ok, Equals, true)

		c.Assert(g.pullers.len(), Equals, len(svcs))
		for _, svc := range svcs {
			c.Check(g.pullers.contains(svc), Equals, true)
		}
	}
}

func checkBuffers(c *C, buffers map[string]*servicelog.RingBuffer, expected []string) {
	c.Assert(buffers, HasLen, len(expected))
	for _, svcName := range expected {
		_, ok := buffers[svcName]
		c.Check(ok, Equals, true)
	}
}

func (s *managerSuite) TestTimelyShutdown(c *C) {
	client := &slowFlushingClient{
		flushTime: 1 * time.Microsecond,
	}

	gathererOptions := logGathererOptions{
		timeoutCurrentFlush: 5 * time.Millisecond,
		timeoutFinalFlush:   5 * time.Millisecond,
		newClient: func(_ *plan.LogTarget) (logClient, error) {
			return client, nil
		},
	}

	m := NewLogManager()
	m.newGatherer = func(t *plan.LogTarget) (*logGatherer, error) {
		return newLogGathererInternal(t, &gathererOptions)
	}

	svc1 := newTestService("svc1")

	// Start 10 log gatherers
	logTargets := make(map[string]*plan.LogTarget, 10)
	for i := 0; i < 10; i++ {
		targetName := fmt.Sprintf("tgt%d", i)
		logTargets[targetName] = &plan.LogTarget{
			Name:     targetName,
			Services: []string{"all"},
		}
	}
	m.PlanChanged(plan.NewCombinedPlan(&plan.Layer{
		Services: map[string]*plan.Service{
			"svc1": svc1.config,
		},
		LogTargets: logTargets,
	}))
	m.ServiceStarted(svc1.config, svc1.ringBuffer)

	c.Assert(m.gatherers, HasLen, 10)

	err := svc1.stop()
	c.Assert(err, IsNil)

	client.SetFlushTime(10 * time.Second)
	// Stop all gatherers and check this happens quickly
	done := make(chan struct{})
	go func() {
		m.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		c.Fatal("LogManager.Stop() took too long")
	}
}

type slowFlushingClient struct {
	flushTime time.Duration
	mu        sync.Mutex
}

func (c *slowFlushingClient) SetLabels(serviceName string, labels map[string]string) {
	// no-op
}

func (c *slowFlushingClient) Add(_ servicelog.Entry) error {
	// no-op
	return nil
}

func (c *slowFlushingClient) Flush(ctx context.Context) error {
	c.mu.Lock()
	flushTime := c.flushTime
	c.mu.Unlock()

	select {
	case <-ctx.Done():
		return fmt.Errorf("flush timed out")
	case <-time.After(flushTime):
		return nil
	}
}

func (c *slowFlushingClient) SetFlushTime(timeout time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.flushTime = timeout
}

func (s *managerSuite) TestLabels(c *C) {
	fakeClient := &labelStore{
		labels:          map[string]map[string]string{},
		notifySetLabels: make(chan struct{}, 2),
	}

	m := NewLogManager()
	m.newGatherer = func(t *plan.LogTarget) (*logGatherer, error) {
		return newLogGathererInternal(t, &logGathererOptions{
			newClient: func(_ *plan.LogTarget) (logClient, error) { return fakeClient, nil },
		})
	}

	svc1 := newTestService("svc1")
	svc1.config.Environment = map[string]string{
		"OWNER": "alice",
		"IP":    "103.2.51.6",
		"PORT":  "3456",
	}

	svc2 := newTestService("svc2")
	svc2.config.Environment = map[string]string{
		"IP":   "103.2.52.88",
		"PORT": "9090",
	}

	pl := plan.NewCombinedPlan(&plan.Layer{
		Services: map[string]*plan.Service{
			"svc1": svc1.config,
			"svc2": svc2.config,
		},
		LogTargets: map[string]*plan.LogTarget{
			"tgt1": {
				Name:     "tgt1",
				Type:     plan.LokiTarget,
				Services: []string{"all"},
				Labels: map[string]string{
					"owner":   "user-$OWNER",
					"address": "http://${IP}:${PORT}",
				},
			},
		},
	})

	m.PlanChanged(pl)
	checkGatherers(c, m.gatherers, map[string][]string{
		"tgt1": nil,
	})

	m.ServiceStarted(svc1.config, svc1.ringBuffer)
	m.ServiceStarted(svc2.config, svc2.ringBuffer)

	fakeClient.waitLabels(c)
	fakeClient.waitLabels(c)
	c.Assert(fakeClient.labels, DeepEquals, map[string]map[string]string{
		"svc1": {
			"owner":   "user-alice",
			"address": "http://103.2.51.6:3456",
		},
		"svc2": {
			"owner":   "user-", // undefined env vars -> empty string
			"address": "http://103.2.52.88:9090",
		},
	})

	// If we change only the target's labels (with no change to the services),
	// we still need to recalculate the client's labels.
	logTargets := pl.LogTargets()
	logTargets["tgt1"].Labels["foo"] = "bar"
	m.PlanChanged(pl)

	// Wait for labels to be set
	fakeClient.waitLabels(c)
	fakeClient.waitLabels(c)
	c.Assert(fakeClient.labels, DeepEquals, map[string]map[string]string{
		"svc1": {
			"owner":   "user-alice",
			"address": "http://103.2.51.6:3456",
			"foo":     "bar",
		},
		"svc2": {
			"owner":   "user-",
			"address": "http://103.2.52.88:9090",
			"foo":     "bar",
		},
	})
}

// Fake logClient implementation which just stores the passed-in labels
type labelStore struct {
	labels map[string]map[string]string
	// synchronise on this channel to avoid data races
	notifySetLabels chan struct{}
}

func (c *labelStore) Add(_ servicelog.Entry) error {
	return nil // no-op
}

func (c *labelStore) Flush(_ context.Context) error {
	return nil // no-op
}

func (c *labelStore) SetLabels(serviceName string, labels map[string]string) {
	c.labels[serviceName] = labels
	select {
	case c.notifySetLabels <- struct{}{}:
	case <-time.After(1 * time.Second):
		panic("timeout waiting for notify for SetLabels")
	}
}

// wait for labels to be set
func (l *labelStore) waitLabels(c *C) {
	select {
	case <-l.notifySetLabels:
	case <-time.After(1 * time.Second):
		c.Fatal("timed out waiting for labels to be set")
	}
}
