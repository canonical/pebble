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

//go:build darwin || freebsd

package ptyutil

import (
	"os"

	"golang.org/x/sys/unix"
)

// ioctlGetTermios is the ioctl request that reads a terminal's attributes.
const ioctlGetTermios = unix.TIOCGETA

// OpenPty creates a new PTS pair, configures them and returns them.
//
// Opening the master and locating the slave differs between FreeBSD and
// Darwin; see openPtyPair in ptyutil_<os>.go. Configuring the pair is the
// same on both.
func OpenPty(uid, gid int64) (*os.File, *os.File, error) {
	revert := true

	ptx, pty, err := openPtyPair()
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if revert {
			pty.Close()
			ptx.Close()
		}
	}()

	// Configure both sides
	for _, entry := range []*os.File{ptx, pty} {
		t, err := unix.IoctlGetTermios(int(entry.Fd()), unix.TIOCGETA)
		if err != nil {
			return nil, nil, err
		}
		t.Iflag |= unix.IMAXBEL | unix.BRKINT | unix.IXANY
		t.Cflag |= unix.HUPCL
		err = unix.IoctlSetTermios(int(entry.Fd()), unix.TIOCSETA, t)
		if err != nil {
			return nil, nil, err
		}

		// Set the default window size.
		err = unix.IoctlSetWinsize(int(entry.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Col: 80, Row: 25})
		if err != nil {
			return nil, nil, err
		}
	}

	// Fix the ownership of the pty side.
	err = unix.Fchown(int(pty.Fd()), int(uid), int(gid))
	if err != nil {
		return nil, nil, err
	}

	revert = false
	return ptx, pty, nil
}
