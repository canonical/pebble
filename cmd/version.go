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

import _ "embed"

//go:generate ./mkversion.sh

// Version is the Pebble version.
//
//go:embed VERSION
var Version string

// MockVersion temporarily changes Version and returns a function to restore it.
func MockVersion(version string) (restore func()) {
	old := Version
	Version = version
	return func() { Version = old }
}
