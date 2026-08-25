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

package tracestate

import (
	"os"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/plan"
)

type managerSuite struct{}

var _ = Suite(&managerSuite{})

func (*managerSuite) SetUpSuite(c *C) {
	// Send logs to stderr, so we can see them when debugging
	l := logger.New(os.Stderr, "[test] ")
	logger.SetLogger(l)
}

func newTestTraceManager(clients map[string]*testClient) *TraceManager {
	m := NewTraceManager()
	m.newGatherer = func(t *plan.TraceTarget) (*traceGatherer, error) {
		client := clients[t.Name]
		if client == nil {
			client = &testClient{}
			clients[t.Name] = client
		}
		return newtraceGathererInternal(t, &traceGathererOptions{
			newClient: func(*plan.TraceTarget) (tracesClient, error) { return client, nil },
		})
	}
	return m
}

func (*managerSuite) TestPlanChange(c *C) {
	clients := map[string]*testClient{}
	m := newTestTraceManager(clients)

	m.PlanChanged(&plan.Plan{
		Services: map[string]*plan.Service{
			"svc1": {Name: "svc1"},
			"svc2": {Name: "svc2"},
		},
		TraceTargets: map[string]*plan.TraceTarget{
			"tgt1": {Name: "tgt1", Services: []string{"all"}},
			"tgt2": {Name: "tgt2", Services: []string{}},
		},
	})
	c.Assert(m.gatherers, HasLen, 2)
	_, ok := m.gatherers["tgt1"]
	c.Check(ok, Equals, true)
	_, ok = m.gatherers["tgt2"]
	c.Check(ok, Equals, true)

	tgt1 := m.gatherers["tgt1"]

	// Removing tgt2, keeping tgt1 - the tgt1 gatherer object should be reused.
	m.PlanChanged(&plan.Plan{
		Services: map[string]*plan.Service{
			"svc1": {Name: "svc1"},
		},
		TraceTargets: map[string]*plan.TraceTarget{
			"tgt1": {Name: "tgt1", Services: []string{"all"}},
		},
	})
	c.Assert(m.gatherers, HasLen, 1)
	c.Check(m.gatherers["tgt1"], Equals, tgt1)
}

func (*managerSuite) TestAddTracesRouting(c *C) {
	clients := map[string]*testClient{}
	m := newTestTraceManager(clients)

	m.PlanChanged(&plan.Plan{
		Services: map[string]*plan.Service{
			"svc1": {Name: "svc1"},
			"svc2": {Name: "svc2"},
		},
		TraceTargets: map[string]*plan.TraceTarget{
			"tgt1": {Name: "tgt1", Services: []string{"svc1"}},
			"tgt2": {Name: "tgt2", Services: []string{"all"}},
		},
	})

	rs := someResourceSpans()

	// svc1 is enrolled in both targets.
	m.AddTraces("svc1", rs)
	// svc2 is enrolled only in tgt2.
	m.AddTraces("svc2", rs)
	// Unknown service (not in the plan at all) is silently discarded.
	m.AddTraces("unknown-svc", rs)

	waitAdded(c, clients["tgt1"], 1)
	waitAdded(c, clients["tgt2"], 2)
}

func (*managerSuite) TestAddTracesServiceNotEnrolledAnywhere(c *C) {
	clients := map[string]*testClient{}
	m := newTestTraceManager(clients)

	m.PlanChanged(&plan.Plan{
		Services: map[string]*plan.Service{
			"svc1": {Name: "svc1"},
		},
		TraceTargets: map[string]*plan.TraceTarget{
			"tgt1": {Name: "tgt1", Services: []string{}},
		},
	})

	m.AddTraces("svc1", someResourceSpans())

	time.Sleep(20 * time.Millisecond)
	waitAdded(c, clients["tgt1"], 0)
}

func (*managerSuite) TestServiceStartedGeneratesNewInstanceID(c *C) {
	clients := map[string]*testClient{}
	m := newTestTraceManager(clients)

	svc := &plan.Service{Name: "svc1"}
	m.PlanChanged(&plan.Plan{
		Services: map[string]*plan.Service{
			"svc1": svc,
		},
		TraceTargets: map[string]*plan.TraceTarget{
			"tgt1": {Name: "tgt1", Services: []string{"all"}},
		},
	})

	m.ServiceStarted(svc)
	m.mu.Lock()
	id1 := m.instanceIDs["svc1"]
	m.mu.Unlock()
	c.Assert(id1, Not(Equals), "")

	m.ServiceStarted(svc)
	m.mu.Lock()
	id2 := m.instanceIDs["svc1"]
	m.mu.Unlock()
	c.Assert(id2, Not(Equals), "")
	c.Check(id2, Not(Equals), id1)

	m.AddTraces("svc1", someResourceSpans())
	client := clients["tgt1"]
	waitAdded(c, client, 1)
	client.mu.Lock()
	defer client.mu.Unlock()
	c.Check(client.added[0].instanceID, Equals, id2)
}

func (*managerSuite) TestAddTracesEvaluatesLabels(c *C) {
	clients := map[string]*testClient{}
	m := newTestTraceManager(clients)

	svc := &plan.Service{
		Name: "svc1",
		Environment: map[string]string{
			"OWNER": "alice",
		},
	}
	m.PlanChanged(&plan.Plan{
		Services: map[string]*plan.Service{
			"svc1": svc,
		},
		TraceTargets: map[string]*plan.TraceTarget{
			"tgt1": {
				Name:     "tgt1",
				Services: []string{"all"},
				Labels: map[string]string{
					"owner": "user-$OWNER",
				},
			},
		},
	})

	m.AddTraces("svc1", someResourceSpans())

	client := clients["tgt1"]
	waitAdded(c, client, 1)
	client.mu.Lock()
	defer client.mu.Unlock()
	c.Check(client.added[0].labels, DeepEquals, map[string]string{"owner": "user-alice"})
}

func (*managerSuite) TestStop(c *C) {
	clients := map[string]*testClient{}
	m := newTestTraceManager(clients)

	m.PlanChanged(&plan.Plan{
		Services: map[string]*plan.Service{
			"svc1": {Name: "svc1"},
		},
		TraceTargets: map[string]*plan.TraceTarget{
			"tgt1": {Name: "tgt1", Services: []string{"all"}},
			"tgt2": {Name: "tgt2", Services: []string{"all"}},
		},
	})

	m.AddTraces("svc1", someResourceSpans())

	done := make(chan struct{})
	go func() {
		m.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		c.Fatal("TraceManager.Stop() took too long")
	}

	waitAdded(c, clients["tgt1"], 1)
}

// waitAdded waits until the client has received the expected number of
// added batches (via at least one Flush call), or times out.
func waitAdded(c *C, client *testClient, expected int) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		client.mu.Lock()
		n := len(client.added)
		flushed := client.flushed
		client.mu.Unlock()
		if n == expected && (expected == 0 || flushed > 0) {
			return
		}
		if time.Now().After(deadline) {
			c.Fatalf("timed out waiting for %d added batches, got %d", expected, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
