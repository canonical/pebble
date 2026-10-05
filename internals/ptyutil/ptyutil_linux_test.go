// Copyright (c) 2026 Canonical Ltd
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
	"math"
	"os"

	"golang.org/x/sys/unix"

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

func (s *ptyutilSuite) TestSetSizeBounds(c *C) {
	ptx, pty, err := OpenPty(int64(os.Getuid()), int64(os.Getgid()))
	c.Assert(err, IsNil)
	defer ptx.Close()
	defer pty.Close()

	// Valid dimensions should succeed.
	err = SetSize(int(ptx.Fd()), 80, 24)
	c.Assert(err, IsNil)

	w, h, err := GetSize(int(ptx.Fd()))
	c.Assert(err, IsNil)
	c.Check(w, Equals, 80)
	c.Check(h, Equals, 24)

	// Max uint16 dimensions should succeed.
	err = SetSize(int(ptx.Fd()), math.MaxUint16, math.MaxUint16)
	c.Assert(err, IsNil)

	w, h, err = GetSize(int(ptx.Fd()))
	c.Assert(err, IsNil)
	c.Check(w, Equals, math.MaxUint16)
	c.Check(h, Equals, math.MaxUint16)

	// Zero dimensions are representable in uint16.
	err = SetSize(int(ptx.Fd()), 0, 0)
	c.Assert(err, IsNil)

	// Negative values or values exceeding MaxUint16 should return an error.
	tests := []struct {
		width  int
		height int
	}{
		{-1, 24},
		{80, -1},
		{-1, -1},
		{math.MaxUint16 + 1, 24},
		{80, math.MaxUint16 + 1},
		{math.MaxUint16 + 1, math.MaxUint16 + 1},
		{math.MinInt, 24},
		{80, math.MinInt},
		{math.MaxInt, 24},
		{80, math.MaxInt},
	}

	for _, tc := range tests {
		err := SetSize(int(ptx.Fd()), tc.width, tc.height)
		c.Check(err, ErrorMatches, "cannot set terminal size: dimension out of range")
	}
}

func (s *ptyutilSuite) TestOpenPtyTermiosFlags(c *C) {
	// The OpenPtyInDevpts function would typically be passed a file descriptor
	// to a devpts file system in a mount namespace created by the container
	// runtime. The host's devpts is usually mounted with ptmxmode=0000, so
	// opening its ptmx would require root. The test uses a file descriptor to
	// /dev instead, in which the `ptmx` file has 0666 permissions.
	dev, err := os.Open("/dev")
	c.Assert(err, IsNil)
	defer dev.Close()

	ptm, pts, err := OpenPtyInDevpts(int(dev.Fd()), int64(os.Getuid()), int64(os.Getgid()))
	c.Assert(err, IsNil, Commentf("open pty pair"))
	defer pts.Close()
	defer ptm.Close()

	wantIflag := uint32(unix.IMAXBEL | unix.IUTF8 | unix.BRKINT | unix.IXANY)
	wantCflag := uint32(unix.HUPCL)

	for _, f := range []*os.File{ptm, pts} {
		tio, err := unix.IoctlGetTermios(int(f.Fd()), unix.TCGETS)
		c.Assert(err, IsNil)

		if tio.Iflag&wantIflag != wantIflag {
			c.Errorf("Iflag: actual = %#x, expected = %#x", tio.Iflag, wantIflag)
		}
		if tio.Cflag&wantCflag != wantCflag {
			c.Errorf("Cflag: actual = %#x, expected = %#x", tio.Cflag, wantCflag)
		}
	}

	// Test writing a VERASE (0x7f) character after a 2-byte UTF-8 character.
	// Without the IUTF8 Iflag set, only one byte will be deleted, leaving
	// garbage in the buffer.
	_, err = ptm.Write([]byte("é\x7f\n"))
	c.Assert(err, IsNil)

	buf := make([]byte, 8)
	l, err := pts.Read(buf)
	c.Assert(err, IsNil)

	c.Assert(buf[:l], DeepEquals, []byte("\n"))
}
