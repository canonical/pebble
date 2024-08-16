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

package cli_test

import (
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestOkay(c *C) {
	s.writeCLIState(c, map[string]any{
		"notices-last-listed": time.Date(2023, 9, 6, 15, 6, 0, 0, time.UTC),
		"notices-last-okayed": time.Time{},
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"okay"})
	c.Assert(err, IsNil)
	c.Check(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")

	cliState := s.readNoticesCLIState(c)
	c.Check(cliState, DeepEquals, map[string]any{
		"notices-last-listed": "2023-09-06T15:06:00Z",
		"notices-last-okayed": "2023-09-06T15:06:00Z",
	})
}

func (s *PebbleSuite) TestOkayWarnings(c *C) {
	s.writeCLIState(c, map[string]any{
		"notices-last-listed":  time.Date(2023, 9, 6, 15, 6, 0, 0, time.UTC),
		"notices-last-okayed":  time.Time{},
		"warnings-last-listed": time.Date(2024, 9, 6, 15, 6, 0, 0, time.UTC),
		"warnings-last-okayed": time.Time{},
	})

	rest, err := cli.ParserForTest().ParseArgs([]string{"okay", "--warnings"})
	c.Assert(err, IsNil)
	c.Check(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")

	cliState := s.readCLIState(c)
	c.Check(cliState, DeepEquals, map[string]any{
		"notices-last-listed":  "2023-09-06T15:06:00Z",
		"notices-last-okayed":  "0001-01-01T00:00:00Z",
		"warnings-last-listed": "2024-09-06T15:06:00Z",
		"warnings-last-okayed": "2024-09-06T15:06:00Z",
	})
}

func (s *PebbleSuite) TestOkayNoNotices(c *C) {
	_, err := cli.ParserForTest().ParseArgs([]string{"okay"})
	c.Assert(err, ErrorMatches, "no notices.* have been listed.*")
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}
