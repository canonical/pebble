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

// openPtyPair opens a new pty master and its slave. On FreeBSD there is no
// /dev/ptmx; posix_openpt(2) is a system call, and the slave is /dev/pts/N
// where N comes from TIOCGPTN. No grantpt/unlockpt step is needed.
func openPtyPair() (*os.File, *os.File, error) {
	fd, _, errno := unix.Syscall(unix.SYS_POSIX_OPENPT, uintptr(os.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC), 0, 0)
	if errno != 0 {
		return nil, nil, errno
	}
	ptx := os.NewFile(fd, "/dev/ptmx")

	id, err := unix.IoctlGetInt(int(ptx.Fd()), unix.TIOCGPTN)
	if err != nil {
		ptx.Close()
		return nil, nil, err
	}
	pty, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", id), os.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		ptx.Close()
		return nil, nil, err
	}
	return ptx, pty, nil
}
