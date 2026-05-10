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

package overlord_test

// test the various managers and their operation together through overlord

import (
	"testing"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/overlord"
	"github.com/canonical/pebble/internals/testutil"
)

type mgrsSuite struct {
	testutil.BaseTest

	dir string

	o *overlord.Overlord
}

func TestMgrsSuite(t *testing.T) {
	tc.Run(t, &mgrsSuite{})
}

func (s *mgrsSuite) SetUpTest(c *tc.C) {
	s.BaseTest.SetUpTest(c)

	s.dir = c.MkDir()

	o, err := overlord.New(&overlord.Options{PebbleDir: s.dir})
	c.Assert(err, tc.IsNil)
	err = o.StartUp()
	c.Assert(err, tc.IsNil)
	s.o = o
}
