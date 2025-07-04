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

//go:build !(cgo && linux)

package cryptutil

import "github.com/GehirnInc/crypt/sha512_crypt"

func crypt6_verify(hashedKey string, key string) error {
	crypt := sha512_crypt.New()
	return crypt.Verify(hashedKey, []byte(key))
}
