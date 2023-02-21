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
	"os"
	"strings"

	. "gopkg.in/check.v1"
)

func (s *partinfoSuite) TestFat32PartitionLabels(c *C) {
	expected := []struct {
		path  string
		label string
	}{
		{path: "testdata/empty.fat32", label: ""},
		{path: "testdata/labelled.fat32", label: "My label"},
	}

	for _, e := range expected {
		f, err := os.OpenFile(e.path, os.O_RDONLY, 0)
		c.Assert(err, IsNil)

		sb, err := newFat32Superblock(f)
		c.Assert(err, IsNil)
		c.Assert(strings.TrimRight(string(sb.Label[:]), " "), Equals, e.label)

		err = f.Close()
		c.Assert(err, IsNil)
	}
}

func (s *partinfoSuite) TestFat32FailsNotEnoughBytes(c *C) {
	f, err := os.OpenFile("testdata/garbage-small.bin", os.O_RDONLY, 0)
	c.Assert(err, IsNil)

	_, err = newFat32Superblock(f)
	c.Assert(err, ErrorMatches, "cannot read FAT32 superblock: unexpected EOF")

	err = f.Close()
	c.Assert(err, IsNil)
}

func (s *partinfoSuite) TestFat32FailsOnGarbage(c *C) {
	f, err := os.OpenFile("testdata/garbage.bin", os.O_RDONLY, 0)
	c.Assert(err, IsNil)

	_, err = newFat32Superblock(f)
	c.Assert(err, ErrorMatches, "cannot read FAT32 superblock: not a FAT32 partition")

	err = f.Close()
	c.Assert(err, IsNil)
}
