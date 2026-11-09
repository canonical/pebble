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
	"errors"
	"fmt"
	"time"
)

const (
	// maxSearchDays bounds how far ahead Next searches for an occurrence.
	// Fifty years comfortably covers sparse schedules such as "feb,29,mon",
	// which only match every few decades.
	maxSearchDays = 50 * 366
)

// nthday is the position of a weekday within a month.
type nthday uint8

const (
	// nthdayAny matches any position within the month.
	nthdayAny nthday = 0
	// nthdayLast matches the last occurrence of the weekday in the month.
	// Positions 1 to 4 match the first to fourth occurrence.
	nthdayLast nthday = 5
)

// clock is a time of day.
type clock struct {
	hour   uint8
	minute uint8
}

func (c clock) String() string {
	return fmt.Sprintf("%02d:%02d", c.hour, c.minute)
}

func (c clock) duration() time.Duration {
	return time.Duration(c.hour)*time.Hour + time.Duration(c.minute)*time.Minute
}

// clockSpan is a span of time within a day, from start up to end. A span
// whose end is at or before its start crosses midnight, so "0:00-0:00" is
// the whole day. The span triggers at start, and then again every period
// for as long as the trigger is still before end.
type clockSpan struct {
	start  clock
	end    clock
	period time.Duration
}

// duration returns the length of the clock span.
func (cs clockSpan) duration() time.Duration {
	d := cs.end.duration() - cs.start.duration()
	if d <= 0 {
		d += 24 * time.Hour
	}
	return d
}

// weekdayPos is a weekday, optionally restricted to a position within the
// month.
type weekdayPos struct {
	weekday time.Weekday
	pos     nthday
}

// weekSpan is a span of days, from the start weekday through to the end
// weekday. Spans wrap around the week, so "fri-mon" includes Friday,
// Saturday, Sunday and Monday.
//
// When start has a position, the span begins on that day and continues
// through the following end weekday (at end's position, if any). When only
// end has a position, the span ends on that day and begins on the nearest
// preceding start weekday.
type weekSpan struct {
	start weekdayPos
	end   weekdayPos
}

// set is one set of a schedule. A time matches a set when its day is
// selected by days, matches one of weekSpans, and is inside one of
// clockSpans.
type set struct {
	// days holds one bitmask per month (January first), with bit N set
	// when day N of that month is selected (bit 0 is unused).
	days       [12]uint32
	weekSpans  []weekSpan
	clockSpans []clockSpan
}

// Schedule is a parsed schedule string.
//
// A Schedule returned by Parse must be treated as immutable: it may then be
// shared between goroutines, as Next only reads it.
type Schedule struct {
	sets []set
	text string
}

func (s *Schedule) String() string {
	return s.text
}

// ErrNoNext is returned by Schedule.Next when the schedule has no further
// triggers.
var ErrNoNext = errors.New("schedule has no further triggers")

// Next returns the time of the earliest trigger of the schedule that hasn't
// yet happened, given that the last event happened at last and the current
// time is now. The returned time is never before the start of the span that
// contains it, but may be before now when now is inside a clock span
// whose trigger hasn't happened since last. The search uses now's location.
//
// Triggers are at wall-clock times in that location, so a span split
// "/30m" triggers at :00 and :30 past the hour even across a daylight saving
// time transition. When the clocks go forward, a trigger at a skipped
// wall-clock time happens at the instant the clocks skip to. When the clocks
// go back, a trigger at a repeated wall-clock time happens only once, at the
// earlier of the two instants.
//
// Next returns errNoNext if the schedule has no further triggers, or
// another error if the schedule is invalid.
func (s *Schedule) Next(last, now time.Time) (time.Time, error) {
	if err := s.validate(); err != nil {
		return time.Time{}, fmt.Errorf("cannot evaluate schedule due to %w", err)
	}
	var next time.Time
	found := false
	for i := range s.sets {
		t, ok := s.sets[i].next(last, now)
		if ok && (!found || t.Before(next)) {
			next = t
			found = true
		}
	}
	if !found {
		return time.Time{}, ErrNoNext
	}
	return next, nil
}

