//go:build termus
// +build termus

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

package boot

import (
	"errors"
	"os"
	"path"

	"github.com/canonical/pebble/internal/osutil"
	. "gopkg.in/check.v1"
)

type utilSuite struct{}

var _ = Suite(&utilSuite{})

func (s *utilSuite) TestMount(c *C) {
	m := &mount{
		source: "/dev/nvme0n1p3",
		target: path.Join(c.MkDir(), "test", "mountpoint"),
		fstype: "btrfs",
		flags:  42,
		data:   "test",
	}

	oldSyscallMount := syscallMount
	defer func() { syscallMount = oldSyscallMount }()
	syscallMount = func(source, target, fstype string, flags uintptr, data string) error {
		c.Assert(source, Equals, m.source)
		c.Assert(target, Equals, m.target)
		c.Assert(fstype, Equals, m.fstype)
		c.Assert(flags, Equals, m.flags)
		c.Assert(data, Equals, m.data)
		return nil
	}

	err := m.mount()
	c.Assert(err, IsNil)
	c.Assert(osutil.IsDir(m.target), Equals, true)

	info, err := os.Stat(m.target)
	c.Assert(err, IsNil)
	c.Assert(info.Mode()&os.ModePerm, Equals, os.FileMode(0755))
}

func (s *utilSuite) TestMountFailsOnSyscall(c *C) {
	m := &mount{
		source: "/dev/nvme0n1p3",
		target: path.Join(c.MkDir(), "test", "mountpoint"),
		fstype: "btrfs",
		flags:  42,
		data:   "test",
	}

	oldSyscallMount := syscallMount
	defer func() { syscallMount = oldSyscallMount }()
	syscallMount = func(source, target, fstype string, flags uintptr, data string) error {
		c.Assert(source, Equals, m.source)
		c.Assert(target, Equals, m.target)
		c.Assert(fstype, Equals, m.fstype)
		c.Assert(flags, Equals, m.flags)
		c.Assert(data, Equals, m.data)
		return errors.New("cannot foo")
	}

	err := m.mount()
	c.Assert(err, ErrorMatches, `cannot mount "/dev/nvme0n1p3": cannot foo`)
}

func (s *utilSuite) TestMountFailsOnMkdir(c *C) {
	root := c.MkDir()
	m := &mount{
		source: "/dev/nvme0n1p3",
		target: path.Join(root, "test", "mountpoint"),
		fstype: "btrfs",
		flags:  42,
		data:   "test",
	}

	err := os.Chmod(root, os.FileMode(0400))
	c.Assert(err, IsNil)

	oldSyscallMount := syscallMount
	defer func() { syscallMount = oldSyscallMount }()
	syscallMount = func(source, target, fstype string, flags uintptr, data string) error {
		c.Assert(source, Equals, m.source)
		c.Assert(target, Equals, m.target)
		c.Assert(fstype, Equals, m.fstype)
		c.Assert(flags, Equals, m.flags)
		c.Assert(data, Equals, m.data)
		return errors.New("cannot foo")
	}

	err = m.mount()
	c.Assert(err, ErrorMatches, `cannot create directory .*: .*permission denied`)
}

func (s *utilSuite) TestUnmount(c *C) {
	m := &mount{
		source: "/dev/nvme0n1p3",
		target: path.Join(c.MkDir(), "test", "mountpoint"),
		fstype: "btrfs",
		flags:  42,
		data:   "test",
	}

	oldSyscallUnmount := syscallUnmount
	defer func() { syscallUnmount = oldSyscallUnmount }()
	syscallUnmount = func(target string, flags int) error {
		c.Assert(target, Equals, m.target)
		c.Assert(flags, Equals, 0)
		return nil
	}

	err := m.unmount()
	c.Assert(err, IsNil)
}

func (s *utilSuite) TestUnmountFails(c *C) {
	m := &mount{
		source: "/dev/nvme0n1p3",
		target: path.Join(c.MkDir(), "test", "mountpoint"),
		fstype: "btrfs",
		flags:  42,
		data:   "test",
	}

	oldSyscallUnmount := syscallUnmount
	defer func() { syscallUnmount = oldSyscallUnmount }()
	syscallUnmount = func(target string, flags int) error {
		c.Assert(target, Equals, m.target)
		c.Assert(flags, Equals, 0)
		return errors.New("cannot foo")
	}

	err := m.unmount()
	c.Assert(err, ErrorMatches, `cannot unmount .*: cannot foo`)
}
