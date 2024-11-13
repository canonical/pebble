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
		metrics: make(map[string]*Metric),
	}
}

func (s *RegistryTestSuite) TestCounter(c *C) {
	s.registry.NewMetric("test_counter", MetricTypeCounter, "Test counter")
	s.registry.IncCounter("test_counter")
	s.registry.IncCounter("test_counter")
	c.Check(s.registry.metrics["test_counter"].value.(int64), Equals, int64(2))
}

func (s *RegistryTestSuite) TestGauge(c *C) {
	s.registry.NewMetric("test_gauge", MetricTypeGauge, "Test gauge")
	s.registry.SetGauge("test_gauge", 10)
	c.Check(s.registry.metrics["test_gauge"].value.(int64), Equals, int64(10))
	s.registry.SetGauge("test_gauge", 20)
	c.Check(s.registry.metrics["test_gauge"].value.(int64), Equals, int64(20))
}

func (s *RegistryTestSuite) TestHistogram(c *C) {
	s.registry.NewMetric("test_histogram", MetricTypeHistogram, "Test histogram")
	s.registry.ObserveHistogram("test_histogram", 1.0)
	s.registry.ObserveHistogram("test_histogram", 2.0)
	histogramValues := s.registry.metrics["test_histogram"].value.([]float64)
	c.Check(len(histogramValues), Equals, 2)
	c.Check(histogramValues[0], Equals, 1.0)
	c.Check(histogramValues[1], Equals, 2.0)
}

func (s *RegistryTestSuite) TestGatherMetrics(c *C) {
	s.registry.NewMetric("test_counter", MetricTypeCounter, "Test counter")
	s.registry.IncCounter("test_counter")
	metricsOutput := s.registry.GatherMetrics()
	expectedOutput := "# HELP test_counter Test counter\n# TYPE test_counter counter\ntest_counter 1\n"
	c.Check(metricsOutput, Equals, expectedOutput)
}

func (s *RegistryTestSuite) TestRaceConditions(c *C) {
	s.registry.NewMetric("race_counter", MetricTypeCounter, "Race counter")
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.registry.IncCounter("race_counter")
		}()
	}
	wg.Wait()
	c.Check(s.registry.metrics["race_counter"].value.(int64), Equals, int64(1000))
}
