// Copyright (c) 2026 Canonical Ltd
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

package schedule_test

import (
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/schedule"
	"github.com/canonical/pebble/internals/testutil"
)

func Test(t *testing.T) {
	testutil.PrintGoroutineLeaks(t, TestingT)
}

type scheduleSuite struct{}

var _ = Suite(&scheduleSuite{})

var (
	anyWeek = schedule.WeekSpan{
		Start: schedule.WeekdayPos{Weekday: time.Sunday},
		End:   schedule.WeekdayPos{Weekday: time.Saturday},
	}
	wholeDay = schedule.ClockSpan{Period: 24 * time.Hour}
)

func weekSpan(start time.Weekday, startPos schedule.Pos, end time.Weekday, endPos schedule.Pos) schedule.WeekSpan {
	return schedule.WeekSpan{
		Start: schedule.WeekdayPos{Weekday: start, Pos: startPos},
		End:   schedule.WeekdayPos{Weekday: end, Pos: endPos},
	}
}

func clockSpan(startHour, startMinute, endHour, endMinute int, period time.Duration) schedule.ClockSpan {
	return schedule.ClockSpan{
		Start:  schedule.Clock{Hour: startHour, Minute: startMinute},
		End:    schedule.Clock{Hour: endHour, Minute: endMinute},
		Period: period,
	}
}

func allYear() [12]uint32 {
	var days [12]uint32
	for m := range days {
		days[m] = 0xFFFFFFFE
	}
	return days
}

func (s *scheduleSuite) TestParseWeekSpans(c *C) {
	sched, err := schedule.Parse("mon-fri,tue3,sat,,wed5-sun")
	c.Assert(err, IsNil)
	c.Assert(sched.Sets, HasLen, 2)

	c.Check(sched.Sets[0].WeekSpans, DeepEquals, []schedule.WeekSpan{
		weekSpan(time.Monday, 0, time.Friday, 0),
		weekSpan(time.Tuesday, 3, time.Tuesday, 3),
		weekSpan(time.Saturday, 0, time.Saturday, 0),
	})
	c.Check(sched.Sets[0].ClockSpans, DeepEquals, []schedule.ClockSpan{wholeDay})
	c.Check(sched.Sets[0].Days, Equals, allYear())

	c.Check(sched.Sets[1].WeekSpans, DeepEquals, []schedule.WeekSpan{
		weekSpan(time.Wednesday, schedule.PosLast, time.Sunday, 0),
	})
	c.Check(sched.Sets[1].ClockSpans, DeepEquals, []schedule.ClockSpan{wholeDay})
	c.Check(sched.Sets[1].Days, Equals, allYear())
}

func (s *scheduleSuite) TestParseClockSpans(c *C) {
	sched, err := schedule.Parse("10:05-20:35/126,0:00-0:00,24:00,,12:34,1:00-2:00/30s,3:00-4:00/10m,5:00-7:00/1h,23:55")
	c.Assert(err, IsNil)
	c.Assert(sched.Sets, HasLen, 2)

	c.Check(sched.Sets[0].ClockSpans, DeepEquals, []schedule.ClockSpan{
		clockSpan(10, 5, 20, 35, 300*time.Second),
		clockSpan(0, 0, 0, 0, 24*time.Hour),
		clockSpan(0, 0, 0, 15, 15*time.Minute),
	})
	c.Check(sched.Sets[0].WeekSpans, DeepEquals, []schedule.WeekSpan{anyWeek})

	c.Check(sched.Sets[1].ClockSpans, DeepEquals, []schedule.ClockSpan{
		clockSpan(12, 34, 12, 49, 15*time.Minute),
		clockSpan(1, 0, 2, 0, 30*time.Second),
		clockSpan(3, 0, 4, 0, 10*time.Minute),
		clockSpan(5, 0, 7, 0, time.Hour),
		clockSpan(23, 55, 0, 10, 15*time.Minute),
	})
	c.Check(sched.Sets[1].WeekSpans, DeepEquals, []schedule.WeekSpan{anyWeek})
}

func (s *scheduleSuite) TestParseClockSpanSplit(c *C) {
	for _, test := range []struct {
		text   string
		period time.Duration
	}{
		{"8:00-10:00/3", 40 * time.Minute},
		{"8:00-10:00/5", 24 * time.Minute},
		{"8:00-10:00/30s", 30 * time.Second},
		{"8:00-10:00/15m", 15 * time.Minute},
		{"8:00-10:00/1h", time.Hour},
		{"8:00-10:00/7", 1028 * time.Second}, // Truncated to whole seconds.
		{"8:00-8:01/99999", time.Second},     // At least one second.
		{"23:00-1:00/4", 30 * time.Minute},   // Crossing midnight.
		{"0:00-0:00/24", time.Hour},          // Whole day.
	} {
		sched, err := schedule.Parse(test.text)
		c.Assert(err, IsNil, Commentf("schedule %q", test.text))
		c.Check(sched.Sets[0].ClockSpans[0].Period, Equals, test.period, Commentf("schedule %q", test.text))
	}
}

