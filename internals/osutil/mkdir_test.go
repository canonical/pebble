// Copyright (c) 2014-2024 Canonical Ltd
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
	"os"
	"os/user"
	"strconv"
	"syscall"
	"testing"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/osutil"
	"github.com/canonical/pebble/internals/osutil/sys"
)

type mkdirSuite struct{}

func TestMkdirSuite(t *testing.T) {
	tc.Run(t, &mkdirSuite{})
}

// Chown requires root, so it's not tested, only test MakeParents, ExistOK, Chmod,
// and the combination of them.
func (mkdirSuite) TestSimpleDir(c *tc.C) {
	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo", 0o755, nil)
	c.Assert(err, tc.IsNil)
	c.Assert(osutil.IsDir(tmpDir+"/foo"), tc.Equals, true)
}

func (mkdirSuite) TestExistOK(c *tc.C) {
	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo", 0o755, nil)
	c.Assert(err, tc.IsNil)
	c.Assert(osutil.IsDir(tmpDir+"/foo"), tc.Equals, true)

	err = osutil.Mkdir(tmpDir+"/foo", 0o755, &osutil.MkdirOptions{
		ExistOK: true,
	})
	c.Assert(err, tc.IsNil)
}

func (mkdirSuite) TestExistNotOK(c *tc.C) {
	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo", 0o755, nil)
	c.Assert(err, tc.IsNil)
	c.Assert(osutil.IsDir(tmpDir+"/foo"), tc.Equals, true)

	err = osutil.Mkdir(tmpDir+"/foo", 0o755, nil)
	c.Assert(err, tc.ErrorMatches, `.*: file exists`)
}

func (mkdirSuite) TestExistsButNotDir(c *tc.C) {
	tmpDir := c.MkDir()

	_, err := os.Create(tmpDir + "/foo")
	c.Assert(err, tc.IsNil)

	err = osutil.Mkdir(tmpDir+"/foo", 0o755, nil)
	c.Assert(err, tc.ErrorMatches, `.*: not a directory`)
}

func (mkdirSuite) TestDirEndWithSlash(c *tc.C) {
	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo/", 0755, nil)
	c.Assert(err, tc.IsNil)
}

func (mkdirSuite) TestMakeParents(c *tc.C) {
	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo/bar", 0o755, &osutil.MkdirOptions{
		MakeParents: true,
	})
	c.Assert(err, tc.IsNil)
	c.Assert(osutil.IsDir(tmpDir+"/foo"), tc.Equals, true)
	c.Assert(osutil.IsDir(tmpDir+"/foo/bar"), tc.Equals, true)
}

func (mkdirSuite) TestMakeParentsAndExistOK(c *tc.C) {
	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo/bar", 0o755, &osutil.MkdirOptions{
		MakeParents: true,
	})
	c.Assert(err, tc.IsNil)
	c.Assert(osutil.IsDir(tmpDir+"/foo"), tc.Equals, true)
	c.Assert(osutil.IsDir(tmpDir+"/foo/bar"), tc.Equals, true)

	err = osutil.Mkdir(tmpDir+"/foo/bar/", 0o755, &osutil.MkdirOptions{
		ExistOK: true,
	})
	c.Assert(err, tc.IsNil)
}

func (mkdirSuite) TestMakeParentsAndExistNotOK(c *tc.C) {
	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo/bar", 0o755, &osutil.MkdirOptions{
		MakeParents: true,
	})
	c.Assert(err, tc.IsNil)
	c.Assert(osutil.IsDir(tmpDir+"/foo"), tc.Equals, true)
	c.Assert(osutil.IsDir(tmpDir+"/foo/bar"), tc.Equals, true)

	err = osutil.Mkdir(tmpDir+"/foo/bar", 0o755, nil)
	c.Assert(err, tc.ErrorMatches, `.*: file exists`)
}

func (mkdirSuite) TestParentExistsButNotDir(c *tc.C) {
	tmpDir := c.MkDir()

	_, err := os.Create(tmpDir + "/foo")
	c.Assert(err, tc.IsNil)

	err = osutil.Mkdir(tmpDir+"/foo/bar/", 0o755, &osutil.MkdirOptions{
		MakeParents: true,
	})
	c.Assert(err, tc.ErrorMatches, `.*: not a directory`)
}

func (mkdirSuite) TestChmod(c *tc.C) {
	oldmask := syscall.Umask(0022)
	defer syscall.Umask(oldmask)

	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo", 0o777, &osutil.MkdirOptions{
		Chmod: true,
	})
	c.Assert(err, tc.IsNil)
	c.Assert(osutil.IsDir(tmpDir+"/foo"), tc.Equals, true)

	info, err := os.Stat(tmpDir + "/foo")
	c.Assert(err, tc.IsNil)
	c.Assert(info.Mode().Perm(), tc.Equals, os.FileMode(0o777))
}

