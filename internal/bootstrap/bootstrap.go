// Copyright (c) 2022 Canonical Ltd
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

package bootstrap

import (
	"errors"
	"os"
)

var osGetpid = os.Getpid

var commonMounts = []mount{
	{"procfs", "/proc", "proc", 0, ""},
	{"devtmpfs", "/dev", "devtmpfs", 0, ""},
	{"devpts", "/dev/pts", "devpts", 0, ""},
	{"sysfs", "/sys", "sysfs", 0, ""},
}

func mountCommon() error {
	for _, m := range commonMounts {
		if err := m.mount(); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks for the proper environment to ensure Bootstrap can be called.
func Validate() error {
	if osGetpid() != 1 {
		return errors.New(`must run as PID 1. Use --force to suppress this check`)
	}
	if v, ok := os.LookupEnv("TERMUS"); !ok || v != "1" {
		return errors.New(`TERMUS environment variable must be set to 1. Use --force to suppress this check`)
	}
	return nil
}

// Do prepares the environment in order to get the system in a working state.
func Do() error {
	if err := mountCommon(); err != nil {
		return err
	}
	return nil
}
