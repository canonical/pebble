// Copyright (c) 2025 Canonical Ltd
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

package metrics_test

import (
	"bytes"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/metrics"
)

func Test(t *testing.T) {
	TestingT(t)
}

type OpenTelemetryWriterSuite struct{}

var _ = Suite(&OpenTelemetryWriterSuite{})

func (s *OpenTelemetryWriterSuite) TestOpenTelemetryWriter(c *C) {
	testCases := []struct {
		name     string
		metric   metrics.Metric
		expected string
	}{
		{
			name: "CounterInt",
			metric: metrics.Metric{
				Name:       "my_counter",
				Type:       metrics.TypeCounterInt,
				ValueInt64: 42,
				Comment:    "A simple counter",
				Labels: []metrics.Label{
					metrics.NewLabel("key1", "value1"),
					metrics.NewLabel("key2", "value2"),
				},
			},
			expected: `
# HELP my_counter A simple counter
# TYPE my_counter counter
my_counter{key1="value1",key2="value2"} 42
`[1:],
		},
		{
			name: "GaugeInt",
			metric: metrics.Metric{
				Name:       "my_gauge",
				Type:       metrics.TypeGaugeInt,
				ValueInt64: 1,
				Comment:    "A simple gauge",
				Labels:     []metrics.Label{}, // Test with no labels
			},
			expected: `
# HELP my_gauge A simple gauge
# TYPE my_gauge gauge
my_gauge 1
`[1:],
		},
		{
			name: "NoComment", // Test without comment
			metric: metrics.Metric{
				Name:       "no_comment_metric",
				Type:       metrics.TypeCounterInt,
				ValueInt64: 42,
				Labels:     []metrics.Label{metrics.NewLabel("env", "prod")},
			},
			expected: `
# TYPE no_comment_metric counter
no_comment_metric{env="prod"} 42
`[1:],
		},

		{
			name: "SpecialCharacters", // Test with special characters in labels
			metric: metrics.Metric{
				Name:       "special_chars",
				Type:       metrics.TypeGaugeInt,
				ValueInt64: 42,
				Comment:    "Metric with special characters",
				Labels: []metrics.Label{
					metrics.NewLabel("key_with_underscore", "value_with_underscore"),
					metrics.NewLabel("key-with-dash", "value-with-dash"),
				},
			},
			expected: `
# HELP special_chars Metric with special characters
# TYPE special_chars gauge
special_chars{key_with_underscore="value_with_underscore",key-with-dash="value-with-dash"} 42
`[1:],
		},
	}

	for _, tc := range testCases {
		buf := &bytes.Buffer{}
		writer := metrics.NewOpenTelemetryWriter(buf)
		err := writer.Write(tc.metric)
		c.Assert(err, IsNil)
		c.Assert(buf.String(), Equals, tc.expected)
	}
}