func (s *scheduleSuite) TestParseMonthSpans(c *C) {
	sched, err := schedule.Parse("dec-mar,may,5-10,jan,15-20,,jan,1-31,feb")
	c.Assert(err, IsNil)
	c.Assert(sched.Sets, HasLen, 2)

	var expected [12]uint32
	expected[0] = 0x1F87E0 // 5-10,15-20
	expected[1] = 0x07E0   // 5-10
	expected[2] = 0x07E0
	expected[4] = 0x07E0
	expected[11] = 0x07E0
	c.Check(sched.Sets[0].Days, Equals, expected)

	expected = [12]uint32{}
	expected[0] = 0xFFFFFFFE // 1-31, explicit
	expected[1] = 0xFFFFFFFE // 1-31, implicit
	c.Check(sched.Sets[1].Days, Equals, expected)
}

func (s *scheduleSuite) TestParseDayRanges(c *C) {
	sched, err := schedule.Parse("mar,29-31,apr,1-2,,jun,25-5")
	c.Assert(err, IsNil)
	c.Assert(sched.Sets, HasLen, 2)

	var expected [12]uint32
	expected[2] = 0xE0000000 // 29-31
	expected[3] = 0x06       // 1-2
	c.Check(sched.Sets[0].Days, Equals, expected)

	expected = [12]uint32{}
	expected[5] = 0xFE00003E // 25-31,1-5
	c.Check(sched.Sets[1].Days, Equals, expected)
}

func (s *scheduleSuite) TestParseMixedSets(c *C) {
	sched, err := schedule.Parse("jan,10-15,5,mon-wed2,fri,09:45,13:00-17:00/5,,feb,sat,12:34")
	c.Assert(err, IsNil)
	c.Assert(sched.Sets, HasLen, 2)

	var expected [12]uint32
	expected[0] = 0xFC20 // 5,10-15
	c.Check(sched.Sets[0].Days, Equals, expected)
	c.Check(sched.Sets[0].WeekSpans, DeepEquals, []schedule.WeekSpan{
		weekSpan(time.Monday, 0, time.Wednesday, 2),
		weekSpan(time.Friday, 0, time.Friday, 0),
	})
	c.Check(sched.Sets[0].ClockSpans, DeepEquals, []schedule.ClockSpan{
		clockSpan(9, 45, 10, 0, 15*time.Minute),
		clockSpan(13, 0, 17, 0, 48*time.Minute),
	})

	expected = [12]uint32{}
	expected[1] = 0xFFFFFFFE
	c.Check(sched.Sets[1].Days, Equals, expected)
	c.Check(sched.Sets[1].WeekSpans, DeepEquals, []schedule.WeekSpan{
		weekSpan(time.Saturday, 0, time.Saturday, 0),
	})
	c.Check(sched.Sets[1].ClockSpans, DeepEquals, []schedule.ClockSpan{
		clockSpan(12, 34, 12, 49, 15*time.Minute),
	})
}

func (s *scheduleSuite) TestParseAnyOrder(c *C) {
	ordered, err := schedule.Parse("jan,10-15,mon-wed2,09:45,13:00-17:00/5")
	c.Assert(err, IsNil)
	shuffled, err := schedule.Parse("09:45,mon-wed2,13:00-17:00/5,jan,10-15")
	c.Assert(err, IsNil)
	c.Check(shuffled.Sets, DeepEquals, ordered.Sets)
}

func (s *scheduleSuite) TestString(c *C) {
	sched, err := schedule.Parse("mon-fri,8:00-17:00,,sat,sun,10:00")
	c.Assert(err, IsNil)
	c.Check(sched.String(), Equals, "mon-fri,8:00-17:00,,sat,sun,10:00")
}

