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
	"sort"
	"sync"
	"time"

	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/servicelog"

	. "gopkg.in/check.v1"
)

type managerSuite struct{}

var _ = Suite(&managerSuite{})

func (s *managerSuite) TestLogManager(c *C) {
	m := newLogManagerForTest(1*time.Second, 10, make(chan []servicelog.Entry))
	// Fake ringbuffer so that log manager can create forwarders
	rb := servicelog.RingBuffer{}

	// Call PlanChanged with new plan
	m.PlanChanged(&plan.Plan{
		Services: map[string]*plan.Service{
			"svc1": {Name: "svc1"},
			"svc2": {Name: "svc2"},
			"svc3": {Name: "svc3"},
		},
		LogTargets: map[string]*plan.LogTarget{
			"tgt1": {Name: "tgt1", Type: plan.LokiTarget, Services: []string{"svc1"}},
			"tgt2": {Name: "tgt2", Type: plan.LokiTarget, Services: []string{"all", "-svc2"}},
			"tgt3": {Name: "tgt3", Type: plan.LokiTarget, Services: []string{"svc1", "svc3", "-svc1"}},
			"tgt4": {Name: "tgt4", Type: plan.LokiTarget, Services: []string{}},
		},
	})

	// Start the three services. We do this concurrently to simulate Pebble's
	// actual service startup, and check there are no race conditions.
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		m.ServiceStarted("svc1", &rb)
	}()
	go func() {
		defer wg.Done()
		m.ServiceStarted("svc2", &rb)
	}()
	go func() {
		defer wg.Done()
		m.ServiceStarted("svc3", &rb)
	}()

	wg.Wait()
	c.Assert(getServiceNames(m.forwarders), DeepEquals, []string{"svc1", "svc2", "svc3"})
	c.Assert(getTargets(m.forwarders["svc1"]), DeepEquals, []string{"tgt1", "tgt2"})
	c.Assert(getTargets(m.forwarders["svc2"]), DeepEquals, []string(nil))
	c.Assert(getTargets(m.forwarders["svc3"]), DeepEquals, []string{"tgt2", "tgt3"})

	// Update the plan
	m.PlanChanged(&plan.Plan{
		Services: map[string]*plan.Service{
			"svc1": {Name: "svc1"},
			"svc2": {Name: "svc2"},
			"svc4": {Name: "svc4"},
		},
		LogTargets: map[string]*plan.LogTarget{
			"tgt1": {Name: "tgt1", Type: plan.LokiTarget, Services: []string{"svc1", "svc2"}},
			"tgt2": {Name: "tgt2", Type: plan.LokiTarget, Services: []string{"svc2"}},
			"tgt3": {Name: "tgt3", Type: plan.LokiTarget, Services: []string{}},
			"tgt4": {Name: "tgt4", Type: plan.LokiTarget, Services: []string{"all"}},
		},
	})

	// Call ServiceStarted
	m.ServiceStarted("svc4", &rb)
	c.Assert(getServiceNames(m.forwarders), DeepEquals, []string{"svc1", "svc2", "svc4"})
	c.Assert(getTargets(m.forwarders["svc1"]), DeepEquals, []string{"tgt1", "tgt4"})
	c.Assert(getTargets(m.forwarders["svc2"]), DeepEquals, []string{"tgt1", "tgt2", "tgt4"})
	c.Assert(getTargets(m.forwarders["svc4"]), DeepEquals, []string{"tgt4"})
}

