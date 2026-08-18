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

// Package metricsstate implements Pebble's local OTLP metrics receiver and
// forwarder. Services enrolled in a metrics opentelemetry target send their
// OTLP metrics to a per-service endpoint hosted by Pebble; the received
// metrics are enriched with resource attributes and buffered here before being
// forwarded on to the configured remote backend(s).
package metricsstate

import (
	"crypto/rand"
	"fmt"
	"os"
	"sync"

	"github.com/canonical/pebble/internals/logger"
	metricspb "github.com/canonical/pebble/internals/otlp/metrics/v1"
	"github.com/canonical/pebble/internals/plan"
)

// MetricsManager receives OTLP metrics on behalf of enrolled services, enriches
// them, and forwards them to the appropriate metrics targets.
type MetricsManager struct {
	mu sync.Mutex

	gatherers map[string]*metricsGatherer
	// instanceIDs holds a UUID generated at each (re)start of a service, used
	// to populate the service.instance.id resource attribute.
	instanceIDs map[string]string
	plan        *plan.Plan

	newGatherer func(*plan.MetricTarget) (*metricsGatherer, error)
}

// NewMetricsManager creates a new MetricsManager.
func NewMetricsManager() *MetricsManager {
	return &MetricsManager{
		gatherers:   map[string]*metricsGatherer{},
		instanceIDs: map[string]string{},
		newGatherer: newMetricsGatherer,
	}
}

// PlanChanged is called by the plan manager when the plan changes. Based on the
// new plan, we will stop old gatherers and start new ones.
func (m *MetricsManager) PlanChanged(pl *plan.Plan) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Create a map to hold gatherers for the new plan. Old gatherers will be
	// moved over or deleted.
	newGatherers := make(map[string]*metricsGatherer, len(pl.MetricTargets))

	for _, target := range pl.MetricTargets {
		gatherer := m.gatherers[target.Name]
		if gatherer == nil {
			// Create new gatherer
			var err error
			gatherer, err = m.newGatherer(target)
			if err != nil {
				logger.Noticef(
					"Internal error: cannot create metrics gatherer for target %q: %v",
					target.Name, err,
				)
				continue
			}
			newGatherers[target.Name] = gatherer
		} else {
			// Copy over existing gatherer
			newGatherers[target.Name] = gatherer
			delete(m.gatherers, target.Name)
		}
	}

	// Old gatherers for now-removed targets need to be shut down.
	for _, gatherer := range m.gatherers {
		go gatherer.Stop()
	}
	m.gatherers = newGatherers

	// Remove instance IDs for services that have been removed from the plan.
	for svc := range m.instanceIDs {
		if _, ok := pl.Services[svc]; !ok {
			delete(m.instanceIDs, svc)
		}
	}

	m.plan = pl
}

// ServiceStarted notifies the metrics manager that the service has started.
// A new service.instance.id is generated, which will be added as a resource
// attribute to metrics received from this service until it restarts again.
func (m *MetricsManager) ServiceStarted(service *plan.Service) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.instanceIDs[service.Name] = newInstanceID()
}

// AddMetrics buffers the given resource metrics, received on behalf of the
// service, for forwarding to every metrics target the service is enrolled in.
// If the service is unknown, or not enrolled in any metric target, the metrics
// are silently discarded.
func (m *MetricsManager) AddMetrics(serviceName string, resourceMetrics []*metricspb.ResourceMetrics) {
	if len(resourceMetrics) == 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.plan == nil {
		return
	}
	service, ok := m.plan.Services[serviceName]
	if !ok {
		return
	}

	instanceID := m.instanceIDs[serviceName]
	for _, target := range m.plan.MetricTargets {
		if !service.MetricsTo(target) {
			continue
		}
		gatherer := m.gatherers[target.Name]
		if gatherer == nil {
			continue
		}
		labels := evaluateLabels(target.Labels, service.Environment)
		gatherer.Add(serviceName, instanceID, labels, resourceMetrics)
	}
}

// Ensure implements overlord.StateManager.
func (m *MetricsManager) Ensure() error {
	return nil
}

// Stop implements overlord.StateStopper and stops all metrics forwarding.
func (m *MetricsManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	wg := sync.WaitGroup{}
	for _, gatherer := range m.gatherers {
		wg.Add(1)
		go func(gatherer *metricsGatherer) {
			gatherer.Stop()
			wg.Done()
		}(gatherer)
	}
	wg.Wait()
}

// evaluateLabels interprets the labels defined in the plan, substituting any
// $env_vars with the corresponding value in the service's environment.
func evaluateLabels(rawLabels, env map[string]string) map[string]string {
	substitute := func(k string) string {
		// Undefined variables default to "", just like Bash
		return env[k]
	}

	labels := make(map[string]string, len(rawLabels))
	for key, rawLabel := range rawLabels {
		labels[key] = os.Expand(rawLabel, substitute)
	}
	return labels
}

// newInstanceID generates a random (version 4) UUID, used to populate the
// service.instance.id resource attribute.
func newInstanceID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		logger.Noticef("Cannot generate random service instance ID: %v", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
