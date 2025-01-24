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
	"sort"
	"strings"
)

// Metric represents a single metric.
type Metric struct {
	Name       string
	Value      interface{}
	LabelPairs []string
}

// WriteTo writes the metric in OpenMetrics format.
func (m *Metric) WriteTo(w io.Writer) (n int64, err error) {
	labelStr := ""
	if len(m.LabelPairs) > 0 {
		sort.Strings(m.LabelPairs)
		labelStr = "{" + strings.Join(m.LabelPairs, ",") + "}"
	}

	var written int
	switch v := m.Value.(type) {
	case int64:
		written, err = fmt.Fprintf(w, "%s%s %d\n", m.Name, labelStr, v)
	case float64:
		written, err = fmt.Fprintf(w, "%s%s %.2f\n", m.Name, labelStr, v) // Format float appropriately
	default:
		written, err = fmt.Fprintf(w, "%s%s %v\n", m.Name, labelStr, m.Value)
	}

	return int64(written), err
}
