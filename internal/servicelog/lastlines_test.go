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
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package servicelog_test

import (
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internal/servicelog"
)

type lastLinesSuite struct{}

var _ = Suite(&lastLinesSuite{})

func (s *lastLinesSuite) TestLastLinesTruncate(c *C) {
	buffer := servicelog.NewRingBuffer(1024)
	defer buffer.Close()

	for i := 1; i <= 9; i++ {
		fmt.Fprintf(buffer, "line %d\n", i)
	}
	fmt.Fprintf(buffer, "2000-01-01T00:00:00Z [foo] line 10\n")
	lines, err := servicelog.LastLines(buffer, 5, "=> ", false)
	c.Assert(err, IsNil)
	c.Assert(lines, Equals, "=> (...)\n=> line 6\n=> line 7\n=> line 8\n=> line 9\n=> 2000-01-01T00:00:00Z [foo] line 10")
}

func (s *lastLinesSuite) TestLastLinesNoIndent(c *C) {
	buffer := servicelog.NewRingBuffer(1024)
	defer buffer.Close()

	fmt.Fprintf(buffer, "foo\n")
	fmt.Fprintf(buffer, "2000-01-01T00:00:00Z [foo] bar\n")
	lines, err := servicelog.LastLines(buffer, 10, "", false)
	c.Assert(err, IsNil)
	c.Assert(lines, Equals, "foo\n2000-01-01T00:00:00Z [foo] bar")
}

func (s *lastLinesSuite) TestLastLinesStripPrefix(c *C) {
	buffer := servicelog.NewRingBuffer(1024)
	defer buffer.Close()

	fmt.Fprintf(buffer, "foo\n")
	fmt.Fprintf(buffer, "2000-01-01T00:00:00.000Z [svc1] bar\n")
	fmt.Fprintf(buffer, "2022-12-25T23:59:59.999Z [service2] log msg\n")
	lines, err := servicelog.LastLines(buffer, 10, "", true)
	c.Assert(err, IsNil)
	c.Assert(lines, Equals, "foo\nbar\nlog msg")
}
