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

	. "gopkg.in/check.v1"
)

type utilSuite struct{}

var _ = Suite(&utilSuite{})

func buildMountImpl(err error) func(string, string, string, uintptr, string) error {
	return func(string, string, string, uintptr, string) error {
		return err
	}
}

func buildUnmountImpl(err error) func(string, int) error {
	return func(string, int) error {
		return err
	}
}

func (s *utilSuite) TestMount(c *C) {
	oldMountImpl := MountImpl
	defer func() { MountImpl = oldMountImpl }()
	MountImpl = buildMountImpl(nil)

	m := &mount{"/dev/nvme0n1p3", "/boot", "ext4", 0, ""}
	err := m.mount()
	c.Assert(err, IsNil)
}

func (s *utilSuite) TestMountFails(c *C) {
	oldMountImpl := MountImpl
	defer func() { MountImpl = oldMountImpl }()
	MountImpl = buildMountImpl(errors.New("cannot foo"))

	m := &mount{"/dev/nvme0n1p3", "/boot", "ext4", 0, ""}
	err := m.mount()
	c.Assert(err, ErrorMatches, `cannot mount "/dev/nvme0n1p3": cannot foo`)
}

func (s *utilSuite) TestUnmount(c *C) {
	oldUnmountImpl := UnmountImpl
	defer func() { UnmountImpl = oldUnmountImpl }()
	UnmountImpl = buildUnmountImpl(nil)

	m := &mount{"/dev/nvme0n1p3", "/boot", "ext4", 0, ""}
	err := m.unmount()
	c.Assert(err, IsNil)
}

func (s *utilSuite) TestUnmountFails(c *C) {
	oldUnmountImpl := UnmountImpl
	defer func() { UnmountImpl = oldUnmountImpl }()
	UnmountImpl = buildUnmountImpl(errors.New("cannot bar"))

	m := &mount{"/dev/nvme0n1p3", "/boot", "ext4", 0, ""}
	err := m.unmount()
	c.Assert(err, ErrorMatches, `cannot unmount "/boot": cannot bar`)
}
