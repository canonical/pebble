// Copyright (C) 2020 Canonical Ltd
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

package quantity

import "fmt"

// Size describes the size in bytes.
type Size uint64

const (
	// SizeKiB is the byte size of one kibibyte (2^10 = 1024 bytes)
	SizeKiB = Size(1 << 10)
	// SizeMiB is the size of one mebibyte (2^20)
	SizeMiB = Size(1 << 20)
	// SizeGiB is the size of one gibibyte (2^30)
	SizeGiB = Size(1 << 30)
)

func (s *Size) String() string {
	if s == nil {
		return "unspecified"
	}
	return fmt.Sprintf("%d", *s)
}
