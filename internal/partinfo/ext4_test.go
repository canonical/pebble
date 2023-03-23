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

	. "gopkg.in/check.v1"
)

func (s *partinfoSuite) TestExt4PartitionLabels(c *C) {
	expected := []struct {
		path  string
		label string
	}{
		{path: "testdata/ext4-superblock-empty.dat", label: ""},
		{path: "testdata/ext4-superblock-label.dat", label: "A great label"},
	}

	for _, e := range expected {
		f, err := os.Open(e.path)
		c.Assert(err, IsNil)

		p, err := newExt4Partition(f)
		c.Assert(err, IsNil)
		c.Assert(p.MountType(), Equals, MountTypeExt4)
		c.Assert(p.DevicePath(), Equals, f.Name())
		c.Assert(p.MountLabel(), Equals, e.label)

		err = f.Close()
		c.Assert(err, IsNil)
	}
}

func (s *partinfoSuite) TestExt4FailsNotEnoughBytes(c *C) {
	f, err := os.Open("testdata/garbage-8.bin")
	c.Assert(err, IsNil)

	_, err = newExt4Partition(f)
	c.Assert(err, ErrorMatches, "cannot read superblock: EOF")

	err = f.Close()
	c.Assert(err, IsNil)
}

func (s *partinfoSuite) TestExt4FailsOnGarbage(c *C) {
	f, err := os.OpenFile("testdata/garbage-2k.bin", os.O_RDONLY, 0)
	c.Assert(err, IsNil)

	_, err = newExt4Partition(f)
	c.Assert(err, ErrorMatches, "invalid ext4 magic")

	err = f.Close()
	c.Assert(err, IsNil)
}