// validate checks the invariants that Parse guarantees and Next relies on.
func (s *Schedule) validate() error {
	for _, st := range s.sets {
		for _, wspan := range st.weekSpans {
			for _, wp := range []weekdayPos{wspan.start, wspan.end} {
				if wp.weekday < time.Sunday || wp.weekday > time.Saturday {
					return fmt.Errorf("invalid weekday %d", wp.weekday)
				}
				if wp.pos < nthdayAny || wp.pos > nthdayLast {
					return fmt.Errorf("invalid weekday position %d", wp.pos)
				}
			}
		}
		for _, cspan := range st.clockSpans {
			for _, c := range []clock{cspan.start, cspan.end} {
				if c.hour > 23 || c.minute > 59 {
					return fmt.Errorf("invalid clock %d:%02d", c.hour, c.minute)
				}
			}
			if cspan.period <= 0 {
				return fmt.Errorf("invalid clock span period %v", cspan.period)
			}
		}
	}
	return nil
}

// next returns the earliest trigger of the set that is after last and whose
// clock span hasn't ended by now, searching day by day from the day before
// now. It returns false if there is none within maxSearchDays.
func (st *set) next(last, now time.Time) (time.Time, bool) {
	loc := now.Location()

	// Start from the day before now, as it may hold a clock span crossing
	// midnight into today.
	y, m, d := now.Date()
	day := civilDate(y, m, d-1)

	for range maxSearchDays {
		if !st.matchesDay(day) {
			day = st.skipDay(day)
			continue
		}
		var next time.Time
		found := false
		for _, cspan := range st.clockSpans {
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
func (st *set) skipDay(day time.Time) time.Time {
	if st.days[day.Month()-1]>>(day.Day()+1) == 0 {
		y, m, _ := day.Date()
		return civilDate(y, m+1, 1)
	}
	return day.AddDate(0, 0, 1)
}

// next returns the earliest trigger of the clock span starting on day that
// is after last and whose span hasn't ended by now.
//
// Triggers are worked out as wall-clock (civil) times, kept in UTC so the
// arithmetic isn't affected by DST, and only then converted to loc.
func (cs clockSpan) next(day, last, now time.Time, loc *time.Location) (time.Time, bool) {
	civilStart := day.Add(cs.start.duration())
	start, _ := wallTime(civilStart, loc)
	end, skipped := wallTime(civilStart.Add(cs.duration()), loc)
	if skipped {
		// The span ends in skipped wall-clock time, so its triggers in the
		// skipped time all happen at end. Keep the span open until then.
		end = end.Add(time.Nanosecond)
	}

	if !end.After(now) || !end.After(last) {
		// Span is entirely in the past.
		return time.Time{}, false
	}
	if start.After(last) {
		return start, true
	}
	// The last event happened inside this span, so find the following part.
	// The wall clock of last can be before civilStart when the clocks went
	// back, or when start was skipped forward, so begin at the first part.
	part := max(civil(last.In(loc)).Sub(civilStart)/cs.period+1, 1)
	for ; part*cs.period < cs.duration(); part++ {
		// Normally the first part tried is after last, but when the clocks
		// go back, wall-clock triggers already passed may come round again.
		t, _ := wallTime(civilStart.Add(part*cs.period), loc)
		if t.After(last) {
			return t, true
		}
	}
	return time.Time{}, false
}

// civil returns the wall-clock time of t as the same wall-clock time in UTC.
func civil(t time.Time) time.Time {
	y, m, d := t.Date()
	hour, minute, second := t.Clock()
	return time.Date(y, m, d, hour, minute, second, t.Nanosecond(), time.UTC)
}

// wallTime returns the instant at which the wall clock in loc shows the
// civil time c (see civil). When the wall clock skips over c, as when DST
// starts, it returns the instant the clock skips to, and skipped is true.
// When the wall clock shows c twice, as when DST ends, it returns the
// earlier instant.
func wallTime(c time.Time, loc *time.Location) (t time.Time, skipped bool) {
	y, m, d := c.Date()
	hour, minute, second := c.Clock()
	t = time.Date(y, m, d, hour, minute, second, c.Nanosecond(), loc)
	zoneStart, zoneEnd := t.ZoneBounds()
	switch wall := civil(t); {
	case wall.After(c):
		// Skipped, and t is in the zone after the transition.
		return zoneStart, true
	case wall.Before(c):
		// Skipped, and t is in the zone before the transition.
		return zoneEnd, true
	}
	if !zoneStart.IsZero() {
		// If the clocks went back at the start of t's zone, c may also have
		// been shown before then.
		_, offset := t.Zone()
		_, prevOffset := zoneStart.Add(-time.Nanosecond).Zone()
		if prevOffset > offset {
			earlier := t.Add(-time.Duration(prevOffset-offset) * time.Second)
			if civil(earlier).Equal(c) {
				return earlier, false
			}
		}
	}
	return t, false
}

// civilDate returns midnight UTC of the given date, normalised. Dates are
// kept in UTC for calendar arithmetic, so it isn't affected by DST.
func civilDate(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// matchesDay reports whether day is selected by the set's days and falls
// within one of its week spans. Clock spans aren't considered.
func (st *set) matchesDay(day time.Time) bool {
	if st.days[day.Month()-1]&(1<<day.Day()) == 0 {
		return false
	}
	for _, wspan := range st.weekSpans {
		if wspan.matches(day) {
			return true
		}
	}
	return false
}

// matches reports whether day falls within the week span, taking any
// positions within the month of its start or end weekday into account.
func (ws weekSpan) matches(day time.Time) bool {
	switch {
	case ws.start.pos == nthdayAny && ws.end.pos == nthdayAny:
		return weekdayDelta(ws.start.weekday, day.Weekday()) <= weekdayDelta(ws.start.weekday, ws.end.weekday)
	case ws.start.pos != nthdayAny:
		// Anchored at the start: the span may have started in this month
		// or the one before.
		y, m, _ := day.Date()
		for _, month := range []time.Month{m, m - 1} {
			start := nthWeekday(y, month, ws.start.weekday, ws.start.pos)
			if start.After(day) {
				continue
			}
			end := ws.end.onOrAfter(start)
			if !day.After(end) {
				return true
			}
		}
	default:
		// Anchored at the end: the span may end in this month or the next.
		y, m, _ := day.Date()
		for _, month := range []time.Month{m, m + 1} {
			end := nthWeekday(y, month, ws.end.weekday, ws.end.pos)
			if end.Before(day) {
				continue
			}
			start := end.AddDate(0, 0, -weekdayDelta(ws.start.weekday, end.Weekday()))
			if !day.Before(start) {
				return true
			}
		}
	}
	return false
}

// onOrAfter returns the first day on or after t that matches the weekday
// and position.
func (wp weekdayPos) onOrAfter(t time.Time) time.Time {
	if wp.pos == nthdayAny {
		return t.AddDate(0, 0, weekdayDelta(t.Weekday(), wp.weekday))
	}
	y, m, _ := t.Date()
	for {
		day := nthWeekday(y, m, wp.weekday, wp.pos)
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
// or of the last one if pos is nthdayLast. The month is normalised.
func nthWeekday(y int, m time.Month, wd time.Weekday, pos nthday) time.Time {
	if pos == nthdayLast {
		last := civilDate(y, m+1, 0)
		return last.AddDate(0, 0, -weekdayDelta(wd, last.Weekday()))
	}
	first := civilDate(y, m, 1)
	return first.AddDate(0, 0, weekdayDelta(first.Weekday(), wd)+7*(int(pos)-1))
}
