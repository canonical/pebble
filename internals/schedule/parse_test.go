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

package schedule

import (
	"time"

	. "gopkg.in/check.v1"
)

type parseSuite struct{}

var _ = Suite(&parseSuite{})

var (
	anyWeek = weekSpan{
		start: weekdayPos{weekday: time.Sunday},
		end:   weekdayPos{weekday: time.Saturday},
	}
	wholeDay = clockSpan{period: 24 * time.Hour}
)

func newWeekSpan(start time.Weekday, startPos nthday, end time.Weekday, endPos nthday) weekSpan {
	return weekSpan{
		start: weekdayPos{weekday: start, pos: startPos},
		end:   weekdayPos{weekday: end, pos: endPos},
	}
}

func newClockSpan(startHour, startMinute, endHour, endMinute uint8, period time.Duration) clockSpan {
	return clockSpan{
		start:  clock{hour: startHour, minute: startMinute},
		end:    clock{hour: endHour, minute: endMinute},
		period: period,
	}
}

func allYear() [12]uint32 {
	var days [12]uint32
	for m := range days {
		days[m] = 0xFFFFFFFE
	}
	return days
}

func (s *parseSuite) TestParseWeekSpans(c *C) {
	sched, err := Parse("mon-fri,tue3,sat,,wed5-sun")
	c.Assert(err, IsNil)
	c.Assert(sched.sets, HasLen, 2)

	c.Check(sched.sets[0].weekSpans, DeepEquals, []weekSpan{
		newWeekSpan(time.Monday, 0, time.Friday, 0),
		newWeekSpan(time.Tuesday, 3, time.Tuesday, 3),
		newWeekSpan(time.Saturday, 0, time.Saturday, 0),
	})
	c.Check(sched.sets[0].clockSpans, DeepEquals, []clockSpan{wholeDay})
	c.Check(sched.sets[0].days, Equals, allYear())

	c.Check(sched.sets[1].weekSpans, DeepEquals, []weekSpan{
		newWeekSpan(time.Wednesday, nthdayLast, time.Sunday, 0),
	})
	c.Check(sched.sets[1].clockSpans, DeepEquals, []clockSpan{wholeDay})
	c.Check(sched.sets[1].days, Equals, allYear())
}

func (s *parseSuite) TestParseClockSpans(c *C) {
	sched, err := Parse("10:05-20:35/126,0:00-0:00,24:00,,12:34,1:00-2:00/30s,3:00-4:00/10m,5:00-7:00/1h,23:55")
	c.Assert(err, IsNil)
	c.Assert(sched.sets, HasLen, 2)

	c.Check(sched.sets[0].clockSpans, DeepEquals, []clockSpan{
		newClockSpan(10, 5, 20, 35, 300*time.Second),
		newClockSpan(0, 0, 0, 0, 24*time.Hour),
		newClockSpan(0, 0, 0, 15, 15*time.Minute),
	})
	c.Check(sched.sets[0].weekSpans, DeepEquals, []weekSpan{anyWeek})

	c.Check(sched.sets[1].clockSpans, DeepEquals, []clockSpan{
		newClockSpan(12, 34, 12, 49, 15*time.Minute),
		newClockSpan(1, 0, 2, 0, 30*time.Second),
		newClockSpan(3, 0, 4, 0, 10*time.Minute),
		newClockSpan(5, 0, 7, 0, time.Hour),
		newClockSpan(23, 55, 0, 10, 15*time.Minute),
	})
	c.Check(sched.sets[1].weekSpans, DeepEquals, []weekSpan{anyWeek})
}

func (s *parseSuite) TestParseClockSpanSplit(c *C) {
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
		sched, err := Parse(test.text)
		c.Assert(err, IsNil, Commentf("schedule %q", test.text))
		c.Check(sched.sets[0].clockSpans[0].period, Equals, test.period, Commentf("schedule %q", test.text))
	}
}

func (s *parseSuite) TestParseMonthSpans(c *C) {
	sched, err := Parse("dec-mar,may,5-10,jan,15-20,,jan,1-31,feb")
	c.Assert(err, IsNil)
	c.Assert(sched.sets, HasLen, 2)

	var expected [12]uint32
	expected[0] = 0x1F87E0 // 5-10,15-20
	expected[1] = 0x07E0   // 5-10
	expected[2] = 0x07E0
	expected[4] = 0x07E0
	expected[11] = 0x07E0
	c.Check(sched.sets[0].days, Equals, expected)

	expected = [12]uint32{}
	expected[0] = 0xFFFFFFFE // 1-31, explicit
	expected[1] = 0xFFFFFFFE // 1-31, implicit
	c.Check(sched.sets[1].days, Equals, expected)
}

