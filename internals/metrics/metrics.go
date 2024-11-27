// Copyright (c) 2024 Canonical Ltd
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

package metrics

import (
	"fmt"
	"math/rand"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
)

// MetricType models the type of a metric.
type MetricType string

const (
	MetricTypeCounter MetricType = "counter"
	MetricTypeGauge   MetricType = "gauge"
)

// MetricVec represents a collection of metrics with the same name and help but different label values.
type MetricVec struct {
	Name    string
	Help    string
	Type    MetricType
	metrics map[string]*Metric // Key is the label values string (e.g., "region=foo,service=bar")
	labels  []string           // Label names (e.g., ["region", "service"])
	mu      sync.RWMutex
}

// Metric represents a single metric within a MetricVec.
type Metric struct {
	LabelValues map[string]string // Label values for this metric
	value       interface{}       // Can be int64 for counter/gauge, or []float64 for histogram.
	mu          sync.RWMutex      // Mutex for individual metric
}

// MetricsRegistry stores and manages metric vectors.
type MetricsRegistry struct {
	metricVecs map[string]*MetricVec
	mu         sync.RWMutex
}

// Package-level variable to hold the single registry.
var registry *MetricsRegistry

// Ensure registry is initialized only once.
var once sync.Once

// GetRegistry returns the singleton MetricsRegistry instance.
func GetRegistry() *MetricsRegistry {
	once.Do(func() {
		registry = &MetricsRegistry{
			metricVecs: make(map[string]*MetricVec),
		}
	})
	return registry
}

func formatLabelKey(labels []string, labelValues []string) string {
	labelPairs := make([]string, len(labels))
	for i := range labels {
		labelPairs[i] = labels[i] + "=" + labelValues[i]
	}

	// Sort labels for consistency
	sort.Strings(labelPairs)
	return strings.Join(labelPairs, ",")
}

// NewCounterVec creates a new counter vector.
func (r *MetricsRegistry) NewCounterVec(name, help string, labels []string) *MetricVec {
	return r.newMetricVec(name, help, labels, MetricTypeCounter)
}

// NewGaugeVec creates a new gauge vector.
func (r *MetricsRegistry) NewGaugeVec(name, help string, labels []string) *MetricVec {
	return r.newMetricVec(name, help, labels, MetricTypeGauge)
}

// newMetricVec is a helper function to create a new metric vector of any type.
func (r *MetricsRegistry) newMetricVec(name, help string, labels []string, metricType MetricType) *MetricVec {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.metricVecs[name]; ok {
		panic(fmt.Sprintf("metric with name %s already registered", name))
	}
	vec := &MetricVec{
		Name:    name,
		Help:    help,
		Type:    metricType,
		metrics: make(map[string]*Metric),
		labels:  labels,
	}
	r.metricVecs[name] = vec
	return vec
}

// WithLabelValues gets or creates a metric with the specified label values.
func (v *MetricVec) WithLabelValues(labelValues ...string) *Metric {
	if len(labelValues) != len(v.labels) {
		panic(fmt.Errorf(
			"%q has %d variable labels named %q but %d values %q were provided",
			v,
			len(v.labels),
			v.labels,
			len(labelValues),
			labelValues,
		))
	}

	labelKey := formatLabelKey(v.labels, labelValues)

	v.mu.RLock()
	metric, ok := v.metrics[labelKey]
	v.mu.RUnlock()

	if !ok {
		v.mu.Lock()
		defer v.mu.Unlock()
		// Double check locking
		metric, ok = v.metrics[labelKey]

		if !ok {
			metric = &Metric{
				LabelValues: make(map[string]string),
				value:       int64(0),
			}
			for i, label := range v.labels {
				metric.LabelValues[label] = labelValues[i]
			}
			v.metrics[labelKey] = metric
		}
	}

	return metric
}

// Inc increments the counter.
func (m *Metric) Inc() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.value = m.value.(int64) + 1
}

// Add adds the given value to the counter.
func (m *Metric) Add(value int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch v := m.value.(type) {
	case int64:
		m.value = v + value
	default:
		// if the metric is not a counter (e.g., a gauge)
		panic(fmt.Errorf("add called on a non-counter metric: %T", m.value))
	}
}

// Set sets the gauge value.
func (m *Metric) Set(value float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.value = value
}

// GatherMetrics function (modified slightly)
func (r *MetricsRegistry) GatherMetrics() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var output string

	for _, vec := range r.metricVecs {
		output += fmt.Sprintf("# HELP %s %s\n", vec.Name, vec.Help)
		output += fmt.Sprintf("# TYPE %s %s\n", vec.Name, vec.Type)

		for labelKey, metric := range vec.metrics {
			switch v := metric.value.(type) {
			case int64:
				output += fmt.Sprintf("%s{%s} %d\n", vec.Name, labelKey, v)
			case float64:
				output += fmt.Sprintf("%s{%s} %f\n", vec.Name, labelKey, v)
			default:
				// Fallback for other types
				output += fmt.Sprintf("%s{%s} %v\n", vec.Name, labelKey, v)
			}
		}
	}

	return output
}

// Example usage
func main() {
	// Get the singleton registry
	registry := GetRegistry()

	myCounter := registry.NewCounterVec("my_counter", "Total number of something processed.", []string{"operation", "status"})
	myGauge := registry.NewGaugeVec("my_gauge", "Current value of something.", []string{"sensor"})

	// Goroutine to update metrics randomly
	go func() {
		for {
			// Use like prometheus client library.

			// counter
			myCounter.WithLabelValues("read", "success").Inc()
			myCounter.WithLabelValues("write", "success").Add(2)
			myCounter.WithLabelValues("read", "failed").Inc()

			// gauge
			myGauge.WithLabelValues("temperature").Set(20.0 + rand.Float64()*10.0)

			time.Sleep(time.Duration(rand.Intn(5)+1) * time.Second) // Random sleep between 1 and 5 seconds
		}
	}()
	// Use Gorilla Mux router
	router := mux.NewRouter()
	router.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(registry.GatherMetrics()))
	})

	// Serve on port 2112
	fmt.Println("Serving metrics on :2112/metrics")
	err := http.ListenAndServe(":2112", router) // Use the router here
	if err != nil {
		fmt.Println("Error starting server:", err)
	}
}
