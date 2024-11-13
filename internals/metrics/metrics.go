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
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"golang.org/x/exp/rand"
)

// MetricType models the type of a metric.
type MetricType string

const (
	MetricTypeCounter   MetricType = "counter"
	MetricTypeGauge     MetricType = "gauge"
	MetricTypeHistogram MetricType = "histogram"
)

// Metric represents a single metric.
type Metric struct {
	Name  string
	Type  MetricType
	Help  string
	value interface{} // Can be int64 for counter/gauge, or []float64 for histogram.
	mu    sync.RWMutex
}

// MetricsRegistry stores and manages metrics.
type MetricsRegistry struct {
	metrics map[string]*Metric
	mu      sync.RWMutex
}

// Package-level variable to hold the single registry.
var registry *MetricsRegistry

// Ensure registry is initialized only once.
var once sync.Once

// GetRegistry returns the singleton MetricsRegistry instance.
func GetRegistry() *MetricsRegistry {
	once.Do(func() {
		registry = &MetricsRegistry{
			metrics: make(map[string]*Metric),
		}
	})
	return registry
}

// NewMetric registers a new metric.
func (r *MetricsRegistry) NewMetric(name string, metricType MetricType, help string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.metrics[name]; ok {
		return fmt.Errorf("metric with name %s already registered", name)
	}

	var value interface{}
	switch metricType {
	case MetricTypeCounter, MetricTypeGauge:
		value = int64(0)
	case MetricTypeHistogram:
		value = make([]float64, 0)
	default:
		return fmt.Errorf("invalid metric type: %s", metricType)
	}

	r.metrics[name] = &Metric{
		Name:  name,
		Type:  metricType,
		Help:  help,
		value: value,
	}

	return nil
}

// IncCounter increments a counter metric.
func (r *MetricsRegistry) IncCounter(name string) error {
	return r.updateMetric(name, MetricTypeCounter, 1)
}

// SetGauge sets the value of a gauge metric.
func (r *MetricsRegistry) SetGauge(name string, value int64) error {
	return r.updateMetric(name, MetricTypeGauge, value)
}

// ObserveHistogram adds a value to a histogram metric.
func (r *MetricsRegistry) ObserveHistogram(name string, value float64) error {
	return r.updateMetric(name, MetricTypeHistogram, value)
}

// updateMetric updates the value of a metric.
func (r *MetricsRegistry) updateMetric(name string, metricType MetricType, value interface{}) error {
	r.mu.RLock()
	metric, ok := r.metrics[name]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("metric with name %s not found", name)
	}

	if metric.Type != metricType {
		return fmt.Errorf("mismatched metric type for %s", name)
	}

	metric.mu.Lock()
	defer metric.mu.Unlock()

	switch metricType {
	case MetricTypeCounter:
		metric.value = metric.value.(int64) + int64(value.(int))
	case MetricTypeGauge:
		metric.value = value.(int64)
	case MetricTypeHistogram:
		metric.value = append(metric.value.([]float64), value.(float64))
	}

	return nil
}

// GatherMetrics gathers all metrics and formats them in Prometheus exposition format.
func (r *MetricsRegistry) GatherMetrics() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var output string
	for _, metric := range r.metrics {
		output += fmt.Sprintf("# HELP %s %s\n", metric.Name, metric.Help)
		output += fmt.Sprintf("# TYPE %s %s\n", metric.Name, metric.Type)

		switch metric.Type {
		case MetricTypeCounter, MetricTypeGauge:
			output += fmt.Sprintf("%s %d\n", metric.Name, metric.value.(int64))
		case MetricTypeHistogram:
			for _, v := range metric.value.([]float64) {
				output += fmt.Sprintf("%s %f\n", metric.Name, v) // Basic histogram representation
			}
		}
	}

	return output
}

// Example usage
func main() {
	// Get the singleton registry
	registry := GetRegistry()

	registry.NewMetric("my_counter", MetricTypeCounter, "A simple counter")
	registry.NewMetric("my_gauge", MetricTypeGauge, "A simple gauge")
	registry.NewMetric("my_histogram", MetricTypeHistogram, "A simple histogram")

	// Goroutine to update metrics randomly
	go func() {
		for {
			// Counter
			err := registry.IncCounter("my_counter") // Increment by 1
			if err != nil {
				fmt.Println("Error incrementing counter:", err)
			}

			// Gauge
			gaugeValue := rand.Int63n(100)
			err = registry.SetGauge("my_gauge", gaugeValue)
			if err != nil {
				fmt.Println("Error setting gauge:", err)
			}

			// Histogram
			histogramValue := rand.Float64() * 10
			err = registry.ObserveHistogram("my_histogram", histogramValue)
			if err != nil {
				fmt.Println("Error observing histogram:", err)
			}

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
