//go:build !termus
// +build !termus

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

package boot

import "errors"

// CheckBootstrap validates the environment to ensure Bootstrap can be called.
func CheckBootstrap() error {
	return errors.New("cannot bootstrap an unsupported platform")
}

// Bootstrap prepares the environment in order to get the system in a working state.
func Bootstrap() error {
	return errors.New("cannot bootstrap an unsupported platform")
}
