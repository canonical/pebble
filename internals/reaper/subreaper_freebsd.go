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

package reaper

import (
	"golang.org/x/sys/unix"
)

// Constants from <sys/procctl.h> and <sys/wait.h>; not exported by x/sys/unix.
const (
	procReapAcquire = 2 // PROC_REAP_ACQUIRE
	procReapRelease = 3 // PROC_REAP_RELEASE
	pPID            = 0 // P_PID
)

// setChildSubreaper sets the "reaper" attribute of the current process,
// turning it on if the argument is nonzero, off otherwise. This is the
// FreeBSD equivalent of Linux's PR_SET_CHILD_SUBREAPER: orphaned descendants
// are reparented to us rather than to PID 1, so we can wait for them.
func setChildSubreaper(set int) error {
	cmd := procReapRelease
	if set != 0 {
		cmd = procReapAcquire
	}
	_, _, errno := unix.Syscall6(unix.SYS_PROCCTL, pPID, uintptr(unix.Getpid()), uintptr(cmd), 0, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
