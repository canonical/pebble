// Copyright (c) 2014-2020 Canonical Ltd
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

package osutil_test

import (
	"errors"
	"os"
	"path"

	. "gopkg.in/check.v1"

	"golang.org/x/sys/unix"

	"github.com/canonical/pebble/internal/osutil"
)

type mountSuite struct{}

var _ = Suite(&mountSuite{})

func (s *mountSuite) TestIsMountedHappyish(c *C) {
	// note the different optional fields
	const content = "" +
		"44 24 7:1 / /snap/ubuntu-core/855 rw,relatime shared:27 - squashfs /dev/loop1 ro\n" +
		"44 24 7:1 / /snap/something/123 rw,relatime - squashfs /dev/loop2 ro\n" +
		"44 24 7:1 / /snap/random/456 rw,relatime opt:1 shared:27 - squashfs /dev/loop1 ro\n"
	defer osutil.FakeMountInfo(content)()

	mounted, err := osutil.IsMounted("/snap/ubuntu-core/855")
	c.Check(err, IsNil)
	c.Check(mounted, Equals, true)

	mounted, err = osutil.IsMounted("/snap/something/123")
	c.Check(err, IsNil)
	c.Check(mounted, Equals, true)

	mounted, err = osutil.IsMounted("/snap/random/456")
	c.Check(err, IsNil)
	c.Check(mounted, Equals, true)

	mounted, err = osutil.IsMounted("/random/made/up/name")
	c.Check(err, IsNil)
	c.Check(mounted, Equals, false)
}

func (s *mountSuite) TestIsMountedBroken(c *C) {
	defer osutil.FakeMountInfo("44 24 7:1 ...truncated-stuff")()

	mounted, err := osutil.IsMounted("/snap/ubuntu-core/855")
	c.Check(err, ErrorMatches, "incorrect number of fields, .*")
	c.Check(mounted, Equals, false)
}

func (s *mountSuite) TestMount(c *C) {
	devNode := "/dev/nvme0n1p3"
	mountpoint := path.Join(c.MkDir(), "test", "mountpoint")
	fsType := "btrfs"

	defer osutil.FakeSyscallMount(func(source, target, fstype string, flags uintptr, data string) error {
		c.Assert(source, Equals, devNode)
		c.Assert(target, Equals, mountpoint)
		c.Assert(fstype, Equals, fsType)
		c.Assert(flags, Equals, uintptr(0))
		c.Assert(data, Equals, "")
		return nil
	})()

	err := osutil.Mount(devNode, mountpoint, fsType, false)
	c.Assert(err, IsNil)
	c.Assert(osutil.IsDir(mountpoint), Equals, true)

	info, err := os.Stat(mountpoint)
	c.Assert(err, IsNil)
	c.Assert(info.Mode()&os.ModePerm, Equals, os.FileMode(0755))
}

func (s *mountSuite) TestMountReadOnly(c *C) {
	devNode := "/dev/nvme0n1p3"
	mountpoint := path.Join(c.MkDir(), "test", "ro", "mountpoint")
	fsType := "btrfs"

	defer osutil.FakeSyscallMount(func(source, target, fstype string, flags uintptr, data string) error {
		c.Assert(source, Equals, devNode)
		c.Assert(target, Equals, mountpoint)
		c.Assert(fstype, Equals, fsType)
		c.Assert(flags, Equals, uintptr(unix.MS_RDONLY))
		c.Assert(data, Equals, "")
		return nil
	})()

	err := osutil.Mount(devNode, mountpoint, fsType, true)
	c.Assert(err, IsNil)
	c.Assert(osutil.IsDir(mountpoint), Equals, true)
}

func (s *mountSuite) TestMountFailsOnSyscall(c *C) {
	defer osutil.FakeSyscallMount(func(source, target, fstype string, flags uintptr, data string) error {
		return errors.New("cannot foo")
	})()
	err := osutil.Mount("/dev/whatever", c.MkDir(), "ext4", false)
	c.Assert(err, ErrorMatches, `cannot foo`)
}

func (s *mountSuite) TestMountFailsOnMkDir(c *C) {
	root := c.MkDir()
	mountpoint := path.Join(root, "test", "mountpoint")

	err := os.Chmod(root, 0400)
	c.Assert(err, IsNil)

	err = osutil.Mount("/dev/whatever", mountpoint, "ext4", false)
	c.Assert(err, ErrorMatches, `cannot create directory .*: .*permission denied`)
}

func (s *mountSuite) TestUnmount(c *C) {
	mountpoint := "/mnt/foo"
	callsToSync := 0
	defer osutil.FakeSyscallSync(func() { callsToSync++ })()
	defer osutil.FakeSyscallUnmount(func(target string, flags int) error {
		c.Assert(target, Equals, mountpoint)
		c.Assert(flags, Equals, 0)
		return nil
	})()
	err := osutil.Unmount(mountpoint)
	c.Assert(err, IsNil)
	c.Assert(callsToSync, Equals, 1)
}

func (s *mountSuite) TestUnmountFails(c *C) {
	mountpoint := "/mnt/foo"
	defer osutil.FakeSyscallUnmount(func(target string, flags int) error {
		return errors.New("cannot bar")
	})()
	err := osutil.Unmount(mountpoint)
	c.Assert(err, ErrorMatches, `cannot bar`)
}
