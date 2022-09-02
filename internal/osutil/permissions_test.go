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

package osutil_test

import (
	"fmt"
	"os"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internal/osutil"
)

type permissionsSuite struct{}

var _ = Suite(&permissionsSuite{})

func (permissionsSuite) TestParsePermissions(c *C) {
	cases := map[string]os.FileMode{
		"644":  os.FileMode(0o644),
		"777":  os.FileMode(0o777),
		"700":  os.FileMode(0o700),
		"066":  os.FileMode(0o066),
		"1777": os.FileMode(0o1777),
		"4777": os.FileMode(0o4777),
	}

	for s, m := range cases {
		mode, err := osutil.ParsePermissions(s, 0)
		c.Assert(err, IsNil)
		c.Check(mode, Equals, m)
	}
}

func (permissionsSuite) TestParsePermissionsDefault(c *C) {
	mode, err := osutil.ParsePermissions("", 0o1644)
	c.Assert(err, IsNil)
	c.Check(mode, Equals, os.FileMode(0o1644))
}

func (permissionsSuite) TestParsePermissionsFails(c *C) {
	cases := []string{"66", "0", "00", "4832", "sfdeljknesv", " ", "   "}
	for _, s := range cases {
		_, err := osutil.ParsePermissions(s, 0)
		c.Assert(err, ErrorMatches, fmt.Sprintf("file permissions must be a 3- or 4-digit octal string, got %q", s))
	}
}
