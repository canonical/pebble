// Copyright (c) 2023 Canonical Ltd
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

package partinfo

import (
	. "gopkg.in/check.v1"
	"os"
)

func (s *partinfoSuite) TestExtPartitionLabels(c *C) {
	expected := []struct {
		path  string
		label string
	}{
		{path: "testdata/empty.ext2", label: ""},
		{path: "testdata/empty.ext3", label: ""},
		{path: "testdata/empty.ext4", label: ""},
		{path: "testdata/labelled.ext2", label: "A simple label"},
		{path: "testdata/labelled.ext3", label: "A nice label"},
		{path: "testdata/labelled.ext4", label: "A great label"},
	}

	for _, e := range expected {
		f, err := os.Open(e.path)
		c.Assert(err, IsNil)

		p, err := newExtPartition(f)
		c.Assert(err, IsNil)
		c.Assert(p.FSType(), Equals, "ext4")
		c.Assert(p.Path(), Equals, f.Name())
		c.Assert(p.Label(), Equals, e.label)

		err = f.Close()
		c.Assert(err, IsNil)
	}
}

func (s *partinfoSuite) TestExtFailsNotEnoughBytes(c *C) {
	f, err := os.Open("testdata/garbage-small.bin")
	c.Assert(err, IsNil)

	_, err = newExtPartition(f)
	c.Assert(err, ErrorMatches, "cannot read superblock: EOF")

	err = f.Close()
	c.Assert(err, IsNil)
}

func (s *partinfoSuite) TestExtFailsOnGarbage(c *C) {
	f, err := os.OpenFile("testdata/garbage.bin", os.O_RDONLY, 0)
	c.Assert(err, IsNil)

	_, err = newExtPartition(f)
	c.Assert(err, ErrorMatches, "invalid Ext magic")

	err = f.Close()
	c.Assert(err, IsNil)
}
