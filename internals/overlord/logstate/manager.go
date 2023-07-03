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
	"sync"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/servicelog"
)

type LogManager struct {
	forwarders map[string]*logForwarder
	gatherers  map[string]*logGatherer

	newForwarder func(serviceName string) *logForwarder
	newGatherer  func(*plan.LogTarget) *logGatherer
}

func NewLogManager() *LogManager {
	return &LogManager{
		forwarders:   map[string]*logForwarder{},
		gatherers:    map[string]*logGatherer{},
		newForwarder: newLogForwarder,
		newGatherer:  newLogGatherer,
	}
}

// PlanChanged is called by the service manager when the plan changes. We update the list of gatherers for each forwarder based on the new plan.
func (m *LogManager) PlanChanged(pl *plan.Plan) {
	// Create a map to hold forwarders/gatherers for the new plan.
	// Old forwarders/gatherers will be moved over or deleted.
	newForwarders := make(map[string]*logForwarder, len(pl.Services))
	newGatherers := make(map[string]*logGatherer, len(pl.LogTargets))

	for serviceName, service := range pl.Services {
		forwarder := m.forwarders[serviceName]
		if forwarder == nil {
			// Create new forwarder
			forwarder = m.newForwarder(serviceName)
			newForwarders[serviceName] = forwarder
		} else {
			// Copy over existing forwarder
			newForwarders[serviceName] = forwarder
			delete(m.forwarders, serviceName)
		}

		// update clients
		forwarder.mu.Lock()
		forwarder.gatherers = []*logGatherer{}

		for _, target := range pl.LogTargets {
			// Only create the gatherer if there is a service logging to it.
			// Don't need gatherers for disabled or unselected targets.
			if service.LogsTo(target) {
				gatherer := m.gatherers[serviceName]
				if gatherer == nil {
					// Create new gatherer
					gatherer = m.newGatherer(target)
					go gatherer.loop()
					newGatherers[target.Name] = gatherer
				} else {
					// Copy over existing gatherer
					newGatherers[target.Name] = gatherer
					delete(m.gatherers, target.Name)
				}

				forwarder.gatherers = append(forwarder.gatherers, gatherer)
			}
		}

		forwarder.mu.Unlock()
	}

	// Old forwarders for now-removed services need to be shut down.
	for _, forwarder := range m.forwarders {
		forwarder.stop()
	}
	m.forwarders = newForwarders

	// Same with old gatherers.
	for _, gatherer := range m.gatherers {
		gatherer.stop()
	}
	m.gatherers = newGatherers
}

// ServiceStarted notifies the log manager that the named service has started,
// and provides a reference to the service's log buffer.
func (m *LogManager) ServiceStarted(serviceName string, buffer *servicelog.RingBuffer) {
	forwarder := m.forwarders[serviceName]
	if forwarder == nil {
		logger.Noticef("Internal error: couldn't find forwarder for %q", serviceName)
		return
	}
	go forwarder.forward(buffer)
}

// Ensure implements overlord.StateManager.
func (m *LogManager) Ensure() error {
	return nil
}

// Stop implements overlord.StateStopper and stops all log forwarding.
func (m *LogManager) Stop() {
	wg := sync.WaitGroup{}
	for _, f := range m.forwarders {
		wg.Add(1)
		go func(f *logForwarder) {
			f.stop()
			wg.Done()
		}(f)
	}
	wg.Wait()
}
