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

//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package cmdstate

import (
	"time"

	"golang.org/x/sys/unix"
)

// waitPtyDrained waits (for a bounded time) until no output is left buffered
// in the pty whose master side is masterFd, that is, until the goroutine
// mirroring the master has read everything the child wrote.
//
// Unlike Linux, the BSD-derived tty layers (FreeBSD, DragonFly, OpenBSD,
// NetBSD and Darwin's XNU) flush the output queue when the last descriptor
// for a pty's slave side is closed, discarding anything the master has not
// yet read. So this must be called before closing our copy of the slave once
// the child has exited.
func waitPtyDrained(masterFd int) {
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		fds := []unix.PollFd{{Fd: int32(masterFd), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, 0)
		if err == unix.EINTR {
			continue
		}
		// POLLHUP means the slave side is already gone (for example, the
		// controlling terminal was revoked when the session leader exited),
		// in which case POLLIN is always reported and nothing can be read.
		if err != nil || n == 0 || fds[0].Revents&unix.POLLIN == 0 || fds[0].Revents&unix.POLLHUP != 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
}
