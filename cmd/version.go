// Copyright (c) 2014-2020 Canonical Ltd
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

//go:generate ./mkversion.sh

import (
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"sync"
)

var (
	// Version will be overwritten at build-time via mkversion.sh
	Version = "unknown"

	pid2ProcPath = "/proc/2/status"
	selfPid      = os.Getpid()

	containerOnce    sync.Once
	containerRuntime bool = true
)

// Containerised returns true if we are running inside a container runtime
// such as lxd or Docker. The detection is only intended for Linux host
// environments as it looks for the Linux kernel kthreadd process, started
// by the Linux kernel after init. Kernel initialised processes do not have
// a parent as its not a child of init (PID1) and as a result has its ppid
// set to zero. We use this property to detect the absence of a container
// runtime. If /proc is not mounted, the detection will assume its a
// container, so make sure early mounts are completed before running this
// check on an unconfined host.
func Containerised() bool {
	containerOnce.Do(func() {
		if s, err := ioutil.ReadFile(pid2ProcPath); err == nil {
			lines := strings.Split(string(s), "\n")
			for _, l := range lines {
				kv := strings.Split(l, "\t")
				if zeroPPid(kv) {
					containerRuntime = false
					break
				}
			}
		}
	})

	return containerRuntime
}

// zeroPPid returns true if a PPid key with a zero value was found,
// otherwise false.
func zeroPPid(kv []string) (found bool) {
	if kv[0] == "PPid:" {
		if ppid, err := strconv.Atoi(kv[1]); err == nil && ppid == 0 {
			found = true
		}
	}
	return found
}

// InitProcess returns true if the system manager is the first process
// started by the Linux kernel.
func InitProcess() bool {
	return selfPid == 1
}
