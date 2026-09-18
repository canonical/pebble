// Copyright (c) 2025 Canonical Ltd
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

package ptyutil

import (
	"os"

	. "gopkg.in/check.v1"
)

type ptyutilSuite struct{}

var _ = Suite(&ptyutilSuite{})

func (s *ptyutilSuite) TestOpenPtyInDevptsInvalidDevptsFd(c *C) {
	f, err := os.Open("/dev/null")
	c.Assert(err, IsNil)
	fd := int(f.Fd())
	c.Assert(f.Close(), IsNil)

	ptx, pty, err := OpenPtyInDevpts(fd, int64(os.Getuid()), int64(os.Getgid()))
	c.Check(ptx, IsNil)
	c.Check(pty, IsNil)
	c.Check(err, NotNil)
}
