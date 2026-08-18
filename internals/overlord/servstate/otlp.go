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

package servstate

import (
	"fmt"
	"time"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/servicelog"
)

// SetOTLPAddress sets the host:port address of Pebble's local OTLP receiver.
// This is used to build the OTEL_EXPORTER_OTLP_XXX_ENDPOINT env vars injected
// into services.
func (m *ServiceManager) SetOTLPAddress(addr string) {
	m.otlpAddrLock.Lock()
	defer m.otlpAddrLock.Unlock()
	m.otlpAddr = addr
}

// otlpAddress returns the address previously set by
// SetOTLPLogsReceiverAddress.
func (m *ServiceManager) otlpAddress() string {
	m.otlpAddrLock.Lock()
	defer m.otlpAddrLock.Unlock()
	return m.otlpAddr
}

// OTLPLogsEnabled reports whether the named service is currently enrolled
// in an opentelemetry log target in the plan.
func (m *ServiceManager) OTLPLogsEnabled(name string) bool {
	currentPlan := m.getPlan()
	service, ok := currentPlan.Services[name]
	if !ok {
		return false
	}
	return otlpLogsEnabledFor(currentPlan, service)
}

// otlpLogsEnabledFor reports whether a service is enrolled in at least one
// opentelemetry log target.
func otlpLogsEnabledFor(p *plan.Plan, service *plan.Service) bool {
	for _, target := range p.LogTargets {
		if target.Type != plan.OpenTelemetryTarget {
			continue
		}
		if service.LogsTo(target) {
			return true
		}
	}
	return false
}

// injectOTLPLogsEnv sets the OTLP logs env vars unless a value already exists
// for that key.
func injectOTLPLogsEnv(env map[string]string, receiverAddr, serviceName string) {
	if receiverAddr == "" {
		return
	}
	setIfAbsent := func(key, value string) {
		if _, ok := env[key]; !ok {
			env[key] = value
		}
	}
	endpoint := fmt.Sprintf("http://%s/v1/services/%s/otlp/v1/logs", receiverAddr, serviceName)
	setIfAbsent("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", endpoint)
	setIfAbsent("OTEL_EXPORTER_OTLP_LOGS_PROTOCOL", "http/json")
	setIfAbsent("OTEL_LOGS_EXPORTER", "otlp")
	setIfAbsent("OTEL_SERVICE_NAME", serviceName)
}

// OTLPMetricsEnabled reports whether the named service is currently enrolled
// in an opentelemetry metrics target.
func (m *ServiceManager) OTLPMetricsEnabled(name string) bool {
	currentPlan := m.getPlan()
	service, ok := currentPlan.Services[name]
	if !ok {
		return false
	}
	return otlpMetricsEnabledFor(currentPlan, service)
}

// otlpMetricsEnabledFor reports whether a service is enrolled in at least
// one opentelemetry metrics target.
func otlpMetricsEnabledFor(p *plan.Plan, service *plan.Service) bool {
	for _, target := range p.MetricTargets {
		if target.Type != plan.OpenTelemetryMetricTarget {
			continue
		}
		if service.MetricsTo(target) {
			return true
		}
	}
	return false
}

// injectOTLPMetricsEnv sets the OTLP metrics env vars unless a value already
// exists for that key.
func injectOTLPMetricsEnv(env map[string]string, receiverAddr, serviceName string) {
	if receiverAddr == "" {
		return
	}
	setIfAbsent := func(key, value string) {
		if _, ok := env[key]; !ok {
			env[key] = value
		}
	}
	endpoint := fmt.Sprintf("http://%s/v1/services/%s/otlp/v1/metrics", receiverAddr, serviceName)
	setIfAbsent("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", endpoint)
	setIfAbsent("OTEL_EXPORTER_OTLP_METRICS_PROTOCOL", "http/json")
	setIfAbsent("OTEL_METRICS_EXPORTER", "otlp")
	setIfAbsent("OTEL_SERVICE_NAME", serviceName)
}

// OTLPTracesEnabled reports whether the named service is currently enrolled
// in an opentelemetry trace target.
func (m *ServiceManager) OTLPTracesEnabled(name string) bool {
	currentPlan := m.getPlan()
	service, ok := currentPlan.Services[name]
	if !ok {
		return false
	}
	return otlpTracesEnabledFor(currentPlan, service)
}

// otlpTracesEnabledFor reports whether a service is enrolled in at least
// one opentelemetry trace target.
func otlpTracesEnabledFor(p *plan.Plan, service *plan.Service) bool {
	for _, target := range p.TraceTargets {
		if target.Type != plan.OpenTelemetryTraceTarget {
			continue
		}
		if service.TracesTo(target) {
			return true
		}
	}
	return false
}

// injectOTLPTracesEnv sets the OTLP trace env vars unless a value already
// exists for that key.
func injectOTLPTracesEnv(env map[string]string, receiverAddr, serviceName string) {
	if receiverAddr == "" {
		return
	}
	setIfAbsent := func(key, value string) {
		if _, ok := env[key]; !ok {
			env[key] = value
		}
	}
	endpoint := fmt.Sprintf("http://%s/v1/services/%s/otlp/v1/traces", receiverAddr, serviceName)
	setIfAbsent("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", endpoint)
	setIfAbsent("OTEL_EXPORTER_OTLP_TRACES_PROTOCOL", "http/json")
	setIfAbsent("OTEL_TRACES_EXPORTER", "otlp")
	setIfAbsent("OTEL_SERVICE_NAME", serviceName)
}

// WriteServiceLog writes a single log entry with an explicit timestamp into the
// named service's ring buffer.
func (m *ServiceManager) WriteServiceLog(name string, t time.Time, message string) bool {
	m.servicesLock.Lock()
	service := m.services[name]
	m.servicesLock.Unlock()
	if service == nil || service.logs == nil {
		return false
	}
	err := servicelog.WriteEntry(service.logs, name, t, message)
	if err != nil {
		logger.Noticef("Cannot write OTLP log entry for service %q: %v", name, err)
	}
	return true
}
