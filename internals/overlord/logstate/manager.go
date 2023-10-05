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
	"os"
	"reflect"
	"sync"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/servicelog"
)

type LogManager struct {
	mu        sync.Mutex
	gatherers map[string]*logGatherer
	services  map[string]*ServiceData
	plan      *plan.Plan

	newGatherer func(*plan.LogTarget) (*logGatherer, error)
}

func NewLogManager() *LogManager {
	return &LogManager{
		gatherers:   map[string]*logGatherer{},
		services:    map[string]*ServiceData{},
		newGatherer: newLogGatherer,
	}
}

// PlanChanged is called by the service manager when the plan changes.
// Based on the new plan, we will Stop old gatherers and start new ones.
func (m *LogManager) PlanChanged(pl *plan.Plan) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Create a map to hold gatherers for the new plan.
	// Old gatherers will be moved over or deleted.
	newGatherers := make(map[string]*logGatherer, len(pl.LogTargets))

	for _, target := range pl.LogTargets {
		gatherer := m.gatherers[target.Name]
		if gatherer == nil {
			// Create new gatherer
			var err error
			gatherer, err = m.newGatherer(target)
			if err != nil {
				logger.Noticef("Internal error: cannot create gatherer for target %q: %v",
					target.Name, err)
				continue
			}
			newGatherers[target.Name] = gatherer
		} else {
			// Copy over existing gatherer
			newGatherers[target.Name] = gatherer
			delete(m.gatherers, target.Name)
		}

		// Evaluate labels
		labels := make(map[string]map[string]string, len(pl.Services))
		for svcName := range pl.Services {
			svcData, ok := m.services[svcName]
			if !ok {
				// Service not yet started
				continue
			}

			labels[svcName] = evaluateLabels(target.Labels, svcData.Env)
		}

		// Update gatherer
		gatherer.PlanChanged(pl, m.services, labels)
	}

	// Old gatherers for now-removed targets need to be shut down.
	for _, gatherer := range m.gatherers {
		go gatherer.Stop()
	}
	m.gatherers = newGatherers

	// Remove old service data
	for svc := range m.services {
		if _, ok := pl.Services[svc]; !ok {
			// Service has been removed
			delete(m.services, svc)
		}
	}

	m.plan = pl
}

// ServiceStarted notifies the log manager that the named service has started,
// and provides a reference to the service's log buffer and environment.
func (m *LogManager) ServiceStarted(service *plan.Service, data *ServiceData) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.services[service.Name].Equal(data) {
		// Service restarted, but nothing about it has changed. So we don't need
		// to do anything
		return
	}

	m.services[service.Name] = data
	for _, gatherer := range m.gatherers {
		target := m.plan.LogTargets[gatherer.targetName]
		if !service.LogsTo(target) {
			continue
		}

		labels := evaluateLabels(target.Labels, data.Env)
		gatherer.ServiceStarted(service, data.Buffer, labels)
	}
}

// evaluateLabels interprets the labels defined in the plan, substituting any
// $env_vars with the corresponding value in the service's environment.
func evaluateLabels(rawLabels, env map[string]string) map[string]string {
	substitute := func(k string) string {
		if v, ok := env[k]; ok {
			return v
		}
		logger.Debugf("variable $%s undefined in service environment", k)
		// undefined variables default to "", just like Bash
		return ""
	}

	labels := make(map[string]string, len(rawLabels))
	for key, rawLabel := range rawLabels {
		labels[key] = os.Expand(rawLabel, substitute)
	}
	return labels
}

// Ensure implements overlord.StateManager.
func (m *LogManager) Ensure() error {
	return nil
}

// Stop implements overlord.StateStopper and stops all log forwarding.
func (m *LogManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	wg := sync.WaitGroup{}
	for _, gatherer := range m.gatherers {
		wg.Add(1)
		go func(gatherer *logGatherer) {
			gatherer.Stop()
			wg.Done()
		}(gatherer)
	}
	wg.Wait()
}

// ServiceData holds the data for each service that the log manager needs to
// keep track of.
type ServiceData struct {
	Buffer *servicelog.RingBuffer
	Env    map[string]string
}

func (d *ServiceData) Equal(other *ServiceData) bool {
	return reflect.DeepEqual(d, other)
}