func (s *scheduleSuite) TestParseErrors(c *C) {
	for _, test := range []struct {
		text string
		err  string
	}{
		{"", `cannot parse schedule: schedule is empty`},
		{",,", `cannot parse schedule ",,": empty set`},
		{"mon,,", `cannot parse schedule "mon,,": empty set`},
		{",,mon", `cannot parse schedule ",,mon": empty set`},
		{"mon,,,tue", `cannot parse schedule "mon,,,tue": empty element`},
		{"mon,", `cannot parse schedule "mon,": empty element`},
		{",mon", `cannot parse schedule ",mon": empty element`},
		{"fry", `cannot parse schedule "fry": "fry" is not a valid month, weekday, day or time`},
		{"mon-fry", `cannot parse schedule "mon-fry": "fry" is not a valid month, weekday, day or time`},
		{"mon0", `cannot parse schedule "mon0": "mon0" is not a valid month, weekday, day or time`},
		{"mon6", `cannot parse schedule "mon6": "mon6" is not a valid month, weekday, day or time`},
		{"MON", `cannot parse schedule "MON": "MON" is not a valid month, weekday, day or time`},
		{"jan-foo", `cannot parse schedule "jan-foo": "foo" is not a valid month`},
		{"janx", `cannot parse schedule "janx": "janx" is not a valid month`},
		{"5", `cannot parse schedule "5": days "5" must follow a month`},
		{"mon,5", `cannot parse schedule "mon,5": days "5" must follow a month`},
		{"jan,0", `cannot parse schedule "jan,0": "0" is not a valid day`},
		{"jan,05", `cannot parse schedule "jan,05": "05" is not a valid day`},
		{"jan,32", `cannot parse schedule "jan,32": "32" is not a valid day`},
		{"jan,123", `cannot parse schedule "jan,123": "123" is not a valid day`},
		{"jan,1-32", `cannot parse schedule "jan,1-32": "32" is not a valid day`},
		{"jan,1-", `cannot parse schedule "jan,1-": "" is not a valid day`},
		{"9", `cannot parse schedule "9": days "9" must follow a month`},
		{"9:0", `cannot parse schedule "9:0": "9:0" is not a valid time`},
		{"9:000", `cannot parse schedule "9:000": "9:000" is not a valid time`},
		{"123:00", `cannot parse schedule "123:00": "123:00" is not a valid time`},
		{"25:00", `cannot parse schedule "25:00": "25:00" is not a valid time`},
		{"24:01", `cannot parse schedule "24:01": "24:01" is not a valid time`},
		{"9:60", `cannot parse schedule "9:60": "9:60" is not a valid time`},
		{"9:00-", `cannot parse schedule "9:00-": "" is not a valid time`},
		{"9:00-10", `cannot parse schedule "9:00-10": "10" is not a valid time`},
		{"9:00~10:00", `cannot parse schedule "9:00~10:00": "9:00~10:00" is not a valid time`},
		{"9:00/2", `cannot parse schedule "9:00/2": "9:00/2" is not a valid time`},
		{"9:00-10:00/", `cannot parse schedule "9:00-10:00/": "/" is not a valid clock span split`},
		{"9:00-10:00/0", `cannot parse schedule "9:00-10:00/0": "/0" is not a valid clock span split`},
		{"9:00-10:00/05", `cannot parse schedule "9:00-10:00/05": "/05" is not a valid clock span split`},
		{"9:00-10:00/100000", `cannot parse schedule "9:00-10:00/100000": "/100000" is not a valid clock span split`},
		{"9:00-10:00/5d", `cannot parse schedule "9:00-10:00/5d": "/5d" is not a valid clock span split`},
		{"9:00-10:00/5ms", `cannot parse schedule "9:00-10:00/5ms": "/5ms" is not a valid clock span split`},
		{"9:00-10:00/m", `cannot parse schedule "9:00-10:00/m": "/m" is not a valid clock span split`},
		{
			"mon,,mon,,mon,,mon,,mon,,mon,,mon,,mon,,mon",
			`cannot parse schedule "mon,,mon,,mon,,mon,,mon,,mon,,mon,,mon,,mon": too many sets \(maximum is 8\)`,
		}, {
			"mon,mon,mon,mon,mon,mon,mon,mon,mon",
			`cannot parse schedule "mon,mon,mon,mon,mon,mon,mon,mon,mon": too many week spans \(maximum is 8\)`,
		}, {
			"1:00,2:00,3:00,4:00,5:00,6:00,7:00,8:00,9:00",
			`cannot parse schedule "1:00,2:00,3:00,4:00,5:00,6:00,7:00,8:00,9:00": too many clock spans \(maximum is 8\)`,
		},
	} {
		_, err := schedule.Parse(test.text)
		c.Check(err, ErrorMatches, test.err, Commentf("schedule %q", test.text))
	}
}

