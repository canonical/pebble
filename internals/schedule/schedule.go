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
package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	// MaxSets is the maximum number of sets in a schedule.
	MaxSets = 8
	// MaxWeekSpans is the maximum number of week spans in a set.
	MaxWeekSpans = 8
	// MaxClockSpans is the maximum number of clock spans in a set.
	MaxClockSpans = 8

	// impliedClockSpan is the length of the span implied by a single clock
	// time, such as "9:00".
	impliedClockSpan = 15 * time.Minute

	// maxSplit is the maximum value of N in a "/N" clock span split.
	maxSplit = 99999

	// maxSearchDays bounds how far ahead Next searches for an occurrence.
	// Fifty years comfortably covers sparse schedules such as "feb,29,mon",
	// which only match every few decades.
	maxSearchDays = 50 * 366
)

// Pos is the position of a weekday within a month.
type Pos int

const (
	// PosAny matches any position within the month.
	PosAny Pos = 0
	// PosLast matches the last occurrence of the weekday in the month.
	// Positions 1 to 4 match the first to fourth occurrence.
	PosLast Pos = 5
)

// Clock is a time of day.
type Clock struct {
	Hour   int
	Minute int
}

func (c Clock) String() string {
	return fmt.Sprintf("%02d:%02d", c.Hour, c.Minute)
}

func (c Clock) duration() time.Duration {
	return time.Duration(c.Hour)*time.Hour + time.Duration(c.Minute)*time.Minute
}

// ClockSpan is a span of time within a day, from Start up to End. A span
// whose End is at or before its Start crosses midnight, so "0:00-0:00" is
// the whole day. The span triggers at Start, and then again every Period
// for as long as the trigger is still before End.
type ClockSpan struct {
	Start  Clock
	End    Clock
	Period time.Duration
}

// Duration returns the length of the clock span.
func (cs ClockSpan) Duration() time.Duration {
	d := cs.End.duration() - cs.Start.duration()
	if d <= 0 {
		d += 24 * time.Hour
	}
	return d
}

// WeekdayPos is a weekday, optionally restricted to a position within the
// month.
type WeekdayPos struct {
	Weekday time.Weekday
	Pos     Pos
}

// WeekSpan is a span of days, from the Start weekday through to the End
// weekday. Spans wrap around the week, so "fri-mon" includes Friday,
// Saturday, Sunday and Monday.
//
// When Start has a position, the span begins on that day and continues
// through the following End weekday (at End's position, if any). When only
// End has a position, the span ends on that day and begins on the nearest
// preceding Start weekday.
type WeekSpan struct {
	Start WeekdayPos
	End   WeekdayPos
}

// Set is one set of a schedule. A time matches a set when its day is
// selected by Days, matches one of WeekSpans, and is inside one of
// ClockSpans.
type Set struct {
	// Days holds one bitmask per month (January first), with bit N set
	// when day N of that month is selected (bit 0 is unused).
	Days       [12]uint32
	WeekSpans  []WeekSpan
	ClockSpans []ClockSpan
}

// Schedule is a parsed schedule string.
type Schedule struct {
	Sets []Set
	text string
}

func (s *Schedule) String() string {
	return s.text
}

var weekdayNames = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}
var monthNames = []string{"jan", "feb", "mar", "apr", "may", "jun", "jul", "aug", "sep", "oct", "nov", "dec"}

const allDays = uint32(1<<32-1) &^ 1

// Parse parses a schedule string.
func Parse(text string) (*Schedule, error) {
	if text == "" {
		return nil, fmt.Errorf("cannot parse schedule: schedule is empty")
	}
	sched := &Schedule{text: text}
	for setText := range strings.SplitSeq(text, ",,") {
		if len(sched.Sets) == MaxSets {
			return nil, fmt.Errorf("cannot parse schedule %q: too many sets (maximum is %d)", text, MaxSets)
		}
		set, err := parseSet(setText)
		if err != nil {
			return nil, fmt.Errorf("cannot parse schedule %q: %w", text, err)
		}
		sched.Sets = append(sched.Sets, set)
	}
	return sched, nil
}

