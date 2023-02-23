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

package bootstrap

import (
	"errors"
	"os"
	"testing"

	. "gopkg.in/check.v1"
)

type bootstrapSuite struct{}

var _ = Suite(&bootstrapSuite{})

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

func (s *bootstrapSuite) TestValidate(c *C) {
	err := Validate()
	c.Assert(err, ErrorMatches, "must run as PID 1. Use --force to suppress this check")
}

func (s *bootstrapSuite) TestValidatePID1(c *C) {
	oldGetpid := osGetpid
	osGetpid = func() int { return 1 }
	defer func() { osGetpid = oldGetpid }()

	err := Validate()
	c.Assert(err, ErrorMatches, "TERMUS environment variable must be set to 1. Use --force to suppress this check")
}

func (s *bootstrapSuite) TestValidatePID1AndEnv(c *C) {
	oldGetpid := osGetpid
	osGetpid = func() int { return 1 }
	defer func() { osGetpid = oldGetpid }()

	oldTermus := os.Getenv("TERMUS")
	defer func() { os.Setenv("TERMUS", oldTermus) }()
	os.Setenv("TERMUS", "1")

	err := Validate()
	c.Assert(err, IsNil)
}

func (s *bootstrapSuite) TestDo(c *C) {
	var attemptedMounts []mount

	oldSyscallMount := syscallMount
	defer func() { syscallMount = oldSyscallMount }()
	syscallMount = func(source, target, fstype string, flags uintptr, data string) error {
		attemptedMounts = append(attemptedMounts, mount{source, target, fstype, flags, data})
		return nil
	}

	err := Do()
	c.Assert(err, IsNil)
	c.Assert(attemptedMounts, DeepEquals, []mount{
		{"procfs", "/proc", "proc", 0, ""},
		{"devtmpfs", "/dev", "devtmpfs", 0, ""},
		{"devpts", "/dev/pts", "devpts", 0, ""},
		{"sysfs", "/sys", "sysfs", 0, ""},
	})
}

func (s *bootstrapSuite) TestDoFails(c *C) {
	oldSyscallMount := syscallMount
	defer func() { syscallMount = oldSyscallMount }()
	syscallMount = func(string, string, string, uintptr, string) error {
		return errors.New("cannot foo")
	}

	err := Do()
	c.Assert(err, ErrorMatches, `cannot mount "procfs": cannot foo`)
}
