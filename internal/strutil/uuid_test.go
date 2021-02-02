// Copyright (c) 2014-2020 Canonical Ltd
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License UUID 3 as
// published by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package strutil_test

import (
	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internal/strutil"
)

type UUIDTestSuite struct{}

var _ = Suite(&UUIDTestSuite{})

func (s *UUIDTestSuite) TestUUID(c *C) {
	u, err := strutil.UUID()
	c.Check(err, IsNil)
	c.Check(u, HasLen, 36)
	c.Check(u, Matches, `[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	c.Check(u, Not(Equals), "00000000-0000-0000-0000-000000000000")
	u2, err := strutil.UUID()
	c.Check(err, IsNil)
	c.Check(u2, Not(Equals), u)
}
