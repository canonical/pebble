// Copyright (c) 2014-2020 Canonical Ltd
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

package timing

import (
	"encoding/json"
	"time"
)

var timeNow = func() time.Time {
	return time.Now()
}

var spanDuration = func(a, b uint64) time.Duration {
	return time.Duration(b - a)
}

// Span represents a measured time span with optional nested measurements.
// The span lasts from A to B, which are both deltas in nanoseconds since
// the root's Base, which is itself the number of seconds since unix epoch.
type Span struct {
	Label   string            `json:"label,omitempty"`
	Summary string            `json:"summary,omitempty"`
	Depth   int               `json:"depth,omitempty"`
	Spans   []*Span           `json:"spans,omitempty"`
	Tags    map[string]string `json:"tags,omitempty"`
	Base    uint64            `json:"base,omitempty"`
	A       uint64            `json:"a,omitempty"`
	B       uint64            `json:"b,omitempty"`

	base time.Time
}

// Start starts a timing span object. Tags provide information to
// identify the timing when retrieving or observing it.
func Start(label, summary string, tags map[string]string) *Span {
	now := timeNow()
	span := &Span{
		Label:   label,
		Summary: summary,
		Tags:    tags,
		Base:    uint64(now.Unix()),
	}
	// Preserve the monotonic clock so all operations are monotonic to this instant.
	// See the documentation of the time package for details on monotonic clocks.
	span.base = now.Add(time.Duration(-now.Nanosecond()))
	return span
}

// Tag decorates the span with a tag for retrieving and observing it.
func (s *Span) Tag(tag, value string) {
	if s.Tags == nil {
		s.Tags = make(map[string]string)
	}
	s.Tags[tag] = value
}

// StartNested starts a nested time span measurement which
// stops when the returned span's Stop method is called.
func (s *Span) StartNested(label, summary string) *Span {
	span := &Span{
		Depth:   s.Depth + 1,
		Label:   label,
		Summary: summary,
		A:       uint64(timeNow().Sub(s.base)),
		base:    s.base,
	}
	s.Spans = append(s.Spans, span)
	return span
}

// MinNestedSpan defines the minimum duration for nested spans to not be elided.
var MinNestedSpan = 5 * time.Millisecond

// Stop stops the measurement.
func (s *Span) Stop() {
	if s.B > 0 {
		return // Previously stopped.
	}
	s.B = uint64(timeNow().Sub(s.base))
	// Look for nested spans that are too fast to matter and drop them.
	for i := len(s.Spans) - 1; i >= 0; i-- {
		span := s.Spans[i]
		if spanDuration(span.A, span.B) < MinNestedSpan {
			if i < len(s.Spans) {
				copy(s.Spans[i:], s.Spans[i+1:])
			}
			s.Spans = s.Spans[:len(s.Spans)-1]
		}
	}
}

type jsonSpan Span

type flatSpan struct {
	*jsonSpan
	Spans []flatSpan `json:"spans,omitempty"`
}

func (s *Span) MarshalJSON() ([]byte, error) {
	flat := make([]flatSpan, 0, countSpans(s))
	flat = flattenSpans(flat, s.Spans)
	return json.Marshal(flatSpan{(*jsonSpan)(s), flat})
}

func flattenSpans(flat []flatSpan, nested []*Span) []flatSpan {
	for _, s := range nested {
		flat = append(flat, flatSpan{(*jsonSpan)(s), nil})
		if len(s.Spans) > 0 {
			flat = flattenSpans(flat, s.Spans)
		}
	}
	return flat
}

func countSpans(s *Span) int {
	count := 1
	for _, n := range s.Spans {
		if len(n.Spans) == 0 {
			count += 1
		} else {
			count += countSpans(n)
		}
	}
	return count
}
