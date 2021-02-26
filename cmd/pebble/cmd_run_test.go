// Copyright (c) 2021 Canonical Ltd
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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	pebble "github.com/canonical/pebble/cmd/pebble"
)

func (s *PebbleSuite) TestEnsurePebbleDir(c *C) {
	// Directory already exists and is accessible
	dir := c.MkDir()
	err := pebble.EnsurePebbleDir(dir)
	c.Assert(err, IsNil)

	// Path exists but is not a directory
	notADir := filepath.Join(dir, "file")
	err = ioutil.WriteFile(notADir, []byte{'.'}, 0644)
	c.Assert(err, IsNil)
	err = pebble.EnsurePebbleDir(notADir)
	c.Assert(err, ErrorMatches, `".*/file" is not a directory`)

	// Directory doesn't exist but is created
	dirNotExist := filepath.Join(dir, "sub")
	err = pebble.EnsurePebbleDir(dirNotExist)
	c.Assert(err, IsNil)
	st, err := os.Stat(dirNotExist)
	c.Assert(err, IsNil)
	c.Assert(st.IsDir(), Equals, true)

	// Permission denied
	err = pebble.EnsurePebbleDir("/cant/go/here")
	c.Assert(err, ErrorMatches, `cannot create pebble directory: mkdir .*: permission denied`)
}