func (s *managerSuite) TestNoLogDuplication(c *C) {
	recv := make(chan []servicelog.Entry)
	m := newLogManagerForTest(10*time.Microsecond, 10, recv)
	rb := servicelog.NewRingBuffer(1024)

	// Utility functions for this test
	writeLog := func(timestamp time.Time, logLine string) {
		_, err := fmt.Fprintf(rb, "%s [svc1] %s\n",
			timestamp.UTC().Format("2006-01-02T15:04:05.000Z07:00"), logLine)
		c.Assert(err, IsNil)
	}
	expectLogs := func(expected ...string) {
		select {
		case entries := <-recv:
			c.Assert(entries, HasLen, len(expected))
			for i, entry := range entries {
				c.Check(entry.Message, Equals, expected[i]+"\n")
			}

		case <-time.After(10 * time.Millisecond):
			c.Fatalf("timed out waiting for request %q", expected)
		}
	}

	m.PlanChanged(&plan.Plan{
		Services: map[string]*plan.Service{
			"svc1": {Name: "svc1"},
		},
		LogTargets: map[string]*plan.LogTarget{
			"tgt1": {Name: "tgt1", Services: []string{"svc1"}},
		},
	})
	m.ServiceStarted("svc1", rb)
	c.Assert(getServiceNames(m.forwarders), DeepEquals, []string{"svc1"})
	c.Assert(getTargets(m.forwarders["svc1"]), DeepEquals, []string{"tgt1"})

	// Write logs
	writeLog(time.Now(), "log line #1")
	writeLog(time.Now(), "log line #2")
	expectLogs("log line #1", "log line #2")

	// Call PlanChanged again
	m.PlanChanged(&plan.Plan{
		Services: map[string]*plan.Service{
			"svc1": {Name: "svc1"},
		},
		LogTargets: map[string]*plan.LogTarget{
			"tgt1": {Name: "tgt1", Services: []string{"svc1"}},
		},
	})
	c.Assert(getServiceNames(m.forwarders), DeepEquals, []string{"svc1"})
	c.Assert(getTargets(m.forwarders["svc1"]), DeepEquals, []string{"tgt1"})

	// Write logs
	writeLog(time.Now(), "log line #3")
	writeLog(time.Now(), "log line #4")
	expectLogs("log line #3", "log line #4")
}

func (s *managerSuite) TestFlushLogsOnInterrupt(c *C) {
	recv := make(chan []servicelog.Entry)
	m := newLogManagerForTest(10*time.Microsecond, 10, recv)
	rb := servicelog.NewRingBuffer(1024)

	// Utility functions for this test
	writeLog := func(timestamp time.Time, logLine string) {
		_, err := fmt.Fprintf(rb, "%s [svc1] %s\n",
			timestamp.UTC().Format("2006-01-02T15:04:05.000Z07:00"), logLine)
		c.Assert(err, IsNil)
	}
	expectLogs := func(expected ...string) {
		select {
		case entries := <-recv:
			c.Assert(entries, HasLen, len(expected))
			for i, entry := range entries {
				c.Check(entry.Message, Equals, expected[i]+"\n")
			}

		case <-time.After(10 * time.Millisecond):
			c.Fatalf("timed out waiting for request %q", expected)
		}
	}

	m.PlanChanged(&plan.Plan{
		Services: map[string]*plan.Service{
			"svc1": {Name: "svc1"},
		},
		LogTargets: map[string]*plan.LogTarget{
			"tgt1": {Name: "tgt1", Services: []string{"svc1"}},
		},
	})
	m.ServiceStarted("svc1", rb)
	c.Assert(getServiceNames(m.forwarders), DeepEquals, []string{"svc1"})
	c.Assert(getTargets(m.forwarders["svc1"]), DeepEquals, []string{"tgt1"})

	// Write logs
	writeLog(time.Now(), "log line #1")
	writeLog(time.Now(), "log line #2")

	// Logs shouldn't be sent through yet
	select {
	case e := <-recv:
		c.Fatalf("unexpected logs received: %v", e)
	default:
	}

	m.Stop()
	expectLogs("log line #1", "log line #2")
}

func newLogManagerForTest(
	tickPeriod time.Duration, bufferCapacity int, recv chan []servicelog.Entry,
) *LogManager {
	return &LogManager{
		forwarders:   map[string]*logForwarder{},
		gatherers:    map[string]*logGatherer{},
		newForwarder: newLogForwarder, // ForTest ?
		newGatherer: func(target *plan.LogTarget) *logGatherer {
			return newLogGathererForTest(target, tickPeriod, bufferCapacity, recv)
		},
	}
}

func getServiceNames(forwarders map[string]*logForwarder) (serviceNames []string) {
	for serviceName := range forwarders {
		serviceNames = append(serviceNames, serviceName)
	}
	sort.Strings(serviceNames)
	return
}

func getTargets(forwarder *logForwarder) (targetNames []string) {
	for _, gatherers := range forwarder.gatherers {
		targetNames = append(targetNames, gatherers.target.Name)
	}
	sort.Strings(targetNames)
	return
}
