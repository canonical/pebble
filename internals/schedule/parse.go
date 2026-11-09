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
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	// maxSets is the maximum number of sets in a schedule.
	maxSets = 8

	// maxWeekSpans is the maximum number of week spans in a set.
	maxWeekSpans = 8

	// maxClockSpans is the maximum number of clock spans in a set.
	maxClockSpans = 8

	// impliedClockSpan is the length of the span implied by a single clock
	// time, such as "9:00".
	impliedClockSpan = 15 * time.Minute

	// maxSplit is the maximum value of N in a "/N" clock span split.
	maxSplit = 99999
)

var weekdayNames = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}
var monthNames = []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}

const allDays = uint32(1<<32-1) &^ 1

// Parse parses a schedule string. Whitespace is not allowed anywhere in it.
func Parse(text string) (*Schedule, error) {
	if text == "" {
		return nil, fmt.Errorf("cannot parse empty schedule")
	}
	sched := &Schedule{text: text}
	for setText := range strings.SplitSeq(text, ",,") {
		if len(sched.sets) == maxSets {
			return nil, fmt.Errorf("cannot parse schedule %q due to more than %d sets", text, maxSets)
		}
		st, err := parseSet(setText)
		if err != nil {
			return nil, fmt.Errorf("cannot parse schedule %q due to %w", text, err)
		}
		sched.sets = append(sched.sets, st)
	}
	return sched, nil
}

