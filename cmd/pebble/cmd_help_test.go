// Copyright (c) 2022 Canonical Ltd
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
	"github.com/jessevdk/go-flags"
	. "gopkg.in/check.v1"

	pebble "github.com/canonical/pebble/cmd/pebble"
)

func (s *PebbleSuite) TestHelpCommandWorks(c *C) {
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"help", "add"})

	flagsErr, ok := err.(*flags.Error)
	c.Assert(ok, Equals, true)
	c.Assert(flagsErr.Type, Equals, flags.ErrHelp)

	c.Assert(rest, HasLen, 0)
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestHelpOptionWorks(c *C) {
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"add", "--help"})

	flagsErr, ok := err.(*flags.Error)
	c.Assert(ok, Equals, true)
	c.Assert(flagsErr.Type, Equals, flags.ErrHelp)

	c.Assert(rest, HasLen, 0)
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestHelpHelpWorks(c *C) {
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"help", "--help"})

	flagsErr, ok := err.(*flags.Error)
	c.Assert(ok, Equals, true)
	c.Assert(flagsErr.Type, Equals, flags.ErrHelp)

	c.Assert(rest, HasLen, 0)
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestHelpAllWorks(c *C) {
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"help", "--all"})
	c.Assert(err, Equals, nil)
	c.Assert(rest, HasLen, 0)
	c.Assert(s.Stdout(), Not(Equals), "")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestHelpManWorks(c *C) {
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"help", "--man"})
	c.Assert(err, Equals, nil)
	c.Assert(rest, HasLen, 0)
	c.Assert(s.Stdout(), Not(Equals), "")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestHelpCommandFailsCommandRequired(c *C) {
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"help"})

	flagsErr, ok := err.(*flags.Error)
	c.Assert(ok, Equals, true)
	c.Assert(flagsErr.Type, Equals, flags.ErrCommandRequired)

	c.Assert(rest, HasLen, 1)
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestHelpOptionFailsCommandRequired(c *C) {
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"--help"})

	flagsErr, ok := err.(*flags.Error)
	c.Assert(ok, Equals, true)
	c.Assert(flagsErr.Type, Equals, flags.ErrCommandRequired)

	c.Assert(rest, HasLen, 1)
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestHelpFailsUnknownCommand(c *C) {
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"help", "foo"})
	c.Assert(err, ErrorMatches, `unknown command "foo", see 'pebble help'.`)
	c.Assert(rest, HasLen, 1)
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestHelpFailsBothCommandAndAll(c *C) {
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"help", "foo", "--all"})
	c.Assert(err, ErrorMatches, `help accepts a command, or '--all', but not both.`)
	c.Assert(rest, HasLen, 1)
	c.Assert(s.Stdout(), Equals, "")
	c.Assert(s.Stderr(), Equals, "")
}
