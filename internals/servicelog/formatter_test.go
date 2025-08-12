// Copyright (c) 2021 Canonical Ltd
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
// along with this program.  If not, see <http://www.gnu.org/licenses/>

package servicelog

import (
	"bytes"
	"fmt"
	"time"

	. "gopkg.in/check.v1"
)

const (
	timeFormatRegex = `\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z`
)

type formatterSuite struct{}

var _ = Suite(&formatterSuite{})

func (s *formatterSuite) TestFormat(c *C) {
	b := &bytes.Buffer{}
	w := NewFormatWriter(b, "test")

	fmt.Fprintln(w, "first")
	fmt.Fprintln(w, "second")
	fmt.Fprintln(w, "third")

	c.Assert(b.String(), Matches, fmt.Sprintf(`
%[1]s \[test\] first
%[1]s \[test\] second
%[1]s \[test\] third
`[1:], timeFormatRegex))
}

func (s *formatterSuite) TestFormatSingleWrite(c *C) {
	b := &bytes.Buffer{}
	w := NewFormatWriter(b, "test")

	fmt.Fprintf(w, "first\nsecond\nthird\n")

	c.Assert(b.String(), Matches, fmt.Sprintf(`
%[1]s \[test\] first
%[1]s \[test\] second
%[1]s \[test\] third
`[1:], timeFormatRegex))
}

func (s *formatterSuite) TestAppendTimestamp(c *C) {
	now := time.Now()
	c.Assert(string(appendTimestamp(nil, now)), Equals,
		now.UTC().Format("2006-01-02T15:04:05.000Z"))

	c.Assert(string(appendTimestamp(nil, time.Time{})), Equals,
		"0001-01-01T00:00:00.000Z")
	c.Assert(string(appendTimestamp(nil, time.Date(2042, 12, 31, 23, 59, 48, 123_456_789, time.UTC))), Equals,
		"2042-12-31T23:59:48.123Z")
	c.Assert(string(appendTimestamp(nil, time.Date(2025, 8, 9, 1, 2, 3, 4_000_000, time.UTC))), Equals,
		"2025-08-09T01:02:03.004Z")
	c.Assert(string(appendTimestamp(nil, time.Date(2025, 8, 9, 1, 2, 3, 4_999_999, time.UTC))), Equals,
		"2025-08-09T01:02:03.004Z") // time.Format truncates (not rounds) milliseconds too
}
