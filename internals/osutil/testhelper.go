// Copyright (C) 2026 Canonical Ltd
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
	"os"
	"regexp"
)

// Match binary names from both go test and dlv test.
var goTestExeRe = regexp.MustCompile(`^.*/(.*go-build.*/.*\.test|debug\.test\d+)$`)

// IsTestBinary checks whether the current process is a go test binary.
func IsTestBinary() bool {
	return len(os.Args) > 0 && goTestExeRe.MatchString(os.Args[0])
}