func parseSet(text string) (Set, error) {
	var set Set
	if text == "" {
		return set, fmt.Errorf("empty set")
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
				set.Days[m] |= days
			}
		}
		months = 0
		days = 0
	}

	for elem := range strings.SplitSeq(text, ",") {
		switch {
		case elem == "":
			return set, fmt.Errorf("empty element")
		case isClockElem(elem):
			if len(set.ClockSpans) == MaxClockSpans {
				return set, fmt.Errorf("too many clock spans (maximum is %d)", MaxClockSpans)
			}
			cspan, err := parseClockSpan(elem)
			if err != nil {
				return set, err
			}
			set.ClockSpans = append(set.ClockSpans, cspan)
		case isDigit(elem[0]):
			if months == 0 {
				return set, fmt.Errorf("days %q must follow a month", elem)
			}
			d, err := parseDays(elem)
			if err != nil {
				return set, err
			}
			days |= d
		default:
			if _, isMonth := lookupName(monthNames, elem[:min(len(elem), 3)]); isMonth {
				m, err := parseMonths(elem)
				if err != nil {
					return set, err
				}
				if days != 0 {
					flush()
				}
				months |= m
				haveMonths = true
				continue
			}
			if len(set.WeekSpans) == MaxWeekSpans {
				return set, fmt.Errorf("too many week spans (maximum is %d)", MaxWeekSpans)
			}
			wspan, err := parseWeekSpan(elem)
			if err != nil {
				return set, err
			}
			set.WeekSpans = append(set.WeekSpans, wspan)
		}
	}
	flush()

	if !haveMonths {
		for m := range set.Days {
			set.Days[m] = allDays
		}
	}
	if len(set.WeekSpans) == 0 {
		set.WeekSpans = []WeekSpan{{
			Start: WeekdayPos{Weekday: time.Sunday},
			End:   WeekdayPos{Weekday: time.Saturday},
		}}
	}
	if len(set.ClockSpans) == 0 {
		set.ClockSpans = []ClockSpan{{Period: 24 * time.Hour}}
	}
	return set, nil
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
		return 0, fmt.Errorf("%q is not a valid month", startText)
	}
	end := start
	if isRange {
		end, ok = lookupName(monthNames, endText)
		if !ok {
			return 0, fmt.Errorf("%q is not a valid month", endText)
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

func parseDay(s string) (int, error) {
	if len(s) < 1 || len(s) > 2 || s[0] == '0' || !isDigit(s[0]) || (len(s) == 2 && !isDigit(s[1])) {
		return 0, fmt.Errorf("%q is not a valid day", s)
	}
	d, _ := strconv.Atoi(s)
	if d > 31 {
		return 0, fmt.Errorf("%q is not a valid day", s)
	}
	return d, nil
}

// parseWeekSpan parses a weekday ("mon", "mon3") or weekday range
// ("mon-fri", "mon3-sun", "mon-sun4").
func parseWeekSpan(elem string) (WeekSpan, error) {
	startText, endText, isRange := strings.Cut(elem, "-")
	start, err := parseWeekday(startText)
	if err != nil {
		return WeekSpan{}, err
	}
	end := start
	if isRange {
		end, err = parseWeekday(endText)
		if err != nil {
			return WeekSpan{}, err
		}
	}
	return WeekSpan{Start: start, End: end}, nil
}

func parseWeekday(s string) (WeekdayPos, error) {
	name := s
	pos := PosAny
	if len(s) == 4 && s[3] >= '1' && s[3] <= '5' {
		name = s[:3]
		pos = Pos(s[3] - '0')
	}
	wd, ok := lookupName(weekdayNames, name)
	if !ok {
		return WeekdayPos{}, fmt.Errorf("%q is not a valid month, weekday, day or time", s)
	}
	return WeekdayPos{Weekday: time.Weekday(wd), Pos: pos}, nil
}

// parseClockSpan parses a single time ("9:00"), which implies a 15 minute
// span, or a clock range ("8:00-17:00"), optionally split into parts
// ("8:00-10:00/3") or periods ("8:00-10:00/30s").
func parseClockSpan(elem string) (ClockSpan, error) {
	startText, rest, isRange := strings.Cut(elem, "-")
	start, err := parseClock(startText)
	if err != nil {
		return ClockSpan{}, err
	}
	if !isRange {
		return ClockSpan{
			Start:  start,
			End:    clockAfter(start, impliedClockSpan),
			Period: impliedClockSpan,
		}, nil
	}

	endText, splitText, isSplit := strings.Cut(rest, "/")
	end, err := parseClock(endText)
	if err != nil {
		return ClockSpan{}, err
	}
	cspan := ClockSpan{Start: start, End: end}
	cspan.Period = cspan.Duration()
	if isSplit {
		period, err := parseSplit(splitText, cspan.Duration())
		if err != nil {
			return ClockSpan{}, err
		}
		cspan.Period = period
	}
	return cspan, nil
}

func clockAfter(c Clock, d time.Duration) Clock {
	mins := (c.Hour*60 + c.Minute + int(d/time.Minute)) % (24 * 60)
	return Clock{Hour: mins / 60, Minute: mins % 60}
}

// parseClock parses "H:MM" or "HH:MM". "24:00" is accepted as midnight.
func parseClock(s string) (Clock, error) {
	hourText, minText, ok := strings.Cut(s, ":")
	if !ok || len(hourText) < 1 || len(hourText) > 2 || len(minText) != 2 ||
		!isDigit(hourText[0]) || (len(hourText) == 2 && !isDigit(hourText[1])) ||
		!isDigit(minText[0]) || !isDigit(minText[1]) {
		return Clock{}, fmt.Errorf("%q is not a valid time", s)
	}
	hour, _ := strconv.Atoi(hourText)
	minute, _ := strconv.Atoi(minText)
	if hour == 24 && minute == 0 {
		hour = 0
	}
	if hour > 23 || minute > 59 {
		return Clock{}, fmt.Errorf("%q is not a valid time", s)
	}
	return Clock{Hour: hour, Minute: minute}, nil
}

// parseSplit parses the N or N plus unit after the "/" of a clock span. With
// no unit, N is the number of equal parts the span is divided into.
// With a unit (s, m or h), N is the period between triggers.
func parseSplit(s string, span time.Duration) (time.Duration, error) {
	numText := strings.TrimRight(s, "smh")
	unitText := s[len(numText):]
	if len(numText) < 1 || len(numText) > 5 || numText[0] == '0' || len(unitText) > 1 {
		return 0, fmt.Errorf("%q is not a valid clock span split", "/"+s)
	}
	for i := range len(numText) {
		if !isDigit(numText[i]) {
			return 0, fmt.Errorf("%q is not a valid clock span split", "/"+s)
		}
	}
	n, _ := strconv.Atoi(numText)
	if n > maxSplit {
		return 0, fmt.Errorf("%q is not a valid clock span split", "/"+s)
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

// Next returns the time of the earliest trigger of the schedule that hasn't
// yet happened, given that the last event happened at last and the current
// time is now. The returned time is never before the start of the span that
// contains it, but may be before now when now is inside a clock span
// whose trigger hasn't happened since last. The search uses now's location.
//
// The boolean result is false if the schedule has no further triggers.
func (s *Schedule) Next(last, now time.Time) (time.Time, bool) {
	var next time.Time
	found := false
	for i := range s.Sets {
		t, ok := s.Sets[i].next(last, now)
		if ok && (!found || t.Before(next)) {
			next = t
			found = true
		}
	}
	return next, found
}

func (set *Set) next(last, now time.Time) (time.Time, bool) {
	loc := now.Location()

	// Start from the day before now, as it may hold a clock span crossing
	// midnight into today.
	y, m, d := now.Date()
	day := civilDate(y, m, d-1)

	for range maxSearchDays {
		if !set.matchesDay(day) {
			day = set.skipDay(day)
			continue
		}
		var next time.Time
		found := false
		for _, cspan := range set.ClockSpans {
			t, ok := cspan.next(day, last, now, loc)
			if ok && (!found || t.Before(next)) {
				next = t
				found = true
			}
		}
		if found {
			return next, true
		}
		day = day.AddDate(0, 0, 1)
	}
	return time.Time{}, false
}

// skipDay returns the day after day, skipping straight to the following
// month if no more days of day's month are selected.
func (set *Set) skipDay(day time.Time) time.Time {
	if set.Days[day.Month()-1]>>(day.Day()+1) == 0 {
		y, m, _ := day.Date()
		return civilDate(y, m+1, 1)
	}
	return day.AddDate(0, 0, 1)
}

// next returns the earliest trigger of the clock span starting on day that
// is after last and whose span hasn't ended by now.
func (cs ClockSpan) next(day, last, now time.Time, loc *time.Location) (time.Time, bool) {
	y, m, d := day.Date()
	start := time.Date(y, m, d, cs.Start.Hour, cs.Start.Minute, 0, 0, loc)
	endDay := d
	if cs.End.duration() <= cs.Start.duration() {
		endDay++
	}
	end := time.Date(y, m, endDay, cs.End.Hour, cs.End.Minute, 0, 0, loc)

	if !end.After(now) || !end.After(last) {
		// Span is entirely in the past.
		return time.Time{}, false
	}
	if start.After(last) {
		return start, true
	}
	// The last event happened inside this span, so find the following part.
	part := last.Sub(start)/cs.Period + 1
	offset := part * cs.Period
	if offset >= cs.Duration() {
		return time.Time{}, false
	}
	return start.Add(offset), true
}

// civilDate returns midnight UTC of the given date, normalised. Dates are
// kept in UTC for calendar arithmetic, so it isn't affected by DST.
func civilDate(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func (set *Set) matchesDay(day time.Time) bool {
	if set.Days[day.Month()-1]&(1<<day.Day()) == 0 {
		return false
	}
	for _, wspan := range set.WeekSpans {
		if wspan.matches(day) {
			return true
		}
	}
	return false
}

func (ws WeekSpan) matches(day time.Time) bool {
	switch {
	case ws.Start.Pos == PosAny && ws.End.Pos == PosAny:
		return weekdayDelta(ws.Start.Weekday, day.Weekday()) <= weekdayDelta(ws.Start.Weekday, ws.End.Weekday)
	case ws.Start.Pos != PosAny:
		// Anchored at the start: the span may have started in this month
		// or the one before.
		y, m, _ := day.Date()
		for _, month := range []time.Month{m, m - 1} {
			start := nthWeekday(y, month, ws.Start.Weekday, ws.Start.Pos)
			if start.After(day) {
				continue
			}
			end := ws.End.onOrAfter(start)
			if !day.After(end) {
				return true
			}
		}
	default:
		// Anchored at the end: the span may end in this month or the next.
		y, m, _ := day.Date()
		for _, month := range []time.Month{m, m + 1} {
			end := nthWeekday(y, month, ws.End.Weekday, ws.End.Pos)
			if end.Before(day) {
				continue
			}
			start := end.AddDate(0, 0, -weekdayDelta(ws.Start.Weekday, end.Weekday()))
			if !day.Before(start) {
				return true
			}
		}
	}
	return false
}

// onOrAfter returns the first day on or after t that matches the weekday
// and position.
func (wp WeekdayPos) onOrAfter(t time.Time) time.Time {
	if wp.Pos == PosAny {
		return t.AddDate(0, 0, weekdayDelta(t.Weekday(), wp.Weekday))
	}
	y, m, _ := t.Date()
	for {
		day := nthWeekday(y, m, wp.Weekday, wp.Pos)
		if !day.Before(t) {
			return day
		}
		m++
	}
}

// weekdayDelta returns the number of days from weekday a forward to b.
func weekdayDelta(a, b time.Weekday) int {
	return (int(b) - int(a) + 7) % 7
}

// nthWeekday returns the date of the pos'th weekday of the given month,
// or of the last one if pos is PosLast. The month is normalised.
func nthWeekday(y int, m time.Month, wd time.Weekday, pos Pos) time.Time {
	if pos == PosLast {
		last := civilDate(y, m+1, 0)
		return last.AddDate(0, 0, -weekdayDelta(wd, last.Weekday()))
	}
	first := civilDate(y, m, 1)
	return first.AddDate(0, 0, weekdayDelta(first.Weekday(), wd)+7*(int(pos)-1))
}
