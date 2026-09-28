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
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	metricNameRegex = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)
	labelNameRegex  = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)
)

type MetricType int

const (
	TypeCounterInt MetricType = iota + 1
	TypeGaugeInt
)

func (mt MetricType) String() string {
	switch mt {
	case TypeCounterInt:
		return "counter"
	case TypeGaugeInt:
		return "gauge"
	default:
		panic(fmt.Sprintf("internal error: invalid metric type %d", mt))
	}
}

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

// OpenTelemetryWriter implements the Writer interface and formats metrics in
// OpenMetrics exposition format.
//
// Metrics are buffered by name and written out by Flush, because the format
// requires every sample of a metric under a single HELP and TYPE line, while
// callers hand metrics over one service or check at a time.
type OpenTelemetryWriter struct {
	w        io.Writer
	order    []string
	families map[string]*metricFamily
}

type metricFamily struct {
	metricType MetricType
	comment    string
	samples    []Metric
}

// NewOpenTelemetryWriter creates a new OpenTelemetryWriter.
func NewOpenTelemetryWriter(w io.Writer) *OpenTelemetryWriter {
	return &OpenTelemetryWriter{w: w, families: make(map[string]*metricFamily)}
}

// Write buffers a metric under its family.
func (otw *OpenTelemetryWriter) Write(m Metric) error {
	if err := validateMetric(m); err != nil {
		return err
	}
	family, ok := otw.families[m.Name]
	if !ok {
		family = &metricFamily{metricType: m.Type, comment: m.Comment}
		otw.families[m.Name] = family
		otw.order = append(otw.order, m.Name)
	}
	family.samples = append(family.samples, m)
	return nil
}

// Flush writes every buffered metric to the underlying writer.
func (otw *OpenTelemetryWriter) Flush() error {
	order := otw.order
	families := otw.families
	otw.order = nil
	otw.families = make(map[string]*metricFamily)

	for _, name := range order {
		if err := otw.writeFamily(name, families[name]); err != nil {
			return err
		}
	}
	return nil
}

func validateMetric(m Metric) error {
	if !metricNameRegex.MatchString(m.Name) {
		return fmt.Errorf("cannot write metric with invalid name %q", m.Name)
	}
	if err := validateText(m.Comment); err != nil {
		return fmt.Errorf("cannot write metric with invalid help comment: %w", err)
	}
	for _, label := range m.Labels {
		if !labelNameRegex.MatchString(label.key) {
			return fmt.Errorf("cannot write metric with invalid label name %q", label.key)
		}
		if err := validateText(label.value); err != nil {
			return fmt.Errorf("cannot write metric with invalid value for label %q: %w", label.key, err)
		}
	}
	return nil
}

func validateText(value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("invalid UTF-8")
	}
	for _, r := range value {
		if r < 0x20 && r != '\n' {
			return fmt.Errorf("unsupported control character")
		}
	}
	return nil
}

func (otw *OpenTelemetryWriter) writeFamily(name string, family *metricFamily) error {
	if family.comment != "" {
		if _, err := fmt.Fprintf(otw.w, "# HELP %s %s\n", name, escapeHelp(family.comment)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(otw.w, "# TYPE %s %s\n", name, family.metricType); err != nil {
		return err
	}
	for _, m := range family.samples {
		if err := otw.writeSample(m); err != nil {
			return err
		}
	}
	_, err := io.WriteString(otw.w, "\n")
	return err
}

func (otw *OpenTelemetryWriter) writeSample(m Metric) error {
	if _, err := io.WriteString(otw.w, m.Name); err != nil {
		return err
	}
	if len(m.Labels) > 0 {
		if _, err := io.WriteString(otw.w, "{"); err != nil {
			return err
		}
		for i, label := range m.Labels {
			if i > 0 {
				if _, err := io.WriteString(otw.w, ","); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintf(otw.w, "%s=\"%s\"", label.key, escapeLabelValue(label.value)); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(otw.w, "}"); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(otw.w, " %d\n", m.ValueInt64)
	return err
}

func escapeHelp(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	return strings.ReplaceAll(value, "\n", `\n`)
}

func escapeLabelValue(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return strings.ReplaceAll(value, "\n", `\n`)
}