func (s *parseSuite) TestParseDayRanges(c *C) {
	sched, err := Parse("mar,29-31,apr,1-2,,jun,25-5")
	c.Assert(err, IsNil)
	c.Assert(sched.sets, HasLen, 2)

	var expected [12]uint32
	expected[2] = 0xE0000000 // 29-31
	expected[3] = 0x06       // 1-2
	c.Check(sched.sets[0].days, Equals, expected)

	expected = [12]uint32{}
	expected[5] = 0xFE00003E // 25-31,1-5
	c.Check(sched.sets[1].days, Equals, expected)
}

func (s *parseSuite) TestParseMixedSets(c *C) {
	sched, err := Parse("jan,10-15,5,mon-wed2,fri,09:45,13:00-17:00/5,,feb,sat,12:34")
	c.Assert(err, IsNil)
	c.Assert(sched.sets, HasLen, 2)

	var expected [12]uint32
	expected[0] = 0xFC20 // 5,10-15
	c.Check(sched.sets[0].days, Equals, expected)
	c.Check(sched.sets[0].weekSpans, DeepEquals, []weekSpan{
		newWeekSpan(time.Monday, 0, time.Wednesday, 2),
		newWeekSpan(time.Friday, 0, time.Friday, 0),
	})
	c.Check(sched.sets[0].clockSpans, DeepEquals, []clockSpan{
		newClockSpan(9, 45, 10, 0, 15*time.Minute),
		newClockSpan(13, 0, 17, 0, 48*time.Minute),
	})

	expected = [12]uint32{}
	expected[1] = 0xFFFFFFFE
	c.Check(sched.sets[1].days, Equals, expected)
	c.Check(sched.sets[1].weekSpans, DeepEquals, []weekSpan{
		newWeekSpan(time.Saturday, 0, time.Saturday, 0),
	})
	c.Check(sched.sets[1].clockSpans, DeepEquals, []clockSpan{
		newClockSpan(12, 34, 12, 49, 15*time.Minute),
	})
}

func (s *parseSuite) TestParseAnyOrder(c *C) {
	ordered, err := Parse("jan,10-15,mon-wed2,09:45,13:00-17:00/5")
	c.Assert(err, IsNil)
	shuffled, err := Parse("09:45,mon-wed2,13:00-17:00/5,jan,10-15")
	c.Assert(err, IsNil)
	c.Check(shuffled.sets, DeepEquals, ordered.sets)
}