func (s *scheduleSuite) TestParseLimits(c *C) {
	_, err := schedule.Parse("mon,,mon,,mon,,mon,,mon,,mon,,mon,,mon")
	c.Check(err, IsNil)
	_, err = schedule.Parse("mon,mon,mon,mon,mon,mon,mon,mon")
	c.Check(err, IsNil)
	_, err = schedule.Parse("1:00,2:00,3:00,4:00,5:00,6:00,7:00,8:00")
	c.Check(err, IsNil)
}

const timeLayout = "2006-01-02 15:04:05"

type nextTest struct {
	schedule string
	last     string
	now      string
	next     string // Empty if there's no next trigger.
	comment  string
}

var nextTests = []nextTest{
	// === Weeks ===
	{
		comment:  "simple single-day next week matching",
		schedule: "mon",
		last:     "2020-01-01 00:00:00",
		now:      "2020-01-09 08:45:00",
		next:     "2020-01-13 00:00:00",
	}, {
		comment:  "two week spans, first one comes first",
		schedule: "mon,tue-wed",
		last:     "2020-01-01 00:00:00",
		now:      "2020-01-09 08:45:00",
		next:     "2020-01-13 00:00:00",
	}, {
		comment:  "two week spans, second one comes first",
		schedule: "tue,mon-wed",
		last:     "2020-01-01 00:00:00",
		now:      "2020-01-09 08:45:00",
		next:     "2020-01-13 00:00:00",
	}, {
		comment:  "match following week as last and now overlap",
		schedule: "thu",
		last:     "2020-01-09 00:00:00",
		now:      "2020-01-09 08:45:00",
		next:     "2020-01-16 00:00:00",
	}, {
		comment:  "now is inside week span, but match next day as last and now overlap",
		schedule: "sun-fri",
		last:     "2020-01-09 00:00:00",
		now:      "2020-01-09 08:45:00",
		next:     "2020-01-10 00:00:00",
	}, {
		comment:  "similar case but on last day of span, so skips to following week",
		schedule: "sun-fri",
		last:     "2020-01-10 00:00:00",
		now:      "2020-01-10 08:45:00",
		next:     "2020-01-12 00:00:00",
	}, {
		comment:  "week span wrapping around the week",
		schedule: "fri-mon",
		last:     "2020-01-07 00:00:00",
		now:      "2020-01-07 08:45:00",
		next:     "2020-01-10 00:00:00",
	}, {
		comment:  "positioned weekday",
		schedule: "mon3",
		last:     "2020-01-01 00:00:00",
		now:      "2020-01-09 08:45:00",
		next:     "2020-01-20 00:00:00",
	}, {
		comment:  "positioned weekday, with range",
		schedule: "mon3-sun",
		last:     "2020-01-01 00:00:00",
		now:      "2020-01-09 08:45:00",
		next:     "2020-01-20 00:00:00",
	}, {
		comment:  "positioned weekday, inside range",
		schedule: "mon3-sun",
		last:     "2020-01-21 00:00:00",
		now:      "2020-01-21 08:45:00",
		next:     "2020-01-22 00:00:00",
	}, {
		comment:  "positioned weekday, past range",
		schedule: "mon3-sun",
		last:     "2020-01-26 00:00:00",
		now:      "2020-01-26 08:45:00",
		next:     "2020-02-17 00:00:00",
	}, {
		comment:  "reverse range positioning",
		schedule: "mon-sun4",
		last:     "2020-01-01 00:00:00",
		now:      "2020-01-09 08:45:00",
		next:     "2020-01-20 00:00:00",
	}, {
		comment:  "reverse range positioning, crossing the year",
		schedule: "mon-fri1",
		last:     "2019-12-27 00:00:00",
		now:      "2019-12-27 08:45:00",
		next:     "2019-12-30 00:00:00",
	}, {
		comment:  "reverse range positioning, skip to next span across month",
		schedule: "mon-fri1",
		last:     "2020-01-03 00:00:00",
		now:      "2020-01-03 08:45:00",
		next:     "2020-02-03 00:00:00",
	}, {
		comment:  "last weekday of the month",
		schedule: "fri5",
		last:     "2020-01-01 00:00:00",
		now:      "2020-02-01 08:45:00",
		next:     "2020-02-28 00:00:00",
	}, {
		comment:  "last weekday of the month, in a month with five of them",
		schedule: "fri5",
		last:     "2020-01-01 00:00:00",
		now:      "2020-01-01 08:45:00",
		next:     "2020-01-31 00:00:00",
	},

	// === Clocks ===
	{
		comment:  "simple single clock case moving forward on same day",
		schedule: "10:00",
		last:     "2020-01-10 00:00:00",
		now:      "2020-01-10 08:45:00",
		next:     "2020-01-10 10:00:00",
	}, {
		comment:  "simple single clock case moving forward to next day",
		schedule: "8:00",
		last:     "2020-01-10 08:10:00",
		now:      "2020-01-10 08:45:00",
		next:     "2020-01-11 08:00:00",
	}, {
		comment:  "two clocks, first one comes first",
		schedule: "9:00,10:00",
		last:     "2020-01-10 00:00:00",
		now:      "2020-01-10 08:45:00",
		next:     "2020-01-10 09:00:00",
	}, {
		comment:  "two clocks, second one comes first",
		schedule: "10:00,9:00",
		last:     "2020-01-10 00:00:00",
		now:      "2020-01-10 08:45:00",
		next:     "2020-01-10 09:00:00",
	}, {
		comment:  "current time inside clock span",
		schedule: "8:00-9:00",
		last:     "2020-01-10 00:00:00",
		now:      "2020-01-10 08:45:00",
		next:     "2020-01-10 08:00:00",
	}, {
		comment:  "current time inside clock span, last also inside it, so skip span",
		schedule: "8:00-9:00",
		last:     "2020-01-10 08:15:00",
		now:      "2020-01-10 08:45:00",
		next:     "2020-01-11 08:00:00",
	}, {
		comment:  "current time inside clock span, split in three so it isn't skipped",
		schedule: "8:00-9:00/3",
		last:     "2020-01-10 08:15:00",
		now:      "2020-01-10 08:45:00",
		next:     "2020-01-10 08:20:00",
	}, {
		comment:  "last at the start of a split part",
		schedule: "8:00-9:00/3",
		last:     "2020-01-10 08:20:00",
		now:      "2020-01-10 08:20:00",
		next:     "2020-01-10 08:40:00",
	}, {
		comment:  "last inside the last split part",
		schedule: "8:00-9:00/3",
		last:     "2020-01-10 08:40:00",
		now:      "2020-01-10 08:40:00",
		next:     "2020-01-11 08:00:00",
	}, {
		comment:  "split cross-day, event on first day",
		schedule: "23:30-0:30/3",
		last:     "2020-01-10 23:45:00",
		now:      "2020-01-11 00:25:00",
		next:     "2020-01-10 23:50:00",
	}, {
		comment:  "split cross-day, event on second day",
		schedule: "23:30-0:30/3",
		last:     "2020-01-10 23:55:00",
		now:      "2020-01-11 00:25:00",
		next:     "2020-01-11 00:10:00",
	}, {
		comment:  "split unit with hours",
		schedule: "8:00-10:00/1h",
		last:     "2020-01-10 08:15:00",
		now:      "2020-01-10 08:45:00",
		next:     "2020-01-10 09:00:00",
	}, {
		comment:  "split unit with hours, end of span isn't a trigger",
		schedule: "8:00-10:00/1h",
		last:     "2020-01-10 09:00:00",
		now:      "2020-01-10 09:00:00",
		next:     "2020-01-11 08:00:00",
	}, {
		comment:  "split unit with minutes",
		schedule: "8:00-10:00/30m",
		last:     "2020-01-10 08:15:00",
		now:      "2020-01-10 08:45:00",
		next:     "2020-01-10 08:30:00",
	}, {
		comment:  "split unit with minutes, last further in",
		schedule: "8:00-10:00/25m",
		last:     "2020-01-10 08:55:00",
		now:      "2020-01-10 08:55:00",
		next:     "2020-01-10 09:15:00",
	}, {
		comment:  "split unit with seconds",
		schedule: "8:00-10:00/30s",
		last:     "2020-01-10 08:15:00",
		now:      "2020-01-10 08:45:00",
		next:     "2020-01-10 08:15:30",
	}, {
		comment:  "split unit with seconds, last has seconds too",
		schedule: "8:00-10:00/25s",
		last:     "2020-01-10 08:15:55",
		now:      "2020-01-10 08:45:00",
		next:     "2020-01-10 08:16:15",
	}, {
		comment:  "second split across the year",
		schedule: "23:59-1:00/25s",
		last:     "2019-12-31 23:59:55",
		now:      "2019-12-31 23:59:57",
		next:     "2020-01-01 00:00:15",
	}, {
		comment:  "whole day span",
		schedule: "0:00-0:00",
		last:     "2020-01-10 00:00:00",
		now:      "2020-01-10 08:45:00",
		next:     "2020-01-11 00:00:00",
	}, {
		comment:  "whole day span split hourly",
		schedule: "0:00-24:00/24",
		last:     "2020-01-10 08:00:00",
		now:      "2020-01-10 08:45:00",
		next:     "2020-01-10 09:00:00",
	}, {
		comment:  "whole day span split every minute, wrapping into next day",
		schedule: "0:00-24:00/1m",
		last:     "2020-01-10 23:59:00",
		now:      "2020-01-10 23:59:30",
		next:     "2020-01-11 00:00:00",
	},

	// === Clocks and weeks ===
	{
		comment:  "simple single-day next week matching with clock",
		schedule: "mon,8:00",
		last:     "2020-01-01 00:00:00",
		now:      "2020-01-09 08:20:00",
		next:     "2020-01-13 08:00:00",
	}, {
		comment:  "same day, but past the implied span end",
		schedule: "thu,8:00",
		last:     "2020-01-01 00:00:00",
		now:      "2020-01-09 08:20:00",
		next:     "2020-01-16 08:00:00",
	}, {
		comment:  "same day, inside the implied span still",
		schedule: "thu,8:00",
		last:     "2020-01-01 00:00:00",
		now:      "2020-01-09 08:10:00",
		next:     "2020-01-09 08:00:00",
	}, {
		comment:  "same day, past implied cross-day end",
		schedule: "thu,23:55",
		last:     "2020-01-01 00:00:00",
		now:      "2020-01-10 00:15:00",
		next:     "2020-01-16 23:55:00",
	}, {
		comment:  "same day, inside implied cross-day end",
		schedule: "thu,23:55",
		last:     "2020-01-01 00:00:00",
		now:      "2020-01-10 00:05:00",
		next:     "2020-01-09 23:55:00",
	}, {
		comment:  "same day, inside implied cross-day end, but event already happened on prior day",
		schedule: "thu,23:55",
		last:     "2020-01-09 23:58:00",
		now:      "2020-01-10 00:05:00",
		next:     "2020-01-16 23:55:00",
	}, {
		comment:  "same day, inside explicit span",
		schedule: "thu,8:00-9:00",
		last:     "2020-01-01 00:00:00",
		now:      "2020-01-09 08:45:00",
		next:     "2020-01-09 08:00:00",
	}, {
		comment:  "now is past the specified day, but span crosses midnight so next is on the prior day",
		schedule: "wed,23:00-1:00",
		last:     "2020-01-08 08:30:00",
		now:      "2020-01-09 00:45:00",
		next:     "2020-01-08 23:00:00",
	}, {
		comment:  "similar to prior case, but the previous day was the end of a span",
		schedule: "tue-wed,23:00-1:00",
		last:     "2020-01-08 08:30:00",
		now:      "2020-01-09 00:45:00",
		next:     "2020-01-08 23:00:00",
	}, {
		comment:  "across the year to first monday",
		schedule: "mon1,8:00",
		last:     "2019-12-27 00:00:00",
		now:      "2019-12-27 08:45:00",
		next:     "2020-01-06 08:00:00",
	}, {
		comment:  "multi-week cross-year span, now inside week span",
		schedule: "fri3-mon1,8:00",
		last:     "2019-12-27 00:00:00",
		now:      "2019-12-27 08:45:00",
		next:     "2019-12-28 08:00:00",
	}, {
		comment:  "multi-week cross-year span, past end of week span",
		schedule: "fri3-mon1,8:00",
		last:     "2020-01-06 08:00:00",
		now:      "2020-01-06 08:45:00",
		next:     "2020-01-17 08:00:00",
	}, {
		comment:  "multi-week span, now at edge of week span",
		schedule: "mon2-fri5,8:00",
		last:     "2019-12-27 00:00:00",
		now:      "2019-12-27 08:45:00",
		next:     "2020-01-13 08:00:00",
	},

	// === Months ===
	{
		comment:  "whole current month, today",
		schedule: "jan",
		last:     "2020-01-01 00:00:00",
		now:      "2020-01-09 08:45:00",
		next:     "2020-01-09 00:00:00",
	}, {
		comment:  "whole other month, pick first day",
		schedule: "feb",
		last:     "2020-01-01 00:00:00",
		now:      "2020-01-09 08:45:00",
		next:     "2020-02-01 00:00:00",
	}, {
		comment:  "current month, skip to right day",
		schedule: "jan,13",
		last:     "2020-01-01 00:00:00",
		now:      "2020-01-09 08:45:00",
		next:     "2020-01-13 00:00:00",
	}, {
		comment:  "two days, last on first",
		schedule: "jan,13,15",
		last:     "2020-01-13 00:00:00",
		now:      "2020-01-13 08:45:00",
		next:     "2020-01-15 00:00:00",
	}, {
		comment:  "different month, shift month and day",
		schedule: "mar,17",
		last:     "2020-01-01 00:00:00",
		now:      "2020-01-09 08:45:00",
		next:     "2020-03-17 00:00:00",
	}, {
		comment:  "friday inside specific range",
		schedule: "feb,11-15,fri",
		last:     "2020-01-01 00:00:00",
		now:      "2020-01-09 08:45:00",
		next:     "2020-02-14 00:00:00",
	}, {
		comment:  "friday on the 12th needs to skip year",
		schedule: "feb,11-13,fri",
		last:     "2020-01-01 00:00:00",
		now:      "2020-01-09 08:45:00",
		next:     "2021-02-12 00:00:00",
	}, {
		comment:  "weekday position inside range",
		schedule: "feb,4-29,mar,1-15,mon1",
		last:     "2020-01-01 00:00:00",
		now:      "2020-01-09 08:45:00",
		next:     "2020-03-02 00:00:00",
	}, {
		comment:  "weekday position inside range, last inside first span",
		schedule: "mar,1-15,mon1,wed1,9:00-10:00/2",
		last:     "2020-03-02 09:00:00",
		now:      "2020-03-02 09:20:00",
		next:     "2020-03-02 09:30:00",
	}, {
		comment:  "weekday position inside range, last in second day skips year",
		schedule: "mar,1-15,mon1,wed1,9:00-10:00",
		last:     "2020-03-04 09:00:00",
		now:      "2020-03-04 09:20:00",
		next:     "2021-03-01 09:00:00",
	}, {
		comment:  "across the year, with match in prior year",
		schedule: "dec,31,23:55",
		last:     "2019-12-31 00:00:00",
		now:      "2020-01-01 00:05:00",
		next:     "2019-12-31 23:55:00",
	}, {
		comment:  "across the year, with range",
		schedule: "jan-dec,1-31,23:55",
		last:     "2019-12-31 00:00:00",
		now:      "2020-01-01 00:05:00",
		next:     "2019-12-31 23:55:00",
	}, {
		comment:  "across the year, with weekday",
		schedule: "dec,tue5,23:55",
		last:     "2019-12-31 00:00:00",
		now:      "2020-01-01 00:05:00",
		next:     "2019-12-31 23:55:00",
	}, {
		comment:  "inverted week span, skipping across month",
		schedule: "jan,feb,5,mon-fri1,8:00",
		last:     "2020-01-03 00:00:00",
		now:      "2020-01-03 08:45:00",
		next:     "2020-02-05 08:00:00",
	}, {
		comment:  "month range wrapping around the year",
		schedule: "nov-feb,1",
		last:     "2020-03-01 00:00:00",
		now:      "2020-03-01 08:45:00",
		next:     "2020-11-01 00:00:00",
	}, {
		comment:  "day range wrapping around the month",
		schedule: "jan,30-2",
		last:     "2020-01-02 00:00:00",
		now:      "2020-01-02 08:45:00",
		next:     "2020-01-30 00:00:00",
	}, {
		comment:  "day range wrapping around the month, from the end",
		schedule: "jan,30-2",
		last:     "2020-01-31 00:00:00",
		now:      "2020-01-31 08:45:00",
		next:     "2021-01-01 00:00:00",
	}, {
		comment:  "leap day",
		schedule: "feb,29",
		last:     "2020-03-01 00:00:00",
		now:      "2020-03-01 08:45:00",
		next:     "2024-02-29 00:00:00",
	}, {
		comment:  "rare leap day on a monday",
		schedule: "feb,29,mon",
		last:     "2020-03-01 00:00:00",
		now:      "2020-03-01 08:45:00",
		next:     "2044-02-29 00:00:00",
	}, {
		comment:  "day that doesn't exist in the month",
		schedule: "feb,30",
		last:     "2020-01-01 00:00:00",
		now:      "2020-01-01 08:45:00",
	}, {
		comment:  "day that doesn't exist in one of the months",
		schedule: "feb-mar,30",
		last:     "2020-01-01 00:00:00",
		now:      "2020-01-01 08:45:00",
		next:     "2020-03-30 00:00:00",
	},

	// === Sets ===
	{
		comment:  "multiple sets, earliest wins",
		schedule: "mon-fri,8:00-17:00,,sat,sun,10:00,,mon1,9:00",
		last:     "2020-01-03 17:00:00",
		now:      "2020-01-03 18:00:00",
		next:     "2020-01-04 10:00:00",
	}, {
		comment:  "multiple sets, earliest wins on the same day",
		schedule: "mon-fri,8:00-17:00,,sat,sun,10:00,,mon1,7:00",
		last:     "2020-01-05 10:00:00",
		now:      "2020-01-05 18:00:00",
		next:     "2020-01-06 07:00:00",
	}, {
		comment:  "readme example of a monthly range on saturdays",
		schedule: "dec-mar,1-14,sat",
		last:     "2020-03-14 00:00:00",
		now:      "2020-03-14 08:00:00",
		next:     "2020-12-05 00:00:00",
	}, {
		comment:  "readme example of a split span",
		schedule: "mon,8:00-10:00/30s",
		last:     "2020-01-06 09:59:30",
		now:      "2020-01-06 09:59:31",
		next:     "2020-01-13 08:00:00",
	},
}

