// Copyright (c) 2014-2023 Canonical Ltd
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
	"strings"
)

var osEnviron = os.Environ

// Environ returns a map representing the environment.
// It converts the slice from os.Environ() into a map.
func Environ() map[string]string {
	env := make(map[string]string)
	for _, kv := range osEnviron() {
		parts := strings.SplitN(kv, "=", 2)
		key := parts[0]
		val := ""
		if len(parts) == 2 {
			val = parts[1]
		}
		env[key] = val
	}
	return env
}
