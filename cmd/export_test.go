// Copyright (c) 2014-2023 Canonical Ltd
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

package cmd

import (
	"sync"
)

// MockPid2ProcPath assigns a temporary path to where the PID2
// status can be found.
func MockPid2ProcPath(path string) (restore func()) {
	orig := pid2ProcPath
	pid2ProcPath = path
	return func() { pid2ProcPath = orig }
}

// MockPid allows faking the pid of this process
func MockPid(pid int) (restore func()) {
	orig := selfPid
	selfPid = pid
	return func() { selfPid = orig }
}

// MockVersion allows mocking the version which would
// otherwise only be real once the generator script
// has run.
func MockVersion(version string) (restore func()) {
	old := Version
	Version = version
	return func() { Version = old }
}

// ResetContainerInit forces the container runtime check
// to retry with globals reset
func ResetContainerInit() {
	containerOnce = sync.Once{}
	containerRuntime = true
}
