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
	"encoding/json"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestOkay(c *C) {
	oldPebbleDir := os.Getenv("PEBBLE")
	defer os.Setenv("PEBBLE", oldPebbleDir)

	tempDir := c.MkDir()
	filename := filepath.Join(tempDir, "notices.json")
	os.Setenv("PEBBLE", tempDir)

	data := []byte(`{"last-listed": "2023-09-06T15:06:00Z", "last-okayed": "0001-01-01T00:00:00Z"}`)
	err := os.WriteFile(filename, data, 0600)
	c.Assert(err, IsNil)

	rest, err := cli.Parser(cli.Client()).ParseArgs([]string{"okay"})
	c.Assert(err, IsNil)
	c.Check(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")

	data, err = os.ReadFile(filename)
	c.Assert(err, IsNil)
	var m map[string]any
	err = json.Unmarshal(data, &m)
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]any{
		"last-listed": "2023-09-06T15:06:00Z",
		"last-okayed": "2023-09-06T15:06:00Z",
	})
}

func (s *PebbleSuite) TestOkayNoNotices(c *C) {
	oldPebbleDir := os.Getenv("PEBBLE")
	defer os.Setenv("PEBBLE", oldPebbleDir)

	os.Setenv("PEBBLE", c.MkDir())

	_, err := cli.Parser(cli.Client()).ParseArgs([]string{"okay"})
	c.Assert(err, ErrorMatches, "no notices.* have been listed.*")
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}
