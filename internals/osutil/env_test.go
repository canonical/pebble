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
	"testing"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/osutil"
)

type envSuite struct{}

func TestEnvSuite(t *testing.T) {
	tc.Run(t, &envSuite{})
}

func (s *envSuite) TestEnviron(c *tc.C) {
	restore := osutil.FakeEnviron(func() []string {
		return []string{"FOO=bar", "BAR=", "TEMP"}
	})
	defer restore()

	env := osutil.Environ()

	c.Assert(len(env), tc.Equals, 3)
	c.Assert(env, tc.DeepEquals, map[string]string{
		"FOO":  "bar",
		"BAR":  "",
		"TEMP": "",
	})
}
