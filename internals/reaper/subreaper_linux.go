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

package reaper

import (
	"errors"

	"golang.org/x/sys/unix"
)

// setChildSubreaper sets the "child subreaper" attribute of the current
// process, turning it on if the argument is nonzero, off otherwise.
//
// If turning it on, we become the parent of dead child processes rather than
// PID 1. This allows us to wait for processes that are started by a Pebble
// service but then die, to "reap" them (see
// https://unix.stackexchange.com/a/250156/73491).
func setChildSubreaper(set int) error {
	err := unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, uintptr(set), 0, 0, 0)
	if err == unix.EINVAL {
		// Not available in kernels before Linux 3.4.
		return errors.New("child subreaping unavailable on this platform")
	}
	return err
}