func (mkdirSuite) TestNoChmod(c *tc.C) {
	oldmask := syscall.Umask(0022)
	defer syscall.Umask(oldmask)

	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo", 0o777, nil)
	c.Assert(err, tc.IsNil)
	c.Assert(osutil.IsDir(tmpDir+"/foo"), tc.Equals, true)

	info, err := os.Stat(tmpDir + "/foo")
	c.Assert(err, tc.IsNil)
	c.Assert(info.Mode().Perm(), tc.Equals, os.FileMode(0o755))
}

func (mkdirSuite) TestMakeParentsAndChmod(c *tc.C) {
	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo/bar", 0o777, &osutil.MkdirOptions{
		MakeParents: true,
		Chmod:       true,
	})
	c.Assert(err, tc.IsNil)
	c.Assert(osutil.IsDir(tmpDir+"/foo"), tc.Equals, true)
	c.Assert(osutil.IsDir(tmpDir+"/foo/bar"), tc.Equals, true)

	info, err := os.Stat(tmpDir + "/foo")
	c.Assert(err, tc.IsNil)
	c.Assert(info.Mode().Perm(), tc.Equals, os.FileMode(0o777))

	info, err = os.Stat(tmpDir + "/foo/bar")
	c.Assert(err, tc.IsNil)
	c.Assert(info.Mode().Perm(), tc.Equals, os.FileMode(0o777))
}

func (mkdirSuite) TestMakeParentsAndNoChmod(c *tc.C) {
	oldmask := syscall.Umask(0022)
	defer syscall.Umask(oldmask)

	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo/bar", 0o777, &osutil.MkdirOptions{
		MakeParents: true,
	})
	c.Assert(err, tc.IsNil)
	c.Assert(osutil.IsDir(tmpDir+"/foo"), tc.Equals, true)
	c.Assert(osutil.IsDir(tmpDir+"/foo/bar"), tc.Equals, true)

	info, err := os.Stat(tmpDir + "/foo")
	c.Assert(err, tc.IsNil)
	c.Assert(info.Mode().Perm(), tc.Equals, os.FileMode(0o755))

	info, err = os.Stat(tmpDir + "/foo/bar")
	c.Assert(err, tc.IsNil)
	c.Assert(info.Mode().Perm(), tc.Equals, os.FileMode(0o755))
}

func (mkdirSuite) TestMakeParentsChmodAndChown(c *tc.C) {
	if os.Getuid() != 0 {
		c.Skip("requires running as root")
	}

	username := os.Getenv("PEBBLE_TEST_USER")
	group := os.Getenv("PEBBLE_TEST_GROUP")
	if username == "" || group == "" {
		c.Fatalf("must set PEBBLE_TEST_USER and PEBBLE_TEST_GROUP")
	}

	u, err := user.Lookup(username)
	c.Assert(err, tc.IsNil)
	g, err := user.LookupGroup(group)
	c.Assert(err, tc.IsNil)
	uid, err := strconv.Atoi(u.Uid)
	c.Assert(err, tc.IsNil)
	gid, err := strconv.Atoi(g.Gid)
	c.Assert(err, tc.IsNil)
	tmpDir := c.MkDir()

	err = osutil.Mkdir(tmpDir+"/foo/bar", 0o777, &osutil.MkdirOptions{
		MakeParents: true,
		Chmod:       true,
		Chown:       true,
		UserID:      sys.UserID(uid),
		GroupID:     sys.GroupID(gid),
	})
	c.Assert(err, tc.IsNil)
	c.Assert(osutil.IsDir(tmpDir+"/foo"), tc.Equals, true)
	c.Assert(osutil.IsDir(tmpDir+"/foo/bar"), tc.Equals, true)

	info, err := os.Stat(tmpDir + "/foo")
	c.Assert(err, tc.IsNil)
	c.Assert(info.Mode().Perm(), tc.Equals, os.FileMode(0o777))
	stat, ok := info.Sys().(*syscall.Stat_t)
	c.Assert(ok, tc.Equals, true)
	c.Assert(int(stat.Uid), tc.Equals, uid)
	c.Assert(int(stat.Gid), tc.Equals, gid)

	info, err = os.Stat(tmpDir + "/foo/bar")
	c.Assert(err, tc.IsNil)
	c.Assert(info.Mode().Perm(), tc.Equals, os.FileMode(0o777))
	stat, ok = info.Sys().(*syscall.Stat_t)
	c.Assert(ok, tc.Equals, true)
	c.Assert(int(stat.Uid), tc.Equals, uid)
	c.Assert(int(stat.Gid), tc.Equals, gid)
}
