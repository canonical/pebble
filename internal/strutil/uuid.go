// Copyright (c) 2014-2021 Canonical Ltd
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

package strutil

import (
	"crypto/rand"
	"fmt"
)

// UUID returns a randomly generated v4 UUID using crypto random sources.
func UUID() (string, error) {
	var r [16]byte
	_, err := rand.Read(r[:])
	if err != nil {
		return "", err
	}
	id := fmt.Sprintf("%x-%x-%x-%x-%x", r[:4], r[4:6], r[6:8], r[8:10], r[10:16])
	return id, nil
}
