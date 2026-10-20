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
	"fmt"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

func getChildSubreaper() (bool, error) {
	var i uintptr
	err := unix.Prctl(unix.PR_GET_CHILD_SUBREAPER, uintptr(unsafe.Pointer(&i)), 0, 0, 0)
	if err != nil {
		return false, err
	}
	return i != 0, nil
}

func isZombie(pid int) (bool, error) {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return false, err
	}
	statFields := strings.Fields(string(stat))
	if len(statFields) < 3 {
		return false, fmt.Errorf("unexpected /proc/%d/stat content: %q", pid, stat)
	}
	return statFields[2] == "Z", nil
}
