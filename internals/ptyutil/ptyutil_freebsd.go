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
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// ioctlGetTermios is the ioctl request that reads a terminal's attributes.
const ioctlGetTermios = unix.TIOCGETA

// OpenPty creates a new PTS pair, configures them and returns them.
func OpenPty(uid, gid int64) (*os.File, *os.File, error) {
	revert := true

	// FreeBSD has no /dev/ptmx; posix_openpt(2) is a system call there.
	fd, _, errno := unix.Syscall(unix.SYS_POSIX_OPENPT, uintptr(os.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC), 0, 0)
	if errno != 0 {
		return nil, nil, errno
	}
	ptx := os.NewFile(fd, "/dev/ptmx")
	defer func() {
		if revert {
			ptx.Close()
		}
	}()

	// Get the pty number and open the pty side.
	id, err := unix.IoctlGetInt(int(ptx.Fd()), unix.TIOCGPTN)
	if err != nil {
		return nil, nil, err
	}
	pty, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", id), os.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if revert {
			pty.Close()
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
