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
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/testutil"
)

func Test(t *testing.T) {
	testutil.PrintGoroutineLeaks(t, TestingT)
}

type scheduleSuite struct{}

var _ = Suite(&scheduleSuite{})

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

func (s *scheduleSuite) TestString(c *C) {
	sched, err := Parse("mon-fri,8:00-17:00,,sat,sun,10:00")
	c.Assert(err, IsNil)
	c.Check(sched.String(), Equals, "mon-fri,8:00-17:00,,sat,sun,10:00")
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
		comment:  "split cross-day part competes with an earlier span on the second day",
		schedule: "23:50-2:00/1h,0:15",
		last:     "2020-01-10 23:50:00",
		now:      "2020-01-11 00:05:00",
		next:     "2020-01-11 00:15:00",
	}, {
		comment:  "split cross-day part competes with a later span on the second day",
		schedule: "23:55-2:00/2h,1:00",
		last:     "2020-01-10 23:55:00",
		now:      "2020-01-11 00:30:00",
		next:     "2020-01-11 01:00:00",
	}, {
		comment:  "split cross-day part competes with an earlier span before midnight",
		schedule: "23:50-2:00/1h,0:15",
		last:     "2020-01-10 23:50:00",
		now:      "2020-01-10 23:55:00",
		next:     "2020-01-11 00:15:00",
	}, {
		comment:  "split cross-day part on the span end isn't a trigger, so second day span wins",
		schedule: "23:00-1:00/2h,0:30",
		last:     "2020-01-10 23:00:00",
		now:      "2020-01-11 00:10:00",
		next:     "2020-01-11 00:30:00",
	}, {
		comment:  "split cross-day part kept when the second day doesn't match",
		schedule: "mon,23:50-2:00/1h",
		last:     "2020-01-13 23:50:00",
		now:      "2020-01-14 00:05:00",
		next:     "2020-01-14 00:50:00",
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
		comment:  "now exactly at the end of the span, so the span is over",
		schedule: "8:00-9:00",
		last:     "2020-01-10 00:00:00",
		now:      "2020-01-10 09:00:00",
		next:     "2020-01-11 08:00:00",
	}, {
		comment:  "now just before the end of the span",
		schedule: "8:00-9:00",
		last:     "2020-01-10 00:00:00",
		now:      "2020-01-10 08:59:59",
		next:     "2020-01-10 08:00:00",
	}, {
		comment:  "now and last exactly at the start of the span",
		schedule: "8:00-9:00",
		last:     "2020-01-10 08:00:00",
		now:      "2020-01-10 08:00:00",
		next:     "2020-01-11 08:00:00",
	}, {
		comment:  "now exactly at the end of a split span crossing midnight",
		schedule: "23:00-1:00/30m",
		last:     "2020-01-10 23:30:00",
		now:      "2020-01-11 01:00:00",
		next:     "2020-01-11 23:00:00",
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
		comment:  "day that doesn't exist in the month, so the search limit is hit",
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
		sched, err := Parse(test.schedule)
		c.Assert(err, IsNil, comment)

		last, err := time.ParseInLocation(timeLayout, test.last, time.UTC)
		c.Assert(err, IsNil)
		now, err := time.ParseInLocation(timeLayout, test.now, time.UTC)
		c.Assert(err, IsNil)

		next, err := sched.Next(last, now)
		if test.next == "" {
			c.Check(err, Equals, ErrNoNext, comment)
			continue
		}
		c.Assert(err, IsNil, comment)
		c.Check(next.Format(timeLayout), Equals, test.next, comment)
		c.Check(next.Location(), Equals, time.UTC, comment)
	}
}

func (s *scheduleSuite) TestNextUsesNowLocation(c *C) {
	loc := time.FixedZone("UTC+10", 10*60*60)
	sched, err := Parse("9:00")
	c.Assert(err, IsNil)

	// 2020-01-10 08:00 in UTC+10.
	now := time.Date(2020, 1, 9, 22, 0, 0, 0, time.UTC).In(loc)
	next, err := sched.Next(now, now)
	c.Assert(err, IsNil)
	c.Check(next.Equal(time.Date(2020, 1, 10, 9, 0, 0, 0, loc)), Equals, true)
	c.Check(next.Location(), Equals, loc)
}

