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
	"bytes"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/plan"
	"github.com/canonical/pebble/internal/servicelog"
	. "gopkg.in/check.v1"
)

type managerSuite struct {
	logbuf        *bytes.Buffer
	restoreLogger func()
}

var _ = Suite(&managerSuite{})

func Test(t *testing.T) { TestingT(t) }

func (s *managerSuite) SetUpTest(c *C) {
	s.logbuf, s.restoreLogger = logger.MockLogger("PREFIX: ")
}

func (s *managerSuite) TearDownTest(c *C) {
	s.restoreLogger()
}

func (s *managerSuite) TestSelectTargets(c *C) {
	unset := plan.LogTarget{Selection: plan.UnsetSelection}
	optout := plan.LogTarget{Selection: plan.OptOutSelection}
	optin := plan.LogTarget{Selection: plan.OptInSelection}
	disabled := plan.LogTarget{Selection: plan.DisabledSelection}

	input := plan.Plan{
		LogTargets: map[string]*plan.LogTarget{
			"unset":    &unset,
			"optout":   &optout,
			"optin":    &optin,
			"disabled": &disabled,
		},
		Services: map[string]*plan.Service{
			"svc1": {LogTargets: nil},
			"svc2": {LogTargets: []string{}},
			"svc3": {LogTargets: []string{"unset"}},
			"svc4": {LogTargets: []string{"optout"}},
			"svc5": {LogTargets: []string{"optin"}},
			"svc6": {LogTargets: []string{"disabled"}},
			"svc7": {LogTargets: []string{"unset", "optin", "disabled"}},
		},
	}

	expected := map[string]map[string]*plan.LogTarget{
		"svc1": {"unset": &unset, "optout": &optout},
		"svc2": {"unset": &unset, "optout": &optout},
		"svc3": {"unset": &unset},
		"svc4": {"optout": &optout},
		"svc5": {"optin": &optin},
		"svc6": {},
		"svc7": {"unset": &unset, "optin": &optin},
	}

	planTargets := selectTargets(&input)
	c.Check(planTargets, DeepEquals, expected)
	// Check no error messages were logged
	c.Check(s.logbuf.Bytes(), HasLen, 0)
}

func (s *managerSuite) TestLogManager(c *C) {
	m := NewLogManager()
	// Fake ringbuffer so that log manager can create forwarders
	rb := servicelog.RingBuffer{}

	// Call PlanChanged with new plan
	m.PlanChanged(&plan.Plan{
		Services: map[string]*plan.Service{
			"svc1": {},
			"svc2": {LogTargets: []string{"optin", "disabled"}},
			"svc3": {LogTargets: []string{"unset"}},
		},
		LogTargets: map[string]*plan.LogTarget{
			"unset":    {Name: "unset", Type: plan.LokiTarget, Selection: plan.UnsetSelection},
			"optout":   {Name: "optout", Type: plan.LokiTarget, Selection: plan.OptOutSelection},
			"optin":    {Name: "optin", Type: plan.LokiTarget, Selection: plan.OptInSelection},
			"disabled": {Name: "disabled", Type: plan.LokiTarget, Selection: plan.DisabledSelection},
		},
	})

	// Start 3 services
	var wg sync.WaitGroup
	wg.Add(3)

	// Call ServiceStarted
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
	c.Check(m.forwarders, HasLen, 4)
	checkForwarderExists(c, m.forwarders, "svc1", "unset")
	checkForwarderExists(c, m.forwarders, "svc1", "optout")
	checkForwarderExists(c, m.forwarders, "svc2", "optin")
	checkForwarderExists(c, m.forwarders, "svc3", "unset")

	// Update the plan
	m.PlanChanged(&plan.Plan{
		Services: map[string]*plan.Service{
			"svc1": {},
			"svc2": {LogTargets: []string{"optout", "disabled"}},
			"svc4": {LogTargets: []string{"optin"}},
		},
		LogTargets: map[string]*plan.LogTarget{
			"unset":    {Name: "unset", Type: plan.LokiTarget, Selection: plan.UnsetSelection},
			"optout":   {Name: "optout", Type: plan.LokiTarget, Selection: plan.OptOutSelection},
			"optin":    {Name: "optin", Type: plan.LokiTarget, Selection: plan.OptInSelection},
			"disabled": {Name: "disabled", Type: plan.LokiTarget, Selection: plan.DisabledSelection},
		},
	})

	// Call ServiceStarted
	m.ServiceStarted("svc4", &rb)

	c.Check(m.forwarders, HasLen, 4)
	checkForwarderExists(c, m.forwarders, "svc1", "unset")
	checkForwarderExists(c, m.forwarders, "svc1", "optout")
	checkForwarderExists(c, m.forwarders, "svc2", "optout")
	checkForwarderExists(c, m.forwarders, "svc4", "optin")
}

