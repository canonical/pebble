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

package metrics

import (
	"fmt"
	"io"
	"strings"
)

type MetricType int

const (
	TypeCounterInt MetricType = iota
	TypeGaugeInt
)

// Metric represents a single metric.
type Metric struct {
	Name       string
	Type       MetricType
	ValueInt64 int64
	Comment    string
	Labels     []Label
}

// Label represents a label for metrics.
type Label struct {
	key   string
	value string
}

// NewLabel creates a new Label with key and value.
func NewLabel(key, value string) Label {
	return Label{key, value}
}

type Writer interface {
	Write(Metric) error
}

// OpenTelemetryWriter implements the Writer interface and formats metrics
// in OpenTelemetryWriter exposition format.
type OpenTelemetryWriter struct {
	w io.Writer
}

// NewOpenTelemetryWriter creates a new OpenTelemetryWriter.
func NewOpenTelemetryWriter(w io.Writer) *OpenTelemetryWriter {
	return &OpenTelemetryWriter{w: w}
}

func (otw *OpenTelemetryWriter) Write(m Metric) error {
	var metricType string
	switch m.Type {
	case TypeCounterInt:
		metricType = "counter"
	case TypeGaugeInt:
		metricType = "gauge"
	}

	_, err := fmt.Fprintf(otw.w, "# HELP %s %s\n", m.Name, m.Comment)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(otw.w, "# TYPE %s %s\n", m.Name, metricType)
	if err != nil {
		return err
	}

	labels := make([]string, len(m.Labels))
	for i, label := range m.Labels {
		labels[i] = fmt.Sprintf("%s=%s", label.key, label.value)
	}

	_, err = fmt.Fprintf(otw.w, "%s{%s} %d\n", m.Name, strings.Join(labels, ","), m.ValueInt64)
	return err
}
