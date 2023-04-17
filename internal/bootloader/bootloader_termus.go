//go:build termus
// +build termus

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

package bootloader

import (
	"errors"
)

// BootloaderMountpoint is the path where the root directory for the current
// bootloader configuration is mounted on bootstrap.
const bootloaderMountpoint = "/var/termus/boot"

type bootloaderNewFunc func(rootdir string) Bootloader

// bootloaders list all possible bootloaders by their constructor
// function.
var bootloaders = []bootloaderNewFunc{
	newGrub,
}

// Find obtains an instance of the first supported bootloader that is available
// on the system.
func Find() (Bootloader, error) {
	for _, newFunc := range bootloaders {
		bl := newFunc(bootloaderMountpoint)
		if err := bl.Find(); err == nil {
			return bl, nil
		}
	}
	return nil, errors.New("cannot determine bootloader")
}
