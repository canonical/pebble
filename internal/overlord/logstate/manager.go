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

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/overlord/servstate"
	"github.com/canonical/pebble/internal/plan"
	"github.com/canonical/pebble/internal/servicelog"
)

type LogManager struct {
	forwarders []*logForwarder
	// service -> targets selected from plan
	planTargets map[string]map[string]*plan.LogTarget
	// Keep service config to know what changed
	services map[string]*plan.Service
	// Keep ring buffer for each running service
	buffers map[string]*servicelog.RingBuffer
	// Keep iterator for each (service, target) pair
	iterators map[string]map[plan.LogTarget]servicelog.Iterator
	// Mutex for reading/writing manager structs
	mu sync.Mutex
}

func NewLogManager() *LogManager {
	return &LogManager{
		buffers: map[string]*servicelog.RingBuffer{},
	}
}

// PlanChanged is called by the service manager when the plan changes. We stop
// all running forwarders, and start new forwarders based on the new plan.
// There are three distinct cases when creating the new forwarders:
//   - Existing service and target: create the forwarder using the same
//     iterator, to avoid log duplication.
//   - Existing service, new target: create the forwarder using a new iterator
//     over the existing buffer.
//   - New service: don't create anything yet. We have to wait until
//     ServiceStarted is called, and we are given a reference to the service's
//     ringbuffer.
func (m *LogManager) PlanChanged(pl *plan.Plan) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Stop all existing forwarders
	for _, f := range m.forwarders {
		f.stop()
	}
	m.forwarders = []*logForwarder{}

	m.planTargets = selectTargets(pl)

	// Create new buffer/iterator structures for LogManager
	// We will update these and overwrite LogManager fields at the end
	buffers := map[string]*servicelog.RingBuffer{}
	iterators := map[string]map[plan.LogTarget]servicelog.Iterator{}

	// Work out which services haven't changed (will continue running)
	// For each of these services, ServiceStarted won't be called, so we need to
	// manually restart those forwarders
	unchangedServices := []string{}
	for serviceName, oldConfig := range m.services {
		if newConfig, ok := pl.Services[serviceName]; ok {
			if !servstate.NeedsRestart(oldConfig, newConfig) {
				unchangedServices = append(unchangedServices, serviceName)
			}
		}
	}

	for _, serviceName := range unchangedServices {
		iterators[serviceName] = map[plan.LogTarget]servicelog.Iterator{}
		// Copy buffer reference over
		buffers[serviceName] = m.buffers[serviceName]

		for _, target := range m.planTargets[serviceName] {
			iterator, ok := m.iterators[serviceName][*target]
			if !ok {
				// New target for existing service - need a new iterator
				iterator = m.buffers[serviceName].HeadIterator(0)
			}

			// Copy iterator reference over
			iterators[serviceName][*target] = iterator

			m.startForwarder(serviceName, target, iterator)
		}
	}

	// For other services: forwarders will be started in ServiceStarted

	// Update buffers and iterators
	m.buffers = buffers
	m.iterators = iterators

	// Update services
	m.services = map[string]*plan.Service{}
	for serviceName, service := range pl.Services {
		m.services[serviceName] = service.Copy()
	}
}

// selectTargets goes through the given plan and determines which service's
// logs should be forwarded to which log targets.
func selectTargets(p *plan.Plan) map[string]map[string]*plan.LogTarget {
	planTargets := make(map[string]map[string]*plan.LogTarget, len(p.Services))

	for serviceName, service := range p.Services {
		planTargets[serviceName] = make(map[string]*plan.LogTarget, len(service.LogTargets))

		for _, targetName := range service.LogTargets {
			target, ok := p.LogTargets[targetName]
			if !ok {
				logger.Noticef("Internal error: log manager cannot find target %q (specified by service %q in plan)",
					targetName, serviceName)
				continue
			}
			if target.Selection != plan.DisabledSelection {
				planTargets[serviceName][targetName] = target
			}
		}

		// Add opt-out targets if none specified
		if len(service.LogTargets) == 0 {
			for targetName, target := range p.LogTargets {
				// Default (unset) is opt-out
				if target.Selection == plan.OptOutSelection || target.Selection == plan.UnsetSelection {
					planTargets[serviceName][targetName] = target
				}
			}
		}
	}

	return planTargets
}

// ServiceStarted notifies the log manager that the named service has started,
// and provides a reference to the service's log buffer.
func (m *LogManager) ServiceStarted(serviceName string, buffer *servicelog.RingBuffer) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.buffers[serviceName] = buffer
	m.iterators[serviceName] = make(map[plan.LogTarget]servicelog.Iterator,
		len(m.planTargets[serviceName]))

	for _, target := range m.planTargets[serviceName] {
		iterator := m.buffers[serviceName].HeadIterator(0)
		m.iterators[serviceName][*target] = iterator

		m.startForwarder(serviceName, target, iterator)
	}
}

// startForwarder starts a new forwarder for the specified service and target,
// and adds this forwarder to the LogManager's list.
func (m *LogManager) startForwarder(
	serviceName string, target *plan.LogTarget, iterator servicelog.Iterator,
) {
	f, err := newLogForwarder(serviceName, target, iterator)
	if err != nil {
		logger.Noticef("Internal error: cannot create forwarder %q -> %q: %v",
			serviceName, target.Name, err)
		return
	}

	m.forwarders = append(m.forwarders, f)
	logger.Debugf("Started forwarding logs for service %q to target %q",
		serviceName, target.Name)
	go f.forward()
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
