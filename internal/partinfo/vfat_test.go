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

func (s *partinfoSuite) TestVfatPartitionLabels(c *C) {
	expected := []struct {
		path  string
		label string
	}{
		{path: "testdata/vfat-superblock-empty.dat", label: ""},
		{path: "testdata/vfat-superblock-labelled.dat", label: "My label"},
	}

	for _, e := range expected {
		f, err := os.Open(e.path)
		c.Assert(err, IsNil)

		p, err := newVFATPartition(f)
		c.Assert(err, IsNil)
		c.Assert(p.MountType(), Equals, MountTypeFAT32)
		c.Assert(p.DevicePath(), Equals, f.Name())
		c.Assert(p.MountLabel(), Equals, e.label)

		err = f.Close()
		c.Assert(err, IsNil)
	}
}

func (s *partinfoSuite) TestVfatFailsNotEnoughBytes(c *C) {
	f, err := os.OpenFile("testdata/garbage-8.bin", os.O_RDONLY, 0)
	c.Assert(err, IsNil)

	_, err = newVFATPartition(f)
	c.Assert(err, ErrorMatches, "cannot read superblock: unexpected EOF")

	err = f.Close()
	c.Assert(err, IsNil)
}

func (s *partinfoSuite) TestVfatFailsOnGarbage(c *C) {
	f, err := os.OpenFile("testdata/garbage-2k.bin", os.O_RDONLY, 0)
	c.Assert(err, IsNil)

	_, err = newVFATPartition(f)
	c.Assert(err, ErrorMatches, "invalid MBR signature")

	err = f.Close()
	c.Assert(err, IsNil)
}
