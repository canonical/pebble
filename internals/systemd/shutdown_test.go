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
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/reaper"
	"github.com/canonical/pebble/internals/systemd"
	"github.com/canonical/pebble/internals/testutil"
)

type shutdownTestSuite struct{}

var _ = Suite(&shutdownTestSuite{})

func (s *shutdownTestSuite) SetUpTest(c *C) {
	// Needed for testutil.exec
	reaper.Start()
}

func (s *shutdownTestSuite) TearDownTest(c *C) {
	reaper.Stop()
}

// TestReboot checks that command construction match the
// expectation of the systemd shutdown command.
func (s *shutdownTestSuite) TestReboot(c *C) {
	cmd := testutil.FakeCommand(c, "shutdown", "", true)
	defer cmd.Restore()

	tests := []struct {
		delay    time.Duration
		delayArg string
		msg      string
	}{
		{0, "+0", ""},
		{0, "+0", "some msg"},
		{-1, "+0", "some msg"},
		{time.Minute, "+1", "some msg"},
		{10 * time.Minute, "+10", "some msg"},
		{30 * time.Second, "+0", "some msg"},
	}

	for _, t := range tests {
		err := systemd.Shutdown.Reboot(t.delay, t.msg)
		c.Assert(err, IsNil)
		c.Check(cmd.Calls(), DeepEquals, [][]string{
			{"shutdown", "-r", t.delayArg, t.msg},
		})

		cmd.ForgetCalls()
	}
}
