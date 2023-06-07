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

package cli_test

import (
	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestHelpCommand(c *C) {
	restore := fakeArgs("pebble", "help")
	defer restore()

	err := cli.RunMain()
	c.Assert(err, Equals, nil)
	c.Check(s.Stdout(), Matches, "(?s)Pebble lets you control services.*Commands can be classified as follows.*")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestHelpAll(c *C) {
	restore := fakeArgs("pebble", "help", "--all")
	defer restore()

	err := cli.RunMain()
	c.Assert(err, Equals, nil)
	c.Check(s.Stdout(), Matches, "(?s)Pebble lets you control services.*run.*help.*version.*warnings.*")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestHelpAllWithCommand(c *C) {
	restore := fakeArgs("pebble", "help", "help", "--all")
	defer restore()

	err := cli.RunMain()
	c.Assert(err, ErrorMatches, `help accepts a command, or '--all', but not both.`)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestHelpMan(c *C) {
	restore := fakeArgs("pebble", "help", "--man")
	defer restore()

	err := cli.RunMain()
	c.Assert(err, Equals, nil)
	c.Check(s.Stdout(), Matches, `(?s)\.TH.*\.SH NAME.*pebble \\- Tool to interact with pebble.*`)
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestHelpOption(c *C) {
	restore := fakeArgs("pebble", "--help")
	defer restore()

	err := cli.RunMain()
	c.Assert(err, Equals, nil)
	c.Check(s.Stdout(), Matches, "(?s)Pebble lets you control services.*Commands can be classified as follows.*")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestHelpWithCommand(c *C) {
	restore := fakeArgs("pebble", "help", "help")
	defer restore()

	err := cli.RunMain()
	c.Assert(err, Equals, nil)
	c.Check(s.Stdout(), Matches, "(?s)Usage.*pebble help.*The help command.*help command options.*")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestHelpWithUnknownCommand(c *C) {
	restore := fakeArgs("pebble", "help", "dachshund")
	defer restore()

	err := cli.RunMain()
	c.Assert(err, ErrorMatches, `unknown command "dachshund", see 'pebble help'.`)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestHelpWithUnknownSubcommand(c *C) {
	restore := fakeArgs("pebble", "help", "add", "dachshund")
	defer restore()

	err := cli.RunMain()
	c.Assert(err, ErrorMatches, `unknown command "dachshund", see 'pebble help add'.`)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestCommandWithHelpOption(c *C) {
	restore := fakeArgs("pebble", "help", "--help")
	defer restore()

	err := cli.RunMain()
	c.Assert(err, Equals, nil)
	c.Check(s.Stdout(), Matches, "(?s)Usage.*pebble help.*The help command.*help command options.*")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestAddHelpCategory(c *C) {
	restore := fakeArgs("pebble")
	defer restore()

	cli.HelpCategories = append(cli.HelpCategories, cli.HelpCategory{
		Label:       "Test category",
		Description: "Test description",
		Commands:    []string{"run", "logs"},
	})

	err := cli.RunMain()
	c.Assert(err, Equals, nil)

	c.Check(s.Stdout(), Matches, "(?s).*Test category: run, logs\n.*")
	c.Check(s.Stderr(), Equals, "")
}