func (s *scheduleSuite) TestNextAcrossDST(c *C) {
	loc, err := time.LoadLocation("Europe/London")
	c.Assert(err, IsNil)
	sched, err := Parse("sun,9:00")
	c.Assert(err, IsNil)

	// Clocks go forward on 2020-03-29, at 1:00 GMT.
	now := time.Date(2020, 3, 28, 12, 0, 0, 0, loc)
	next, err := sched.Next(now, now)
	c.Assert(err, IsNil)
	c.Check(next.Equal(time.Date(2020, 3, 29, 9, 0, 0, 0, loc)), Equals, true)
	c.Check(next.Format("15:04 MST"), Equals, "09:00 BST")
}

func (s *scheduleSuite) TestNextMixedLocations(c *C) {
	loc := time.FixedZone("UTC+10", 10*60*60)
	sched, err := Parse("9:00-10:00/30m")
	c.Assert(err, IsNil)

	// last is 09:15 in UTC+10, but given in UTC.
	last := time.Date(2020, 1, 9, 23, 15, 0, 0, time.UTC)
	now := last.In(loc)
	next, err := sched.Next(last, now)
	c.Assert(err, IsNil)
	c.Check(next.Equal(time.Date(2020, 1, 10, 9, 30, 0, 0, loc)), Equals, true)
	c.Check(next.Location(), Equals, loc)

	// now's location is used, whatever last's location.
	next, err = sched.Next(last.In(loc), last)
	c.Assert(err, IsNil)
	c.Check(next.Equal(time.Date(2020, 1, 10, 9, 0, 0, 0, time.UTC)), Equals, true)
	c.Check(next.Location(), Equals, time.UTC)
}

func (s *scheduleSuite) TestNextAcrossDSTSequences(c *C) {
	loc, err := time.LoadLocation("Europe/London")
	c.Assert(err, IsNil)

	for _, test := range []struct {
		comment  string
		schedule string
		start    time.Time
		events   []string
	}{{
		// Clocks go forward on 2020-03-29, from 1:00 GMT to 2:00 BST.
		comment:  "clocks go forward inside the span",
		schedule: "0:30-3:00/30m",
		start:    time.Date(2020, 3, 29, 0, 0, 0, 0, loc),
		events: []string{
			"03-29 00:30 GMT",
			"03-29 02:00 BST", // 01:00 and 01:30 GMT were skipped.
			"03-29 02:30 BST",
			"03-30 00:30 BST",
		},
	}, {
		comment:  "clocks go forward inside a span crossing midnight, ending in skipped time",
		schedule: "23:30-1:30/30m",
		start:    time.Date(2020, 3, 28, 12, 0, 0, 0, loc),
		events: []string{
			"03-28 23:30 GMT",
			"03-29 00:00 GMT",
			"03-29 00:30 GMT",
			"03-29 02:00 BST", // 01:00 GMT was skipped.
			"03-29 23:30 BST",
		},
	}, {
		comment:  "clocks go forward over a whole span",
		schedule: "1:15",
		start:    time.Date(2020, 3, 28, 12, 0, 0, 0, loc),
		events: []string{
			"03-29 02:00 BST", // 01:15 GMT was skipped.
			"03-30 01:15 BST",
		},
	}, {
		// Clocks go back on 2020-10-25, from 2:00 BST to 1:00 GMT.
		comment:  "clocks go back inside the span",
		schedule: "0:30-3:00/30m",
		start:    time.Date(2020, 10, 25, 0, 0, 0, 0, loc),
		events: []string{
			"10-25 00:30 BST",
			"10-25 01:00 BST",
			"10-25 01:30 BST",
			"10-25 02:00 GMT", // 01:00 and 01:30 GMT aren't repeated.
			"10-25 02:30 GMT",
			"10-26 00:30 GMT",
		},
	}, {
		comment:  "clocks go back inside a span crossing midnight",
		schedule: "23:30-1:30/30m",
		start:    time.Date(2020, 10, 24, 12, 0, 0, 0, loc),
		events: []string{
			"10-24 23:30 BST",
			"10-25 00:00 BST",
			"10-25 00:30 BST",
			"10-25 01:00 BST", // Span ends at 01:30 BST, so 01:00 GMT isn't repeated.
			"10-25 23:30 GMT",
		},
	}, {
		comment:  "clocks go back over a whole span",
		schedule: "1:15",
		start:    time.Date(2020, 10, 24, 12, 0, 0, 0, loc),
		events: []string{
			"10-25 01:15 BST",
			"10-26 01:15 GMT",
		},
	}} {
		sched, err := Parse(test.schedule)
		c.Assert(err, IsNil)

		// Pretend every event fires exactly on time.
		last := test.start
		var events []string
		for range test.events {
			next, err := sched.Next(last, last)
			c.Assert(err, IsNil)
			c.Assert(next.After(last), Equals, true, Commentf(test.comment))
			events = append(events, next.Format("01-02 15:04 MST"))
			last = next
		}
		c.Check(events, DeepEquals, test.events, Commentf(test.comment))
	}
}