func (s *scheduleSuite) TestNext(c *C) {
	for _, test := range nextTests {
		comment := Commentf("%s: schedule %q", test.comment, test.schedule)
		sched, err := schedule.Parse(test.schedule)
		c.Assert(err, IsNil, comment)

		last, err := time.ParseInLocation(timeLayout, test.last, time.UTC)
		c.Assert(err, IsNil)
		now, err := time.ParseInLocation(timeLayout, test.now, time.UTC)
		c.Assert(err, IsNil)

		next, ok := sched.Next(last, now)
		if test.next == "" {
			c.Check(ok, Equals, false, comment)
			continue
		}
		c.Assert(ok, Equals, true, comment)
		c.Check(next.Format(timeLayout), Equals, test.next, comment)
		c.Check(next.Location(), Equals, time.UTC, comment)
	}
}

func (s *scheduleSuite) TestNextUsesNowLocation(c *C) {
	loc := time.FixedZone("UTC+10", 10*60*60)
	sched, err := schedule.Parse("9:00")
	c.Assert(err, IsNil)

	// 2020-01-10 08:00 in UTC+10.
	now := time.Date(2020, 1, 9, 22, 0, 0, 0, time.UTC).In(loc)
	next, ok := sched.Next(now, now)
	c.Assert(ok, Equals, true)
	c.Check(next.Equal(time.Date(2020, 1, 10, 9, 0, 0, 0, loc)), Equals, true)
	c.Check(next.Location(), Equals, loc)
}

