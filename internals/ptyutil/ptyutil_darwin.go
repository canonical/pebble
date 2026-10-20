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
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// openPtyPair opens a new pty master and its slave. On Darwin the master is
// the /dev/ptmx clone device, and the slave (/dev/ttysNNN) must be granted
// and unlocked before it can be opened, mirroring what libc's grantpt(3),
// unlockpt(3) and ptsname(3) do with the TIOCPTY* ioctls.
func openPtyPair() (*os.File, *os.File, error) {
	ptx, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}

	fd := ptx.Fd()
	for _, req := range []uint{unix.TIOCPTYGRANT, unix.TIOCPTYUNLK} {
		if _, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, uintptr(req), 0); errno != 0 {
			ptx.Close()
			return nil, nil, errno
		}
	}

	// TIOCPTYGNAME fills a 128-byte buffer with the slave's path.
	var name [128]byte
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, unix.TIOCPTYGNAME, uintptr(unsafe.Pointer(&name[0]))); errno != 0 {
		ptx.Close()
		return nil, nil, errno
	}

	pty, err := os.OpenFile(unix.ByteSliceToString(name[:]), os.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		ptx.Close()
		return nil, nil, err
	}
	return ptx, pty, nil
}
