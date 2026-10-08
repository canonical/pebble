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

package osutil

import (
	"fmt"
	"os"
)

// FakeMountInfo fakes content of /proc/self/mountinfo.
func FakeMountInfo(text string) (restore func()) {
	old := procSelfMountInfo
	f, err := os.CreateTemp("", "mountinfo")
	if err != nil {
		panic(fmt.Errorf("cannot open temporary file: %s", err))
	}
	if err := os.WriteFile(f.Name(), []byte(text), 0644); err != nil {
		panic(fmt.Errorf("cannot write mock mountinfo file: %s", err))
	}
	procSelfMountInfo = f.Name()
	return func() {
		os.Remove(f.Name())
		procSelfMountInfo = old
	}
}
