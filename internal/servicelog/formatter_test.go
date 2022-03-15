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

package servicelog_test

import (
	"bytes"
	"fmt"
	"regexp"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internal/servicelog"
)

const (
	timeFormatRegex = `\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z`
)

type formatterSuite struct{}

var _ = Suite(&formatterSuite{})

func (s *formatterSuite) TestFormatWriter(c *C) {
	b := &bytes.Buffer{}
	w := servicelog.NewFormatWriter(b, "test")

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
	w := servicelog.NewFormatWriter(b, "test")

	fmt.Fprintf(w, "first\nsecond\nthird\n")

	c.Assert(b.String(), Matches, fmt.Sprintf(`
%[1]s \[test\] first
%[1]s \[test\] second
%[1]s \[test\] third
`[1:], timeFormatRegex))
}

func (s *formatterSuite) TestTrimWriter(c *C) {
	raw := `
3/4/3005 hello my name is joe
4/5/4200 and I work in a button factory
1/1/0033 this log entry is very old
and dates in the middle 1/1/0033 are kept
1/1/0034 check that no-trailing-newline case is flushed`[1:]

	trimmed := `
hello my name is joe
and I work in a button factory
this log entry is very old
and dates in the middle 1/1/0033 are kept
check that no-trailing-newline case is flushed`[1:]

	chunkSizes := []int{1, 2, 3, 4, 5, 6, 7, 11, 13, 27, 100}
	for _, size := range chunkSizes {
		b := &bytes.Buffer{}
		w := servicelog.NewTrimWriter(b, regexp.MustCompile("^[0-9]{1,2}/[0-9]{1,2}/[0-9]{4} +"))
		c.Logf("---- chunk size %v ----", size)
		for pos := 0; pos < len(raw); pos += size {
			end := pos + size
			if end > len(raw) {
				end = len(raw)
			}
			fmt.Fprint(w, raw[pos:end])
			w.Write([]byte{}) // shouldn't break anything
		}
		w.Flush()
		c.Assert(b.String(), Equals, trimmed)
		w.Flush() // should be idempotent
		c.Assert(b.String(), Equals, trimmed)
	}
}
