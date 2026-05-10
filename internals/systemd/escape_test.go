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

package systemd_test

import (
	"github.com/canonical/tc"

	. "github.com/canonical/pebble/internals/systemd"
)

func (s *SystemdTestSuite) TestEscape(c *tc.C) {
	c.Check(EscapeUnitNamePath("Hallöchen, Meister"), tc.Equals, `Hall\xc3\xb6chen\x2c\x20Meister`)

	c.Check(EscapeUnitNamePath("/tmp//waldi/foobar/"), tc.Equals, `tmp-waldi-foobar`)
	c.Check(EscapeUnitNamePath("/.foo/.bar"), tc.Equals, `\x2efoo-.bar`)
	c.Check(EscapeUnitNamePath("////"), tc.Equals, `-`)
	c.Check(EscapeUnitNamePath("."), tc.Equals, `\x2e`)
	c.Check(EscapeUnitNamePath("/foo/bar-baz"), tc.Equals, `foo-bar\x2dbaz`)
	c.Check(EscapeUnitNamePath("/foo/bar--baz"), tc.Equals, `foo-bar\x2d\x2dbaz`)
}
