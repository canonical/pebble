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
	"sync"
	"testing"

	. "gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&RegistryTestSuite{})

// Test Suite structure
type RegistryTestSuite struct {
	registry *MetricsRegistry
}

func (s *RegistryTestSuite) SetUpTest(c *C) {
	s.registry = &MetricsRegistry{
		metricVecs: make(map[string]*MetricVec),
	}
}

func (s *RegistryTestSuite) TestCounterWithoutLabels(c *C) {
	labels := []string{}
	testCounter := s.registry.NewCounterVec("test_counter", "Total number of something processed", labels)
	testCounter.WithLabelValues().Inc()
	c.Check(s.registry.metricVecs["test_counter"].metrics[formatLabelKey(labels, []string{})].value.(int64), Equals, int64(1))
	testCounter.WithLabelValues().Inc()
	c.Check(s.registry.metricVecs["test_counter"].metrics[formatLabelKey(labels, []string{})].value.(int64), Equals, int64(2))
}

func (s *RegistryTestSuite) TestCounterWithLabels(c *C) {
	labels := []string{"operation", "status"}
	testCounter := s.registry.NewCounterVec("test_counter", "Total number of something processed", labels)
	testCounter.WithLabelValues("read", "success").Inc()
	c.Check(s.registry.metricVecs["test_counter"].metrics[formatLabelKey(labels, []string{"read", "success"})].value.(int64), Equals, int64(1))
	testCounter.WithLabelValues("write", "fail").Add(2)
	c.Check(s.registry.metricVecs["test_counter"].metrics[formatLabelKey(labels, []string{"write", "fail"})].value.(int64), Equals, int64(2))
}

func (s *RegistryTestSuite) TestGauge(c *C) {
	labels := []string{"sensor"}
	testGauge := s.registry.NewGaugeVec("test_gauge", "Current value of something", labels)
	testGauge.WithLabelValues("temperature").Set(10.0)
	c.Check(s.registry.metricVecs["test_gauge"].metrics[formatLabelKey(labels, []string{"temperature"})].value.(float64), Equals, float64(10.0))
	testGauge.WithLabelValues("temperature").Set(20.0)
	c.Check(s.registry.metricVecs["test_gauge"].metrics[formatLabelKey(labels, []string{"temperature"})].value.(float64), Equals, float64(20.0))
}

func (s *RegistryTestSuite) TestGatherMetrics(c *C) {
	testCounter := s.registry.NewCounterVec("test_counter", "Total number of something processed", []string{"operation", "status"})
	testCounter.WithLabelValues("read", "success").Inc()
	testGauge := s.registry.NewGaugeVec("test_gauge", "Current value of something", []string{"sensor"})
	testGauge.WithLabelValues("temperature").Set(10.0)
	metricsOutput := s.registry.GatherMetrics()
	expectedOutput := "# HELP test_counter Total number of something processed\n# TYPE test_counter counter\ntest_counter{operation=read,status=success} 1\n"
	expectedOutput += "# HELP test_gauge Current value of something\n# TYPE test_gauge gauge\ntest_gauge{sensor=temperature} 10.00\n"
	c.Check(metricsOutput, Equals, expectedOutput)
}

func (s *RegistryTestSuite) TestGatherMetricsWithoutLabels(c *C) {
	testCounter := s.registry.NewCounterVec("test_counter", "Total number of something processed", []string{})
	testCounter.WithLabelValues().Inc()
	metricsOutput := s.registry.GatherMetrics()
	expectedOutput := "# HELP test_counter Total number of something processed\n# TYPE test_counter counter\ntest_counter 1\n"
	c.Check(metricsOutput, Equals, expectedOutput)
}

func (s *RegistryTestSuite) TestRaceConditions(c *C) {
	counter := s.registry.NewCounterVec("test_counter", "Total number of something processed", []string{})
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			counter.WithLabelValues().Inc()
		}()
	}
	wg.Wait()
	c.Check(s.registry.metricVecs["test_counter"].metrics[formatLabelKey([]string{}, []string{})].value.(int64), Equals, int64(1000))
}
