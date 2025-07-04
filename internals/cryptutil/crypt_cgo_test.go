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

//go:build cgo && linux

package cryptutil

import (
	. "gopkg.in/check.v1"
)

func (s *cryptSuite) TestLibcryptVersionCalled(c *C) {
	cryptNotCalled.Store(true)
	err := Verify(
		"$6$PyHGCImoZPK/0kyz$L8H4NZq1TfrbSHDDdeSIFYXNMqIZrEqcRty2jU2IXDk5pbzJKwUNPrk/SoVdgeRi9PBT4AqpX3QMQPbguhBNu1",
		"a",
	)
	c.Assert(err, IsNil)
	c.Assert(cryptNotCalled.Load(), Equals, false)
}
