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

// Package schedule implements the schedule syntax described at
// https://github.com/niemeyer/schedule-syntax.
//
// A schedule is one or more sets separated by a double comma. Each set
// selects days of the year (months and days of the month), weekdays
// (optionally positioned within the month), and clock spans (optionally
// split into repeating triggers), for example:
//
//	mon-fri,8:00-17:00,,sat,sun,10:00,,mon1,9:00
//	dec-mar,1-14,sat
//	mon,8:00-10:00/30s
//
// A time matches a set when its day matches the set's days and one of its
// weekdays, and the time is inside one of its clock spans. A time matches the
// schedule when it matches any of its sets. A set that leaves out days,
// weekdays or clock spans matches all of them, so "mon" matches the whole of
// every Monday, and "9:00" matches every day.
//
// Whitespace is not allowed anywhere in a schedule, including around it,
// and is deliberately not trimmed or skipped: it is rejected by the grammar
// like any other unexpected character.
//
// Clock spans are evaluated in wall-clock time, in the location of the
// current time passed to [Schedule.Next]. See its documentation for how
// daylight saving time transitions are handled.
//
// # Clock spans
//
// A clock span triggers once at its start. A single time such as "9:00" is a
// span of 15 minutes, and a range such as "9:00-17:00" runs up to, but not
// including, its end. When the trigger is missed, for example because the
// system was down, it is still due until the span ends, so [Schedule.Next]
// can return a time before now.
//
// A span may be split with "/N" into N equal parts, each of which triggers at
// its start, or with "/Ns", "/Nm" or "/Nh" into periods of N seconds, minutes
// or hours. The end of the span is never itself a trigger.
//
//	9:00               every day at 9:00, or by 9:15 if missed
//	9:00-17:00         once a day, at 9:00 or as soon after as possible
//	23:00-1:00         once a night, in a span crossing midnight
//	0:00-24:00/24      every hour, on the hour
//	9:00-17:00/30m     every 30 minutes, from 9:00 to 16:30
//	8:00-10:00/3       at 8:00, 8:40 and 9:20
//	mon,9:00,17:00     on Mondays at 9:00 and at 17:00
//
// # Weekdays
//
// Weekdays are "sun" to "sat", and ranges of them such as "fri-mon" wrap
// around the week. A weekday may be followed by its position within the
// month, from 1 to 4, or 5 for the last such weekday in the month. A range
// with a positioned start begins on that day and runs to the following end
// weekday. A range with only a positioned end runs from the preceding start
// weekday up to that day.
//
//	mon-fri,8:00-17:00  once each working day
//	sat,sun             once each weekend day, at midnight
//	mon1,9:00           on the first Monday of each month
//	fri5,17:00          on the last Friday of each month
//	mon3-sun            from the third Monday of each month to the Sunday after
//	mon-fri1            from the Monday before the first Friday of each month
//	                    to that Friday
//
// # Months and days
//
// Months are "jan" to "dec", and ranges of them such as "nov-feb" wrap around
// the year. Days of the month are 1 to 31, and must follow a month: they
// apply to every month listed since the previous days. Ranges of days such as
// "25-5" wrap around the month. A month with no days following it selects all
// of its days.
//
//	jan,1               on New Year's Day
//	dec,24-26,jan,1     on 24, 25 and 26 December, and on 1 January
//	jan-mar,15          on the 15th of January, February and March
//	dec-feb,sat,sun     at weekends in the winter months
//	mar,1-7,mon         on the first Monday of March
//	feb,29              on leap days
//
// # Limitations
//
// The syntax has some limits that are enforced by [Parse]:
//
//   - A schedule has at most 8 sets, and each set has at most 8 weekdays or
//     weekday ranges and at most 8 clock spans.
//   - Month and weekday names are lowercase, three letters long, and can't be
//     abbreviated or spelled out.
//   - Clock times have a resolution of one minute, from "0:00" to "23:59".
//     "24:00" is accepted as a synonym for "0:00", so "0:00-24:00" is the
//     whole day.
//   - A split has no leading zeros and is at most 99999. A split into equal
//     parts is truncated to whole seconds, and is at least one second.
//   - Weekday positions only count from the start of the month, from 1 to 4,
//     with 5 always meaning the last such weekday, even in months that only
//     have four of them.
//   - A schedule can't refer to a specific year, or to a time zone.
//
// Some schedules parse but never or rarely trigger. A day that doesn't
// exist in any of its months, such as "feb,30", never matches. [Schedule.Next]
// only searches about 50 years ahead for the next trigger, which comfortably
// covers rare schedules such as "feb,29,mon", and returns an error if there's
// none in that time.
package schedule