func (s *scheduleSuite) TestNextAcrossDST(c *C) {
	loc, err := time.LoadLocation("Europe/London")
	c.Assert(err, IsNil)
	sched, err := schedule.Parse("sun,9:00")
	c.Assert(err, IsNil)

	// Clocks go forward on 2020-03-29, at 1:00 GMT.
	now := time.Date(2020, 3, 28, 12, 0, 0, 0, loc)
	next, ok := sched.Next(now, now)
	c.Assert(ok, Equals, true)
	c.Check(next.Equal(time.Date(2020, 3, 29, 9, 0, 0, 0, loc)), Equals, true)
	c.Check(next.Format("15:04 MST"), Equals, "09:00 BST")
}

func (s *scheduleSuite) TestNextSequence(c *C) {
	sched, err := schedule.Parse("mon,fri,10:00,15:00")
	c.Assert(err, IsNil)

	// Starting on a Sunday, and pretending every event fires exactly on time.
	last := time.Date(2020, 1, 5, 0, 0, 0, 0, time.UTC)
	var events []string
	for range 5 {
		next, ok := sched.Next(last, last)
		c.Assert(ok, Equals, true)
		events = append(events, next.Format("Mon 15:04"))
		last = next
	}
	c.Check(events, DeepEquals, []string{
		"Mon 10:00",
		"Mon 15:00",
		"Fri 10:00",
		"Fri 15:00",
		"Mon 10:00",
	})
}

func (s *scheduleSuite) TestNextSplitSequence(c *C) {
	sched, err := schedule.Parse("8:00-10:00/3")
	c.Assert(err, IsNil)

	last := time.Date(2020, 1, 10, 0, 0, 0, 0, time.UTC)
	var events []string
	for range 4 {
		next, ok := sched.Next(last, last)
		c.Assert(ok, Equals, true)
		events = append(events, next.Format("2006-01-02 15:04"))
		last = next
	}
	c.Check(events, DeepEquals, []string{
		"2020-01-10 08:00",
		"2020-01-10 08:40",
		"2020-01-10 09:20",
		"2020-01-11 08:00",
	})
}
