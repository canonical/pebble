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
	"fmt"
)

// Bootloader provides primitives to retain state across boots and handle
// boot slots.
type Bootloader interface {
	// Name returns the bootloader name.
	Name() string

	// Present returns whether the bootloader is currently present on the
	// system--in other words, whether this bootloader has been installed to
	// the current system. Implementations should only return non-nil error if
	// they can positively identify that the bootloader is installed, but there
	// is actually an error with the installation.
	Present() (bool, error)

	// ActiveSlot obtains the label of the currently booted slot.
	ActiveSlot() string

	// SetActiveSlot instructs the bootloader to select the slot with the
	// specified label on the next reboot.
	SetActiveSlot(label string) error

	// Status obtains the status of the slot with the specified label.
	// If there is no saved status for the slot, or if the saved status is not
	// any of Unbootable, Try or Fail, Try will be returned.
	Status(label string) Status
}

// Status represents the conditions in which a boot attempt was made.
type Status string

const (
	// Unbootable indicates that the slot cannot be booted from in any case.
	// For example, this slot might be empty.
	Unbootable Status = "unbootable"

	// Try indicates that the slot can potentially be booted from.
	Try Status = "try"

	// Fail indicates that there was a problem preventing a slot from being
	// booted from.
	Fail Status = "fail"
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
func Find() (*Bootloader, error) {
	for _, newFunc := range bootloaders {
		bl := newFunc(bootloaderMountpoint)
		isPresent, err := bl.Present()
		if err != nil {
			return nil, fmt.Errorf("bootloader %q found but not usable: %w", bl.Name(), err)
		}
		if isPresent {
			return &bl, nil
		}
	}
	return nil, errors.New("cannot determine bootloader")
}