func (s *parseSuite) TestParseErrors(c *C) {
	for _, test := range []struct {
		text string
		err  string
	}{
		{"", `cannot parse empty schedule`},
		{",,", `cannot parse schedule ",," due to empty set`},
		{"mon,,", `cannot parse schedule "mon,," due to empty set`},
		{",,mon", `cannot parse schedule ",,mon" due to empty set`},
		{"mon,,,tue", `cannot parse schedule "mon,,,tue" due to empty element`},
		{"mon,", `cannot parse schedule "mon," due to empty element`},
		{",mon", `cannot parse schedule ",mon" due to empty element`},
		{"fry", `cannot parse schedule "fry" due to unknown month, weekday, day or time "fry"`},
		{"mon-fry", `cannot parse schedule "mon-fry" due to unknown month, weekday, day or time "fry"`},
		{"mon0", `cannot parse schedule "mon0" due to unknown month, weekday, day or time "mon0"`},
		{"mon6", `cannot parse schedule "mon6" due to unknown month, weekday, day or time "mon6"`},
		{"MON", `cannot parse schedule "MON" due to unknown month, weekday, day or time "MON"`},
		{"jan-foo", `cannot parse schedule "jan-foo" due to invalid month "foo"`},
		{"janx", `cannot parse schedule "janx" due to invalid month "janx"`},
		{"5", `cannot parse schedule "5" due to days "5" before a month`},
		{"mon,5", `cannot parse schedule "mon,5" due to days "5" before a month`},
		{"jan,0", `cannot parse schedule "jan,0" due to invalid day "0"`},
		{"jan,05", `cannot parse schedule "jan,05" due to invalid day "05"`},
		{"jan,32", `cannot parse schedule "jan,32" due to invalid day "32"`},
		{"jan,123", `cannot parse schedule "jan,123" due to invalid day "123"`},
		{"jan,1-32", `cannot parse schedule "jan,1-32" due to invalid day "32"`},
		{"jan,1-", `cannot parse schedule "jan,1-" due to invalid day ""`},
		{"9", `cannot parse schedule "9" due to days "9" before a month`},
		{"9:0", `cannot parse schedule "9:0" due to invalid time "9:0"`},
		{"9:000", `cannot parse schedule "9:000" due to invalid time "9:000"`},
		{"123:00", `cannot parse schedule "123:00" due to invalid time "123:00"`},
		{"25:00", `cannot parse schedule "25:00" due to invalid time "25:00"`},
		{"24:01", `cannot parse schedule "24:01" due to invalid time "24:01"`},
		{"9:60", `cannot parse schedule "9:60" due to invalid time "9:60"`},
		{"9:00-", `cannot parse schedule "9:00-" due to invalid time ""`},
		{"9:00-10", `cannot parse schedule "9:00-10" due to invalid time "10"`},
		{"9:00~10:00", `cannot parse schedule "9:00~10:00" due to invalid time "9:00~10:00"`},
		{"9:00/2", `cannot parse schedule "9:00/2" due to invalid time "9:00/2"`},
		// Whitespace is never allowed, not even around the schedule.
		{" ", `cannot parse schedule " " due to unknown month, weekday, day or time " "`},
		{" mon", `cannot parse schedule " mon" due to unknown month, weekday, day or time " mon"`},
		{"mon ", `cannot parse schedule "mon " due to unknown month, weekday, day or time "mon "`},
		{"mon, tue", `cannot parse schedule "mon, tue" due to unknown month, weekday, day or time " tue"`},
		{"jan, 5", `cannot parse schedule "jan, 5" due to unknown month, weekday, day or time " 5"`},
		{"9:00 -10:00", `cannot parse schedule "9:00 -10:00" due to invalid time "9:00 "`},
		{"9:00-10:00/5 m", `cannot parse schedule "9:00-10:00/5 m" due to invalid clock span split "/5 m"`},
		{"9:00-10:00/", `cannot parse schedule "9:00-10:00/" due to invalid clock span split "/"`},
		{"9:00-10:00/0", `cannot parse schedule "9:00-10:00/0" due to invalid clock span split "/0"`},
		{"9:00-10:00/05", `cannot parse schedule "9:00-10:00/05" due to invalid clock span split "/05"`},
		{"9:00-10:00/100000", `cannot parse schedule "9:00-10:00/100000" due to invalid clock span split "/100000"`},
		{"9:00-10:00/5d", `cannot parse schedule "9:00-10:00/5d" due to invalid clock span split "/5d"`},
		{"9:00-10:00/5ms", `cannot parse schedule "9:00-10:00/5ms" due to invalid clock span split "/5ms"`},
		{"9:00-10:00/0s", `cannot parse schedule "9:00-10:00/0s" due to invalid clock span split "/0s"`},
		{"9:00-10:00/0m", `cannot parse schedule "9:00-10:00/0m" due to invalid clock span split "/0m"`},
		{"9:00-10:00/0h", `cannot parse schedule "9:00-10:00/0h" due to invalid clock span split "/0h"`},
		{"9:00-10:00/m", `cannot parse schedule "9:00-10:00/m" due to invalid clock span split "/m"`},
		{
			"mon,,mon,,mon,,mon,,mon,,mon,,mon,,mon,,mon",
			`cannot parse schedule "mon,,mon,,mon,,mon,,mon,,mon,,mon,,mon,,mon" due to more than 8 sets`,
		}, {
			"mon,mon,mon,mon,mon,mon,mon,mon,mon",
			`cannot parse schedule "mon,mon,mon,mon,mon,mon,mon,mon,mon" due to more than 8 week spans in a set`,
		}, {
			"1:00,2:00,3:00,4:00,5:00,6:00,7:00,8:00,9:00",
			`cannot parse schedule "1:00,2:00,3:00,4:00,5:00,6:00,7:00,8:00,9:00" due to more than 8 clock spans in a set`,
		},
	} {
		_, err := Parse(test.text)
		c.Check(err, ErrorMatches, test.err, Commentf("schedule %q", test.text))
	}
}

func (s *parseSuite) TestParseLimits(c *C) {
	_, err := Parse("mon,,mon,,mon,,mon,,mon,,mon,,mon,,mon")
	c.Check(err, IsNil)
	_, err = Parse("mon,mon,mon,mon,mon,mon,mon,mon")
	c.Check(err, IsNil)
	_, err = Parse("1:00,2:00,3:00,4:00,5:00,6:00,7:00,8:00")
	c.Check(err, IsNil)
}
