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

package osutil

import (
	"fmt"
	"os"
	"strconv"
)

// ParsePermissions parses an octal string representing Unix file permissions
// into a os.FileMode value. If permissions is an empty string, defaultMode will
// be used instead.
func ParsePermissions(permissions string, defaultMode os.FileMode) (os.FileMode, error) {
	if permissions == "" {
		return defaultMode, nil
	}

	// Allow 777, 0777 or 1777, but never 77 or 1
	if len(permissions) != 3 && len(permissions) != 4 {
		return 0, fmt.Errorf("file permissions must be a 3- or 4-digit octal string, got %q", permissions)
	}

	v, err := strconv.ParseUint(permissions, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("file permissions must be a 3- or 4-digit octal string, got %q", permissions)
	}

	return os.FileMode(v), nil
}
