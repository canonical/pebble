// Copyright (c) 2023 Canonical Ltd
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
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"sync"
)

var (
	selfPid = os.Getpid()

	pid2ProcPath   = "/proc/2/status"
	rockPath       = "/.rock/metadata.yaml"
	lxdPath        = "/proc/1/environ"
	dockerEnvPath  = "/.dockerenv"
	dockerInitPath = "/.dockerinit"

	// Confined may be initialised by a binary derived from
	// from this repository. This provides an override mechanism
	// to bypass (speed up) detection if it is not needed.
	Confined *bool

	once    sync.Once
	failure error
)

// IsInit returns true if the system manager is the first process
// started by the Linux kernel.
func IsInit() bool {
	return selfPid == 1
}

// IsConfined works out if we are running inside a container.
// If the error is set, the detection result is meaningless.
func IsConfined() (bool, error) {
	// This will not only force a single detection, but also block additional
	// concurrent calls until the primary is complete.
	once.Do(checks)

	if Confined == nil {
		failure = fmt.Errorf("confined state was globally set to nil")
	}

	if failure != nil {
		return false, failure
	}

	return *Confined, nil
}

// checks is a curated list of checks for speedy detection of
// confined environments. The quickest most obvious checks should be
// performed first. The list is iterated until a confinement check returns
// true. If all the checks fail to detect a confined runtime, we can
// assume its a unconfined virtual/real machine.
//
// If the system manager is used as library for derived projects where
// its certain that no confinement exist, use the Confined global to
// bypass this check.
func checks() {
	var res bool
	var err error

	// Was Confined globally set already?
	if Confined != nil {
		return
	}

	// If any check encounters an error, or if we reach a
	// conclusion, we update the globals and return.
	defer func() {
		Confined = &res
		failure = err
	}()

	for _, c := range checkList {
		res, err = c()
		if err != nil || res {
			return
		}
	}
}

var checkList = []func() (bool, error){
	isRock,
	isLxd,
	isDocker,
	noKernel,
}

// isRock checks if it can access /.meta/metadata.yaml.
func isRock() (bool, error) {
	_, err := os.Stat(rockPath)
	if err == nil {
		return true, nil
	} else if os.IsNotExist(err) {
		return false, nil
	} else {
		return false, fmt.Errorf("rock detection file stat returned an error")
	}
}

// isLxd checks if /proc/1/environ contains the "container=xxx" variable. This
// check should work for OCI compliant images in general.
func isLxd() (bool, error) {
	_, err := os.Stat(lxdPath)
	if err == nil {
		s, err := ioutil.ReadFile(lxdPath)
		if err != nil {
			return false, err
		} else {
			lines := strings.Split(string(s), "\000")
			for _, l := range lines {
				kv := strings.Split(l, "=")
				if kv[0] == "container" {
					return true, nil
				}
			}
		}
		return false, nil
	} else if os.IsNotExist(err) {
		return false, nil
	} else {
		return false, fmt.Errorf("lxd/oci detection file stat returned an error")
	}
}

// isDocker checks for /.dockerenv or /.dockerinit
func isDocker() (bool, error) {
	_, err1 := os.Stat(dockerInitPath)
	_, err2 := os.Stat(dockerEnvPath)
	if err1 == nil || err2 == nil {
		return true, nil
	} else if os.IsNotExist(err1) && os.IsNotExist(err2) {
		return false, nil
	} else {
		return false, fmt.Errorf("docker detection file stat returned an error")
	}
}

// noKernel returns true if a kernel is not visible. The check will inspect
// the PPID of PID2 if it exists. If the PPID is zero its kernel owned, which
// strongly suggests we have complete PID visibility, and not confined.
//
// This check can be used to confirm the service manager is run inside of
// a container runtime. The following two known situations will result in
// invalid results:
//
//  1. If /proc is not mounted, it will return true
//  2. If docker passes through host pids, "docker run --pid host", it will
//     detect the kernel, even though its inside a container.
//
// This is used as a last best effort test for container runtime cases not
// picked up by earlier tests. It is also very useful to verify that indeed
// the environment appears like a normal machine with unconfined access, as
// this is what the assumption will be.
func noKernel() (bool, error) {
	// This path may not exist in a specific userspace, so we
	// will not report any file not found errors.
	s, err := ioutil.ReadFile(pid2ProcPath)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	} else if err == nil {
		lines := strings.Split(string(s), "\n")
		for _, l := range lines {
			kv := strings.Split(l, "\t")
			if len(kv) == 2 && kv[0] == "PPid:" {
				ppid, err := strconv.Atoi(kv[1])
				if err != nil {
					return false, err
				}
				if ppid == 0 {
					return false, nil
				}
			}
		}
	}
	return true, nil
}
