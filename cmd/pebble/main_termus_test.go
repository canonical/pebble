//go:build termus
// +build termus

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
	"os"

	. "gopkg.in/check.v1"

	pebble "github.com/canonical/pebble/cmd/pebble"
)

func (s *PebbleSuite) TestGetEnvPaths(c *C) {
	os.Setenv("PEBBLE", "")
	os.Setenv("PEBBLE_SOCKET", "")
	pebbleDir, socketPath := pebble.GetEnvPaths()
	c.Assert(pebbleDir, Equals, "/termus/var/lib/pebble")
	c.Assert(socketPath, Equals, "/termus/var/lib/pebble/.pebble.socket")

	os.Setenv("PEBBLE", "/foo")
	pebbleDir, socketPath = pebble.GetEnvPaths()
	c.Assert(pebbleDir, Equals, "/foo")
	c.Assert(socketPath, Equals, "/foo/.pebble.socket")

	os.Setenv("PEBBLE", "/bar")
	os.Setenv("PEBBLE_SOCKET", "/path/to/socket")
	pebbleDir, socketPath = pebble.GetEnvPaths()
	c.Assert(pebbleDir, Equals, "/bar")
	c.Assert(socketPath, Equals, "/path/to/socket")
}
