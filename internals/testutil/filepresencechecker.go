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

package testutil

import (
	"fmt"
	"os"

	"github.com/canonical/tc"
)

type filePresenceChecker struct {
	*tc.CheckerInfo
	present bool
}

// FilePresent verifies that the given file exists.
var FilePresent tc.Checker = &filePresenceChecker{
	CheckerInfo: &tc.CheckerInfo{Name: "FilePresent", Params: []string{"filename"}},
	present:     true,
}

// FileAbsent verifies that the given file does not exist.
var FileAbsent tc.Checker = &filePresenceChecker{
	CheckerInfo: &tc.CheckerInfo{Name: "FileAbsent", Params: []string{"filename"}},
	present:     false,
}

func (c *filePresenceChecker) Check(params []any, names []string) (result bool, error string) {
	filename, ok := params[0].(string)
	if !ok {
		return false, "filename must be a string"
	}
	_, err := os.Stat(filename)
	if os.IsNotExist(err) && c.present {
		return false, fmt.Sprintf("file %q is absent but should exist", filename)
	}
	if err == nil && !c.present {
		return false, fmt.Sprintf("file %q is present but should not exist", filename)
	}
	return true, ""
}