// checkForwarderExists checks that a forwarder for the given service and
// target exists in the provided slice of forwarders.
func checkForwarderExists(c *C, forwarders []*logForwarder, serviceName, targetName string) {
	for _, f := range forwarders {
		if f.service == serviceName && f.target.Name == targetName {
			return
		}
	}
	c.Errorf("no forwarder found with service: %q, target: %q", serviceName, targetName)
}

func (s *managerSuite) TestNoLogDuplication(c *C) {
	// Reduce Loki flush time
	flushDelayOld := flushDelay
	flushDelay = 1 * time.Microsecond
	defer func() {
		flushDelay = flushDelayOld
	}()

	m := NewLogManager()
	rb := servicelog.NewRingBuffer(1024)

	// Set up fake "Loki" server
	requests := make(chan string)
	srv := newFakeLokiServer(requests)
	defer srv.Close()

	// Utility functions for this test
	writeLog := func(timestamp time.Time, logLine string) {
		_, err := fmt.Fprintf(rb, "%s [svc1] %s\n",
			timestamp.UTC().Format("2006-01-02T15:04:05.000Z07:00"), logLine)
		c.Assert(err, IsNil)
	}
	expectLogs := func(expected string) {
		select {
		case req := <-requests:
			c.Assert(req, Equals, expected)
		case <-time.After(10 * time.Millisecond):
			c.Fatalf("timed out waiting for request %q", expected)
		}
	}

	m.PlanChanged(&plan.Plan{
		Services: map[string]*plan.Service{
			"svc1": {},
		},
		LogTargets: map[string]*plan.LogTarget{
			"unset": {
				Type:      plan.LokiTarget,
				Location:  srv.URL(),
				Selection: plan.UnsetSelection,
			},
		},
	})
	m.ServiceStarted("svc1", rb)
	c.Assert(m.forwarders, HasLen, 1)

	// Write logs
	writeLog(time.Date(2023, 1, 31, 1, 23, 45, 67890, time.UTC), "log line #1")
	writeLog(time.Date(2023, 1, 31, 1, 23, 46, 67890, time.UTC), "log line #2")
	expectLogs(`{"streams":[{"stream":{"pebble_service":"svc1"},"values":[["1675128225000000000","log line #1"],["1675128226000000000","log line #2"]]}]}`)

	// Call PlanChanged again
	m.PlanChanged(&plan.Plan{
		Services: map[string]*plan.Service{
			"svc1": {},
		},
		LogTargets: map[string]*plan.LogTarget{
			"unset": {
				Type:      plan.LokiTarget,
				Location:  srv.URL(),
				Selection: plan.UnsetSelection,
			},
		},
	})
	c.Check(m.forwarders, HasLen, 1)

	// Write logs
	writeLog(time.Date(2023, 1, 31, 1, 23, 47, 67890, time.UTC), "log line #3")
	writeLog(time.Date(2023, 1, 31, 1, 23, 48, 67890, time.UTC), "log line #4")
	expectLogs(`{"streams":[{"stream":{"pebble_service":"svc1"},"values":[["1675128227000000000","log line #3"],["1675128228000000000","log line #4"]]}]}`)
}
