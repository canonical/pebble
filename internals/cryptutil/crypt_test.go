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
	. "gopkg.in/check.v1"
)

type cryptSuite struct{}

var _ = Suite(&cryptSuite{})

func (s *cryptSuite) TestVerifyPasswd6(c *C) {
	err := Verify(
		"$6$PyHGCImoZPK/0kyz$L8H4NZq1TfrbSHDDdeSIFYXNMqIZrEqcRty2jU2IXDk5pbzJKwUNPrk/SoVdgeRi9PBT4AqpX3QMQPbguhBNu1",
		"a",
	)
	c.Assert(err, IsNil)
}

func (s *cryptSuite) TestVerifyPasswd6FailsWithBadKey(c *C) {
	err := Verify(
		"$6$PyHGCImoZPK/0kyz$L8H4NZq1TfrbSHDDdeSIFYXNMqIZrEqcRty2jU2IXDk5pbzJKwUNPrk/SoVdgeRi9PBT4AqpX3QMQPbguhBNu1",
		"b",
	)
	c.Assert(err, NotNil)
}

func (s *cryptSuite) TestVerifyPasswd6WithRounds(c *C) {
	err := Verify(
		"$6$rounds=1000$PyHGCImoZPK/0kyz$nuqrhBzhXojF6/SjS6vBn1OThCl5U8dIhYA4EVtkzVDiFdrCjRTt7YM9bAm8tsQyN8ywWZPq1qYNJDcnF0MCZ/",
		"a",
	)
	c.Assert(err, IsNil)
}

func (s *cryptSuite) TestVerifyPasswd6WithIllformedSalt(c *C) {
	err := Verify(
		"$6$rounds=1000$PyHGCImoZPK/0kyznuqrhBzhXojF6/SjS6vBn1OThCl5U8dIhYA4EVtkzVDiFdrCjRTt7YM9bAm8tsQyN8ywWZPq1qYNJDcnF0MCZ/",
		"a",
	)
	c.Assert(err, NotNil)
}

func (s *cryptSuite) TestVerifyPasswd5Unsupported(c *C) {
	err := Verify(
		"$5$ClNNHz7FMy8Wzf7.$Ca85oYLoUPTM1jjKyQCnS1eq/DCY4IQ.WIRqqgwE9a4",
		"a",
	)
	c.Assert(err, ErrorMatches, `unsupported key type`)
}

func (s *cryptSuite) TestVerifyPasswd1Unsupported(c *C) {
	err := Verify(
		"$1$MgYHak8b$HJxCvRizgEnBZDyaHFmh//",
		"a",
	)
	c.Assert(err, ErrorMatches, `unsupported key type`)
}

func (s *cryptSuite) TestVerifyPasswdApr1Unsupported(c *C) {
	err := Verify(
		"$apr1$0z.oA5jA$6v/W4LrAb95IipZY.SueG0",
		"a",
	)
	c.Assert(err, ErrorMatches, `unsupported key type`)
}

func (s *cryptSuite) TestVerifyPasswdAixMD5Unsupported(c *C) {
	err := Verify(
		"0AsejNrv$.XgG72eDqFhZzpp7K66uX1",
		"a",
	)
	c.Assert(err, ErrorMatches, `unsupported key type`)
}
