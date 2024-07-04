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

package client_test

import (
	"time"

	"gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
)

// Pebble has never produced warnings (and now it can't), so these tests
// basically do nothing.

func (cs *clientSuite) TestWarningsAll(c *check.C) {
	warnings, err := cs.cli.Warnings(client.WarningsOptions{All: true})
	c.Assert(err, check.IsNil)
	c.Assert(warnings, check.HasLen, 0)
}

func (cs *clientSuite) TestWarnings(c *check.C) {
	warnings, err := cs.cli.Warnings(client.WarningsOptions{})
	c.Assert(err, check.IsNil)
	c.Assert(warnings, check.HasLen, 0)
}

func (cs *clientSuite) TestOkay(c *check.C) {
	err := cs.cli.Okay(time.Now())
	c.Assert(err, check.IsNil)
}
