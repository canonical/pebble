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

package main_test

import (
	"fmt"
	"net/http"

	. "gopkg.in/check.v1"

	pebble "github.com/canonical/pebble/cmd/pebble"
)

func (s *PebbleSuite) TestVersion(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"type":"sync","status-code":200,"status":"OK","result":{"version":"7.89"}}`)
	})

	restore := fakeVersion("4.56")
	defer restore()

	_, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"version"})
	c.Assert(err, IsNil)
	c.Check(s.Stdout(), Equals, "client  4.56\nserver  7.89\n")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestVersionExtraArgs(c *C) {
	rest, err := pebble.Parser(pebble.Client()).ParseArgs([]string{"version", "extra", "args"})
	c.Assert(err, Equals, pebble.ErrExtraArgs)
	c.Assert(rest, HasLen, 1)
}
