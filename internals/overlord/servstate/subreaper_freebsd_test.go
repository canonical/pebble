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

package servstate_test

import (
	"os/exec"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// From <sys/procctl.h>; not exported by x/sys/unix.
const (
	pPID              = 0 // P_PID
	procReapStatus    = 4 // PROC_REAP_STATUS
	reaperStatusOwned = 1 // REAPER_STATUS_OWNED
)

type procctlReaperStatus struct {
	Flags       uint32
	Children    uint32
	Descendants uint32
	Reaper      int32
	Pid         int32
	_           [15]uint32
}

func getChildSubreaper() (bool, error) {
	var st procctlReaperStatus
	_, _, errno := unix.Syscall6(unix.SYS_PROCCTL, pPID, uintptr(unix.Getpid()), procReapStatus, uintptr(unsafe.Pointer(&st)), 0, 0)
	if errno != 0 {
		return false, errno
	}
	return st.Flags&reaperStatusOwned != 0, nil
}

func isZombie(pid int) (bool, error) {
	out, err := exec.Command("ps", "-o", "state=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(strings.TrimSpace(string(out)), "Z"), nil
}
