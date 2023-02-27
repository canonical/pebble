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
	"path"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"
)

var _ = Suite(&partinfoSuite{})

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type partinfoSuite struct {
	sysfsPath, devfsPath       string
	oldSysfsPath, oldDevfsPath string
}

func (s *partinfoSuite) SetUpSuite(c *C) {
	root := c.MkDir()
	s.sysfsPath = path.Join(root, "sys")
	s.devfsPath = path.Join(root, "dev")

	err := os.MkdirAll(s.sysfsPath, 0777)
	c.Assert(err, IsNil)
	err = os.MkdirAll(s.devfsPath, 0777)
	c.Assert(err, IsNil)

	s.oldSysfsPath = sysfsPath
	s.oldDevfsPath = devfsPath

	sysfsPath = s.sysfsPath
	devfsPath = s.devfsPath
}

func (s *partinfoSuite) TearDownSuite(_ *C) {
	sysfsPath = s.oldSysfsPath
	devfsPath = s.oldDevfsPath
}

func (s *partinfoSuite) TestEnumeratePartitions(c *C) {
	// Create /dev/sda1
	fat32Path, err := filepath.Abs("testdata/labelled.fat32")
	c.Assert(err, IsNil)
	err = os.Symlink(fat32Path, path.Join(s.devfsPath, "sda1"))
	c.Assert(err, IsNil)
	// Create /sys/block/sda and /sys/block/sda/sda1
	err = os.MkdirAll(path.Join(s.sysfsPath, "block", "sda", "sda1"), 0777)
	c.Assert(err, IsNil)

	// Create /dev/sda2
	ext4Path, err := filepath.Abs("testdata/labelled.ext4")
	err = os.Symlink(ext4Path, path.Join(s.devfsPath, "sda2"))
	c.Assert(err, IsNil)
	// Create /sys/block/sda/sda2
	err = os.MkdirAll(path.Join(s.sysfsPath, "block", "sda", "sda2"), 0777)
	c.Assert(err, IsNil)

	// Create /sys/block/sda/trace and /sys/block/sda/ro (which do not represent partitions)
	err = os.MkdirAll(path.Join(s.sysfsPath, "block", "sda", "trace"), 0777)
	c.Assert(err, IsNil)
	_, err = os.Create(path.Join(s.sysfsPath, "block", "sda", "ro"))
	c.Assert(err, IsNil)

	p, err := Enumerate()
	c.Assert(err, IsNil)
	c.Assert(p, HasLen, 2)
	c.Assert(p[0].Path(), Equals, path.Join(s.devfsPath, "sda1"))
	c.Assert(p[0].Label(), Equals, "My label")
	c.Assert(p[0].FSType(), Equals, "vfat")
	c.Assert(p[1].Path(), Equals, path.Join(s.devfsPath, "sda2"))
	c.Assert(p[1].Label(), Equals, "A great label")
	c.Assert(p[1].FSType(), Equals, "ext4")
}

func (s *partinfoSuite) TestEnumeratePartitionsFailsWithInaccessibleSysfs(c *C) {
	err := os.Chmod(path.Join(s.sysfsPath, "block"), 0)
	c.Assert(err, IsNil)

	defer func() { os.Chmod(path.Join(s.sysfsPath, "block"), 0777) }()

	_, err = Enumerate()
	c.Assert(os.IsPermission(err), Equals, true)
}

func (s *partinfoSuite) TestEnumeratePartitionsFailsWithInaccessibleBlockDeviceEntry(c *C) {
	err := os.MkdirAll(path.Join(s.sysfsPath, "block", "inaccessible"), 0)
	c.Assert(err, IsNil)

	_, err = Enumerate()
	c.Assert(os.IsPermission(err), Equals, true)
}
