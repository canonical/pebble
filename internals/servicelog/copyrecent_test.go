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

package servicelog_test

import (
	"io"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/servicelog"
)

type copyRecentSuite struct{}

var _ = Suite(&copyRecentSuite{})

func (s *copyRecentSuite) TestCopyRecentFiltersByTime(c *C) {
	src := servicelog.NewRingBuffer(4096)
	defer src.Close()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c.Assert(servicelog.WriteEntry(src, "svc", base, "too old"), IsNil)
	c.Assert(servicelog.WriteEntry(src, "svc", base.Add(30*time.Second), "recent 1"), IsNil)
	c.Assert(servicelog.WriteEntry(src, "svc", base.Add(45*time.Second), "recent 2"), IsNil)

	dest := servicelog.NewRingBuffer(4096)
	defer dest.Close()

	cutoff := base.Add(10 * time.Second)
	err := servicelog.CopyRecent(dest, src, cutoff)
	c.Assert(err, IsNil)

	it := dest.TailIterator()
	defer it.Close()
	out, err := io.ReadAll(it)
	c.Assert(err, IsNil)
	c.Check(string(out), Equals,
		"2026-01-01T00:00:30.000Z [svc] recent 1\n"+
			"2026-01-01T00:00:45.000Z [svc] recent 2\n")
}

func (s *copyRecentSuite) TestCopyRecentNoMatches(c *C) {
	src := servicelog.NewRingBuffer(4096)
	defer src.Close()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c.Assert(servicelog.WriteEntry(src, "svc", base, "old line"), IsNil)

	dest := servicelog.NewRingBuffer(4096)
	defer dest.Close()

	err := servicelog.CopyRecent(dest, src, base.Add(time.Minute))
	c.Assert(err, IsNil)

	c.Check(dest.Buffered(), Equals, 0)
}

func (s *copyRecentSuite) TestCopyRecentEmptySource(c *C) {
	src := servicelog.NewRingBuffer(4096)
	defer src.Close()

	dest := servicelog.NewRingBuffer(4096)
	defer dest.Close()

	err := servicelog.CopyRecent(dest, src, time.Now())
	c.Assert(err, IsNil)
	c.Check(dest.Buffered(), Equals, 0)
}
