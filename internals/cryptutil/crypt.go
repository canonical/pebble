// Copyright (c) 2025 Canonical Ltd
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

package cryptutil

import (
	"fmt"
	"strings"
)

// Verify returns a nil error if the provided salted hashed key was derived from
// the provided key.
// This library only support crypt using sha512. When compiled with CGO, libcrypt
// is used.
func Verify(hashedKey string, key string) error {
	if !strings.HasPrefix(hashedKey, "$6$") {
		return fmt.Errorf("unsupported key type")
	}
	return crypt6_verify(hashedKey, key)
}
