// Copyright (c) 2023 Canonical Ltd
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

package main_test

import (
	"gopkg.in/check.v1"

	pebble "github.com/canonical/pebble/cmd/pebble"
)

func (s *PebbleSuite) TestBootFirmwareExtraArgs(c *check.C) {
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"boot-firmware", "extra", "args"})
	c.Assert(err, check.Equals, pebble.ErrExtraArgs)
	c.Assert(rest, check.HasLen, 1)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *PebbleSuite) TestBootFirmware(c *check.C) {
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"boot-firmware"})
	c.Assert(err, check.ErrorMatches, "cannot bootstrap an unsupported platform")
	c.Assert(rest, check.HasLen, 1)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}

func (s *PebbleSuite) TestBootFirmwareForce(c *check.C) {
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"boot-firmware", "--force"})
	c.Assert(err, check.ErrorMatches, "cannot bootstrap an unsupported platform")
	c.Assert(rest, check.HasLen, 1)
	c.Check(s.Stdout(), check.Equals, "")
	c.Check(s.Stderr(), check.Equals, "")
}
