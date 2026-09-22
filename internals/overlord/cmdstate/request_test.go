// Copyright (c) 2026 Canonical Ltd
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

package cmdstate_test

import (
	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/cmdstate"
	"github.com/canonical/pebble/internals/overlord/state"
)

type requestSuite struct{}

var _ = Suite(&requestSuite{})

func (s *requestSuite) TestExecEmptyCommand(c *C) {
	st := state.New(nil)

	task, _, err := cmdstate.Exec(st, &cmdstate.ExecArgs{})

	c.Check(task, IsNil)
	c.Check(err, ErrorMatches, "cannot execute command: command cannot be empty")
}
