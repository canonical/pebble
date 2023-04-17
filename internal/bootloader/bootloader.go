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

import "errors"

var ErrNoBootloader = errors.New("bootloader not present")

// Bootloader provides primitives to retain state across boots and handle
// boot slots.
type Bootloader interface {
	// Name returns the bootloader name.
	Name() string

	// Find attempts to locate this bootloader in the running system.
	// If the bootloader cannot be located, an error will be returned.
	Find() error

	// ActiveSlot obtains the label of the currently booted slot.
	ActiveSlot() string

	// SetActiveSlot instructs the bootloader to select the slot with the
	// specified label on the next reboot.
	SetActiveSlot(label string) error
}
