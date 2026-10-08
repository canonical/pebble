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

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

func getPeerCred(fd int) (*ucred, error) {
	cred, err := unix.GetsockoptXucred(fd, unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	if err != nil {
		return nil, err
	}
	// Since FreeBSD 13, struct xucred ends with a pointer-sized union holding
	// cr_pid. x/sys/unix hides that field behind an unnamed member, so read it
	// by offset from the end of the struct.
	pid := *(*int32)(unsafe.Add(unsafe.Pointer(cred), unsafe.Sizeof(*cred)-unsafe.Sizeof(uintptr(0))))
	return &ucred{Pid: pid, Uid: cred.Uid}, nil
}
