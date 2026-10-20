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

package daemon

import "golang.org/x/sys/unix"

// rbAutoboot is RB_AUTOBOOT from <sys/reboot.h>. It is 0 on every
// BSD-derived system (FreeBSD, DragonFly, OpenBSD, NetBSD and Darwin) and is
// not exported by x/sys/unix.
const rbAutoboot = 0

// sysReboot restarts the system by invoking reboot(2) directly, as x/sys/unix
// has no Reboot wrapper for the BSDs. The system call number differs per OS,
// but unix.SYS_REBOOT is defined for all of them. The second argument is only
// meaningful on NetBSD (a bootstr for the boot loader); the other systems
// ignore it.
func sysReboot() error {
	_, _, errno := unix.Syscall(unix.SYS_REBOOT, rbAutoboot, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
