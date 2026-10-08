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

package daemon

import "golang.org/x/sys/unix"

// RB_AUTOBOOT from <sys/reboot.h>; not exported by x/sys/unix.
const rebootCmdRestart = 0

func sysReboot(cmd int) error {
	_, _, errno := unix.Syscall(unix.SYS_REBOOT, uintptr(cmd), 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