// parseSet parses one set of a schedule: a comma-separated list of months,
// days, weekdays and clock spans. Days must follow a month, and apply to the
// months before them. Other elements may appear in any order. Anything the
// set leaves out defaults to everything: all days of the year, any weekday,
// and the whole day.
func parseSet(text string) (set, error) {
	var st set
	if text == "" {
		return st, fmt.Errorf("empty set")
	}

	// Months accumulate into a group until days follow them, at which point
	// the days apply to every month in the group. A month after days starts
	// a new group. A group with no days selects all days of its months.
	var months uint16 // bit N set for month N (0 is January)
	var days uint32
	haveMonths := false
	flush := func() {
		if months == 0 {
			return
		}
		if days == 0 {
			days = allDays
		}
		for m := range 12 {
			if months&(1<<m) != 0 {
				st.days[m] |= days
			}
		}
		months = 0
		days = 0
	}

	for elem := range strings.SplitSeq(text, ",") {
		switch {
		case elem == "":
			return st, fmt.Errorf("empty element")
		case isClockElem(elem):
			if len(st.clockSpans) == maxClockSpans {
				return st, fmt.Errorf("more than %d clock spans in a set", maxClockSpans)
			}
			cspan, err := parseClockSpan(elem)
			if err != nil {
				return st, err
			}
			st.clockSpans = append(st.clockSpans, cspan)
		case isDigit(elem[0]):
			if months == 0 {
				return st, fmt.Errorf("days %q before a month", elem)
			}
			d, err := parseDays(elem)
			if err != nil {
				return st, err
			}
			days |= d
		default:
			if _, isMonth := lookupName(monthNames, elem[:min(len(elem), 3)]); isMonth {
				m, err := parseMonths(elem)
				if err != nil {
					return st, err
				}
				if days != 0 {
					flush()
				}
				months |= m
				haveMonths = true
				continue
			}
			if len(st.weekSpans) == maxWeekSpans {
				return st, fmt.Errorf("more than %d week spans in a set", maxWeekSpans)
			}
			wspan, err := parseWeekSpan(elem)
			if err != nil {
				return st, err
			}
			st.weekSpans = append(st.weekSpans, wspan)
		}
	}
	flush()

	if !haveMonths {
		for m := range st.days {
			st.days[m] = allDays
		}
	}
	if len(st.weekSpans) == 0 {
		st.weekSpans = []weekSpan{{
			start: weekdayPos{weekday: time.Sunday},
			end:   weekdayPos{weekday: time.Saturday},
		}}
	}
	if len(st.clockSpans) == 0 {
		st.clockSpans = []clockSpan{{period: 24 * time.Hour}}
	}
	return st, nil
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func isClockElem(elem string) bool {
	return strings.Contains(elem, ":")
}

func lookupName(names []string, s string) (int, bool) {
	for i, name := range names {
		if s == name {
			return i, true
		}
	}
	return 0, false
}

// parseMonths parses a month ("jan") or month range ("dec-mar"), returning
// a bitmask with bit N set for month N (0 is January).
func parseMonths(elem string) (uint16, error) {
	startText, endText, isRange := strings.Cut(elem, "-")
	start, ok := lookupName(monthNames, startText)
	if !ok {
		return 0, fmt.Errorf("invalid month %q", startText)
	}
	end := start
	if isRange {
		end, ok = lookupName(monthNames, endText)
		if !ok {
			return 0, fmt.Errorf("invalid month %q", endText)
		}
	}
	var months uint16
	for m := start; ; m = (m + 1) % 12 {
		months |= 1 << m
		if m == end {
			break
		}
	}
	return months, nil
}

// parseDays parses a day ("5") or day range ("5-10"), returning a bitmask
// with bit N set for day N. Ranges wrap around the month, so "25-5" is
// days 25 to 31 and 1 to 5.
func parseDays(elem string) (uint32, error) {
	startText, endText, isRange := strings.Cut(elem, "-")
	start, err := parseDay(startText)
	if err != nil {
		return 0, err
	}
	end := start
	if isRange {
		end, err = parseDay(endText)
		if err != nil {
			return 0, err
		}
	}
	var days uint32
	for d := start; ; d = d%31 + 1 {
		days |= 1 << d
		if d == end {
			break
		}
	}
	return days, nil
}

// parseDay parses a day of the month, from "1" to "31", without leading zeros.
func parseDay(s string) (int, error) {
	if len(s) < 1 || len(s) > 2 || s[0] == '0' || !isDigit(s[0]) || (len(s) == 2 && !isDigit(s[1])) {
		return 0, fmt.Errorf("invalid day %q", s)
	}
	d, _ := strconv.Atoi(s)
	if d > 31 {
		return 0, fmt.Errorf("invalid day %q", s)
	}
	return d, nil
}

// parseWeekSpan parses a weekday ("mon", "mon3") or weekday range
// ("mon-fri", "mon3-sun", "mon-sun4").
func parseWeekSpan(elem string) (weekSpan, error) {
	startText, endText, isRange := strings.Cut(elem, "-")
	start, err := parseWeekday(startText)
	if err != nil {
		return weekSpan{}, err
	}
	end := start
	if isRange {
		end, err = parseWeekday(endText)
		if err != nil {
			return weekSpan{}, err
		}
	}
	return weekSpan{start: start, end: end}, nil
}

// parseWeekday parses a weekday name ("mon"), optionally followed by its
// position within the month, from 1 to 4, or 5 for the last ("mon3").
func parseWeekday(s string) (weekdayPos, error) {
	name := s
	pos := nthdayAny
	if len(s) == 4 && s[3] >= '1' && s[3] <= '5' {
		name = s[:3]
		pos = nthday(s[3] - '0')
	}
	wd, ok := lookupName(weekdayNames, name)
	if !ok {
		return weekdayPos{}, fmt.Errorf("unknown month, weekday, day or time %q", s)
	}
	return weekdayPos{weekday: time.Weekday(wd), pos: pos}, nil
}

// parseClockSpan parses a single time ("9:00"), which implies a 15 minute
// span, or a clock range ("8:00-17:00"), optionally split into parts
// ("8:00-10:00/3") or periods ("8:00-10:00/30s").
func parseClockSpan(elem string) (clockSpan, error) {
	startText, rest, isRange := strings.Cut(elem, "-")
	start, err := parseClock(startText)
	if err != nil {
		return clockSpan{}, err
	}
	if !isRange {
		return clockSpan{
			start:  start,
			end:    clockAfter(start, impliedClockSpan),
			period: impliedClockSpan,
		}, nil
	}

	endText, splitText, isSplit := strings.Cut(rest, "/")
	end, err := parseClock(endText)
	if err != nil {
		return clockSpan{}, err
	}
	cspan := clockSpan{start: start, end: end}
	cspan.period = cspan.duration()
	if isSplit {
		period, err := parseSplit(splitText, cspan.duration())
		if err != nil {
			return clockSpan{}, err
		}
		cspan.period = period
	}
	return cspan, nil
}

func clockAfter(c clock, d time.Duration) clock {
	mins := (c.hour*60 + c.minute + int(d/time.Minute)) % (24 * 60)
	return clock{hour: mins / 60, minute: mins % 60}
}

// parseClock parses "H:MM" or "HH:MM". "24:00" is accepted as midnight.
func parseClock(s string) (clock, error) {
	hourText, minText, ok := strings.Cut(s, ":")
	if !ok || len(hourText) < 1 || len(hourText) > 2 || len(minText) != 2 ||
		!isDigit(hourText[0]) || (len(hourText) == 2 && !isDigit(hourText[1])) ||
		!isDigit(minText[0]) || !isDigit(minText[1]) {
		return clock{}, fmt.Errorf("invalid time %q", s)
	}
	hour, _ := strconv.Atoi(hourText)
	minute, _ := strconv.Atoi(minText)
	if hour == 24 && minute == 0 {
		hour = 0
	}
	if hour > 23 || minute > 59 {
		return clock{}, fmt.Errorf("invalid time %q", s)
	}
	return clock{hour: hour, minute: minute}, nil
}

// parseSplit parses the N or N plus unit after the "/" of a clock span. With
// no unit, N is the number of equal parts the span is divided into.
// With a unit (s, m or h), N is the period between triggers.
func parseSplit(s string, span time.Duration) (time.Duration, error) {
	numText := strings.TrimRight(s, "smh")
	unitText := s[len(numText):]
	if len(numText) < 1 || len(numText) > 5 || numText[0] == '0' || len(unitText) > 1 {
		return 0, fmt.Errorf("invalid clock span split %q", "/"+s)
	}
	for i := range len(numText) {
		if !isDigit(numText[i]) {
			return 0, fmt.Errorf("invalid clock span split %q", "/"+s)
		}
	}
	n, _ := strconv.Atoi(numText)
	if n > maxSplit {
		return 0, fmt.Errorf("invalid clock span split %q", "/"+s)
	}
	switch unitText {
	case "s":
		return time.Duration(n) * time.Second, nil
	case "m":
		return time.Duration(n) * time.Minute, nil
	case "h":
		return time.Duration(n) * time.Hour, nil
	}
	// Whole seconds, like the original implementation.
	period := (span / time.Duration(n)).Truncate(time.Second)
	return max(period, time.Second), nil
}
