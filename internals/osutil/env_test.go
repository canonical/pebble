// Copyright (c) 2014-2023 Canonical Ltd
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

package osutil_test

import (
	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/osutil"
)

type envSuite struct{}

var _ = Suite(&envSuite{})

func (s *envSuite) TestEnviron(c *C) {
	restore := osutil.FakeEnviron(func() []string {
		return []string{"FOO=bar", "BAR=", "TEMP"}
	})
	defer restore()

	env := osutil.Environ()

	c.Assert(len(env), Equals, 3)
	c.Assert(env, DeepEquals, map[string]string{
		"FOO":  "bar",
		"BAR":  "",
		"TEMP": "",
	})
}