func (s *scheduleSuite) TestNextAcrossDSTLateTrigger(c *C) {
	loc, err := time.LoadLocation("Europe/London")
	c.Assert(err, IsNil)
	sched, err := Parse("0:30-3:00/30m")
	c.Assert(err, IsNil)

	// The 01:30 BST trigger happened, and now the clocks have gone back to
	// 01:10 GMT: the wall-clock 01:30 isn't repeated, so 02:00 GMT is next.
	last := time.Date(2020, 10, 25, 0, 30, 0, 0, time.UTC) // 01:30 BST
	now := time.Date(2020, 10, 25, 1, 10, 0, 0, time.UTC)  // 01:10 GMT
	next, err := sched.Next(last.In(loc), now.In(loc))
	c.Assert(err, IsNil)
	c.Check(next.Format("15:04 MST"), Equals, "02:00 GMT")

	// The skipped 01:00 GMT trigger is due exactly when clocks go forward.
	last = time.Date(2020, 3, 29, 0, 30, 0, 0, loc)
	now = time.Date(2020, 3, 29, 1, 0, 0, 0, time.UTC) // 02:00 BST
	next, err = sched.Next(last, now.In(loc))
	c.Assert(err, IsNil)
	c.Check(next.Format("15:04 MST"), Equals, "02:00 BST")
}

func (s *scheduleSuite) TestNextInvalidSchedule(c *C) {
	now := time.Date(2020, 1, 10, 9, 15, 0, 0, time.UTC)
	for _, test := range []struct {
		set set
		err string
	}{{
		set: set{clockSpans: []clockSpan{newClockSpan(9, 0, 10, 0, 0)}},
		err: "cannot evaluate schedule due to invalid clock span period 0s",
	}, {
		set: set{clockSpans: []clockSpan{newClockSpan(9, 0, 10, 0, -time.Minute)}},
		err: "cannot evaluate schedule due to invalid clock span period -1m0s",
	}, {
		set: set{clockSpans: []clockSpan{newClockSpan(24, 0, 10, 0, time.Minute)}},
		err: "cannot evaluate schedule due to invalid clock 24:00",
	}, {
		set: set{clockSpans: []clockSpan{newClockSpan(9, 0, 10, 60, time.Minute)}},
		err: "cannot evaluate schedule due to invalid clock 10:60",
	}, {
		set: set{weekSpans: []weekSpan{newWeekSpan(7, 0, time.Monday, 0)}},
		err: "cannot evaluate schedule due to invalid weekday 7",
	}, {
		set: set{weekSpans: []weekSpan{newWeekSpan(time.Monday, 0, time.Monday, 6)}},
		err: "cannot evaluate schedule due to invalid weekday position 6",
	}} {
		sched := &Schedule{sets: []set{test.set}}
		_, err := sched.Next(now, now)
		c.Check(err, ErrorMatches, test.err)
	}
}

func (s *scheduleSuite) TestNextSequence(c *C) {
	sched, err := Parse("mon,fri,10:00,15:00")
	c.Assert(err, IsNil)

	// Starting on a Sunday, and pretending every event fires exactly on time.
	last := time.Date(2020, 1, 5, 0, 0, 0, 0, time.UTC)
	var events []string
	for range 5 {
		next, err := sched.Next(last, last)
		c.Assert(err, IsNil)
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
	sched, err := Parse("8:00-10:00/3")
	c.Assert(err, IsNil)

	last := time.Date(2020, 1, 10, 0, 0, 0, 0, time.UTC)
	var events []string
	for range 4 {
		next, err := sched.Next(last, last)
		c.Assert(err, IsNil)
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
