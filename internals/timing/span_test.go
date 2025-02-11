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

package timing_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/testutil"
	"github.com/canonical/pebble/internals/timing"
)

func Test(t *testing.T) { TestingT(t) }

type spanSuite struct {
	testutil.BaseTest
	st       *state.State
	duration time.Duration
	fakeTime time.Time
}

var _ = Suite(&spanSuite{})

func (s *spanSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.st = state.New(nil)
	s.duration = 0

	s.fakeTimeNow(c)
	s.fakeMinNestedSpan(0)
}

func (s *spanSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *spanSuite) fakeTimeNow(c *C) {
	t, err := time.Parse(time.RFC3339, "2020-01-01T01:01:00.0Z")
	c.Assert(err, IsNil)
	s.fakeTime = t
	// Increase fakeTime by 1 millisecond on each call, and report it as current time
	s.AddCleanup(timing.FakeTimeNow(func() time.Time {
		s.fakeTime = s.fakeTime.Add(time.Millisecond)
		return s.fakeTime
	}))
}

func (s *spanSuite) fakeSpanDuration(c *C) {
	// Increase duration by 1 millisecond on each call
	s.AddCleanup(timing.FakeSpanDuration(func(a, b uint64) time.Duration {
		c.Check(a < b, Equals, true)
		s.duration += time.Millisecond
		return s.duration
	}))
}

func (s *spanSuite) fakeMinNestedSpan(threshold time.Duration) {
	oldThreshold := timing.MinNestedSpan
	timing.MinNestedSpan = threshold
	restore := func() {
		timing.MinNestedSpan = oldThreshold
	}
	s.AddCleanup(restore)
}

func encodeDecode(span *timing.Span) any {
	data, err := json.Marshal(span)
	if err != nil {
		panic(err)
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewBuffer(data))
	decoder.UseNumber()
	err = decoder.Decode(&decoded)
	if err != nil {
		panic(err)
	}
	return decoded
}

func (s *spanSuite) TestSave(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	// Two timings, with 2 nested measures.
	var spans []*timing.Span
	for i := 0; i < 2; i++ {
		span1 := timing.Start("", "", map[string]string{"task": fmt.Sprint(i)})
		span1.Tag("change", "12")
		span2 := span1.StartNested("First level", "...")
		span3 := span2.StartNested("Second level", "...")
		span3.Stop()
		span2.Stop()
		span1.Stop()

		spans = append(spans, span1)
	}

	decoded0 := encodeDecode(spans[0])
	decoded1 := encodeDecode(spans[1])

	c.Assert(decoded0, DeepEquals, map[string]any{
		"tags": map[string]any{
			"change": "12",
			"task":   "0",
		},
		"base": json.Number("1577840460"),
		"b":    json.Number("6000000"),
		"spans": []any{
			map[string]any{
				"label":   "First level",
				"summary": "...",
				"depth":   json.Number("1"),
				"a":       json.Number("2000000"),
				"b":       json.Number("5000000"),
			},
			map[string]any{
				"label":   "Second level",
				"summary": "...",
				"depth":   json.Number("2"),
				"a":       json.Number("3000000"),
				"b":       json.Number("4000000"),
			},
		},
	})
	c.Assert(decoded1, DeepEquals, map[string]any{
		"tags": map[string]any{
			"change": "12",
			"task":   "1",
		},
		"base": json.Number("1577840460"),
		"b":    json.Number("12000000"),
		"spans": []any{
			map[string]any{
				"label":   "First level",
				"summary": "...",
				"depth":   json.Number("1"),
				"a":       json.Number("8000000"),
				"b":       json.Number("11000000"),
			},
			map[string]any{
				"label":   "Second level",
				"summary": "...",
				"depth":   json.Number("2"),
				"a":       json.Number("9000000"),
				"b":       json.Number("10000000"),
			},
		},
	})
}

func (s *spanSuite) testDurationThreshold(c *C, threshold time.Duration, expected any) {
	s.fakeMinNestedSpan(threshold)

	span1 := timing.Start("", "", nil)
	span2 := span1.StartNested("main", "...")
	span3 := span2.StartNested("nested", "...")
	span4 := span3.StartNested("nested more", "...")
	span4.Stop()
	span3.Stop()
	span2.Stop()
	span1.Stop()

	c.Assert(encodeDecode(span1), DeepEquals, expected)
}

func (s *spanSuite) TestDurationThresholdAll(c *C) {
	s.testDurationThreshold(c, 0, map[string]any{
		"base": json.Number("1577840460"),
		"b":    json.Number("8000000"),
		"spans": []any{
			map[string]any{
				"label":   "main",
				"summary": "...",
				"depth":   json.Number("1"),
				"a":       json.Number("2000000"),
				"b":       json.Number("7000000"),
			},
			map[string]any{
				"label":   "nested",
				"summary": "...",
				"depth":   json.Number("2"),
				"a":       json.Number("3000000"),
				"b":       json.Number("6000000"),
			},
			map[string]any{
				"label":   "nested more",
				"summary": "...",
				"depth":   json.Number("3"),
				"a":       json.Number("4000000"),
				"b":       json.Number("5000000"),
			},
		},
	})
}

func (s *spanSuite) TestDurationThreshold(c *C) {
	s.testDurationThreshold(c, 3000000, map[string]any{
		"base": json.Number("1577840460"),
		"b":    json.Number("8000000"),
		"spans": []any{
			map[string]any{
				"label":   "main",
				"summary": "...",
				"depth":   json.Number("1"),
				"a":       json.Number("2000000"),
				"b":       json.Number("7000000"),
			},
			map[string]any{
				"label":   "nested",
				"summary": "...",
				"depth":   json.Number("2"),
				"a":       json.Number("3000000"),
				"b":       json.Number("6000000"),
			},
		},
	})
}

func (s *spanSuite) TestDurationThresholdRootOnly(c *C) {
	s.testDurationThreshold(c, 4000000, map[string]any{
		"base": json.Number("1577840460"),
		"b":    json.Number("8000000"),
		"spans": []any{
			map[string]any{
				"label":   "main",
				"summary": "...",
				"depth":   json.Number("1"),
				"a":       json.Number("2000000"),
				"b":       json.Number("7000000"),
			},
		},
	})
}
