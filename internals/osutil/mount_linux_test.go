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
	"testing"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/osutil"
)

type mountSuite struct{}

func TestMountSuite(t *testing.T) {
	tc.Run(t, &mountSuite{})
}

func (s *mountSuite) TestIsMountedHappyish(c *tc.C) {
	// note the different optional fields
	const content = "" +
		"44 24 7:1 / /snap/ubuntu-core/855 rw,relatime shared:27 - squashfs /dev/loop1 ro\n" +
		"44 24 7:1 / /snap/something/123 rw,relatime - squashfs /dev/loop2 ro\n" +
		"44 24 7:1 / /snap/random/456 rw,relatime opt:1 shared:27 - squashfs /dev/loop1 ro\n"
	defer osutil.FakeMountInfo(content)()

	mounted, err := osutil.IsMounted("/snap/ubuntu-core/855")
	c.Check(err, tc.IsNil)
	c.Check(mounted, tc.Equals, true)

	mounted, err = osutil.IsMounted("/snap/something/123")
	c.Check(err, tc.IsNil)
	c.Check(mounted, tc.Equals, true)

	mounted, err = osutil.IsMounted("/snap/random/456")
	c.Check(err, tc.IsNil)
	c.Check(mounted, tc.Equals, true)

	mounted, err = osutil.IsMounted("/random/made/up/name")
	c.Check(err, tc.IsNil)
	c.Check(mounted, tc.Equals, false)
}

func (s *mountSuite) TestIsMountedBroken(c *tc.C) {
	defer osutil.FakeMountInfo("44 24 7:1 ...truncated-stuff")()

	mounted, err := osutil.IsMounted("/snap/ubuntu-core/855")
	c.Check(err, tc.ErrorMatches, "incorrect number of fields, .*")
	c.Check(mounted, tc.Equals, false)
}
