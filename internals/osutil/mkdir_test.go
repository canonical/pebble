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
	"os"
	"os/user"
	"strconv"
	"syscall"

	"gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/osutil"
	"github.com/canonical/pebble/internals/osutil/sys"
)

type mkdacSuite struct{}

var _ = check.Suite(&mkdacSuite{})

// Chown requires root, so it's not tested, only test MakeParents, ExistOK, Chmod,
// and the combination of them.
func (mkdacSuite) TestMkdir(c *check.C) {
	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo", 0o755, nil)
	c.Assert(err, check.IsNil)
	c.Assert(osutil.IsDir(tmpDir+"/foo"), check.Equals, true)
}

func (mkdacSuite) TestMkdirExistNotOK(c *check.C) {
	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo", 0o755, nil)
	c.Assert(err, check.IsNil)
	c.Assert(osutil.IsDir(tmpDir+"/foo"), check.Equals, true)

	err = osutil.Mkdir(tmpDir+"/foo", 0o755, nil)
	c.Assert(err, check.ErrorMatches, `.*: file exists`)
}

func (mkdacSuite) TestMkdirExistOK(c *check.C) {
	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo", 0o755, nil)
	c.Assert(err, check.IsNil)
	c.Assert(osutil.IsDir(tmpDir+"/foo"), check.Equals, true)

	err = osutil.Mkdir(tmpDir+"/foo", 0o755, &osutil.MkdirOptions{ExistOK: true})
	c.Assert(err, check.IsNil)
}

func (mkdacSuite) TestMkdirEndWithSlash(c *check.C) {
	tmpDir := c.MkDir()

	err := osutil.Mkdir(
		tmpDir+"/foo/",
		0755,
		&osutil.MkdirOptions{
			MakeParents: true,
			ExistOK:     true,
		},
	)
	c.Assert(err, check.IsNil)
}

func (mkdacSuite) TestMkdirMakeParents(c *check.C) {
	tmpDir := c.MkDir()

	err := osutil.Mkdir(
		tmpDir+"/foo/bar",
		0o755,
		&osutil.MkdirOptions{MakeParents: true},
	)
	c.Assert(err, check.IsNil)
	c.Assert(osutil.IsDir(tmpDir+"/foo"), check.Equals, true)
	c.Assert(osutil.IsDir(tmpDir+"/foo/bar"), check.Equals, true)
}

func (mkdacSuite) TestMkdirMakeParentsExistNotOK(c *check.C) {
	tmpDir := c.MkDir()

	err := osutil.Mkdir(
		tmpDir+"/foo/bar",
		0o755,
		&osutil.MkdirOptions{MakeParents: true},
	)
	c.Assert(err, check.IsNil)
	c.Assert(osutil.IsDir(tmpDir+"/foo"), check.Equals, true)
	c.Assert(osutil.IsDir(tmpDir+"/foo/bar"), check.Equals, true)

	err = osutil.Mkdir(tmpDir+"/foo/bar", 0o755, nil)
	c.Assert(err, check.ErrorMatches, `.*: file exists`)
}

func (mkdacSuite) TestMkdirMakeParentsExistOK(c *check.C) {
	tmpDir := c.MkDir()

	err := osutil.Mkdir(
		tmpDir+"/foo/bar",
		0o755,
		&osutil.MkdirOptions{MakeParents: true},
	)
	c.Assert(err, check.IsNil)
	c.Assert(osutil.IsDir(tmpDir+"/foo"), check.Equals, true)
	c.Assert(osutil.IsDir(tmpDir+"/foo/bar"), check.Equals, true)

	err = osutil.Mkdir(tmpDir+"/foo/bar/", 0o755, &osutil.MkdirOptions{ExistOK: true})
	c.Assert(err, check.IsNil)
}

func (mkdacSuite) TestMkdirChmod(c *check.C) {
	oldmask := syscall.Umask(0022)
	defer syscall.Umask(oldmask)

	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo", 0o777, &osutil.MkdirOptions{Chmod: true})
	c.Assert(err, check.IsNil)
	c.Assert(osutil.IsDir(tmpDir+"/foo"), check.Equals, true)

	info, err := os.Stat(tmpDir + "/foo")
	c.Assert(err, check.IsNil)
	c.Assert(info.Mode().Perm(), check.Equals, os.FileMode(0o777))
}

func (mkdacSuite) TestMkdirNoChmod(c *check.C) {
	oldmask := syscall.Umask(0022)
	defer syscall.Umask(oldmask)

	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo", 0o777, nil)
	c.Assert(err, check.IsNil)
	c.Assert(osutil.IsDir(tmpDir+"/foo"), check.Equals, true)

	info, err := os.Stat(tmpDir + "/foo")
	c.Assert(err, check.IsNil)
	c.Assert(info.Mode().Perm(), check.Equals, os.FileMode(0o755))
}

func (mkdacSuite) TestMkdirMakePatentsChmod(c *check.C) {
	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo/bar", 0o777, &osutil.MkdirOptions{MakeParents: true, Chmod: true})
	c.Assert(err, check.IsNil)
	c.Assert(osutil.IsDir(tmpDir+"/foo"), check.Equals, true)
	c.Assert(osutil.IsDir(tmpDir+"/foo/bar"), check.Equals, true)

	info, err := os.Stat(tmpDir + "/foo")
	c.Assert(err, check.IsNil)
	c.Assert(info.Mode().Perm(), check.Equals, os.FileMode(0o777))

	info, err = os.Stat(tmpDir + "/foo/bar")
	c.Assert(err, check.IsNil)
	c.Assert(info.Mode().Perm(), check.Equals, os.FileMode(0o777))
}

func (mkdacSuite) TestMkdirMakeParentsNoChmod(c *check.C) {
	oldmask := syscall.Umask(0022)
	defer syscall.Umask(oldmask)

	tmpDir := c.MkDir()

	err := osutil.Mkdir(tmpDir+"/foo/bar", 0o777, &osutil.MkdirOptions{MakeParents: true})
	c.Assert(err, check.IsNil)
	c.Assert(osutil.IsDir(tmpDir+"/foo"), check.Equals, true)
	c.Assert(osutil.IsDir(tmpDir+"/foo/bar"), check.Equals, true)

	info, err := os.Stat(tmpDir + "/foo")
	c.Assert(err, check.IsNil)
	c.Assert(info.Mode().Perm(), check.Equals, os.FileMode(0o755))

	info, err = os.Stat(tmpDir + "/foo/bar")
	c.Assert(err, check.IsNil)
	c.Assert(info.Mode().Perm(), check.Equals, os.FileMode(0o755))
}

// See .github/workflows/tests.yml for how to run this test as root.
func (mkdacSuite) TestMkdirChownAndChmod(c *check.C) {
	if os.Getuid() != 0 {
		c.Skip("requires running as root")
	}

	username := os.Getenv("PEBBLE_TEST_USER")
	group := os.Getenv("PEBBLE_TEST_GROUP")
	if username == "" || group == "" {
		c.Fatalf("must set PEBBLE_TEST_USER and PEBBLE_TEST_GROUP")
	}

	u, err := user.Lookup(username)
	c.Assert(err, check.IsNil)
	g, err := user.LookupGroup(group)
	c.Assert(err, check.IsNil)
	uid, err := strconv.Atoi(u.Uid)
	c.Assert(err, check.IsNil)
	gid, err := strconv.Atoi(g.Gid)
	c.Assert(err, check.IsNil)

	tmpDir := c.MkDir()

	err = osutil.Mkdir(
		tmpDir+"/foo",
		0o777,
		&osutil.MkdirOptions{
			Chmod:   true,
			Chown:   true,
			UserID:  sys.UserID(uid),
			GroupID: sys.GroupID(gid),
		},
	)
	c.Assert(err, check.IsNil)
	c.Assert(osutil.IsDir(tmpDir+"/foo"), check.Equals, true)

	info, err := os.Stat(tmpDir + "/foo")
	c.Assert(err, check.IsNil)
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		c.Assert(int(stat.Uid), check.Equals, uid)
		c.Assert(int(stat.Gid), check.Equals, gid)
	}
	c.Assert(info.Mode().Perm(), check.Equals, os.FileMode(0o777))
}

// See .github/workflows/tests.yml for how to run this test as root.
func (mkdacSuite) TestMkdirMakeParentsChownAndChown(c *check.C) {
	if os.Getuid() != 0 {
		c.Skip("requires running as root")
	}

	username := os.Getenv("PEBBLE_TEST_USER")
	group := os.Getenv("PEBBLE_TEST_GROUP")
	if username == "" || group == "" {
		c.Fatalf("must set PEBBLE_TEST_USER and PEBBLE_TEST_GROUP")
	}

	u, err := user.Lookup(username)
	c.Assert(err, check.IsNil)
	g, err := user.LookupGroup(group)
	c.Assert(err, check.IsNil)
	uid, err := strconv.Atoi(u.Uid)
	c.Assert(err, check.IsNil)
	gid, err := strconv.Atoi(g.Gid)
	c.Assert(err, check.IsNil)
	tmpDir := c.MkDir()

	err = osutil.Mkdir(
		tmpDir+"/foo/bar",
		0o777,
		&osutil.MkdirOptions{
			MakeParents: true,
			Chmod:       true,
			Chown:       true,
			UserID:      sys.UserID(uid),
			GroupID:     sys.GroupID(gid),
		},
	)
	c.Assert(err, check.IsNil)
	c.Assert(osutil.IsDir(tmpDir+"/foo"), check.Equals, true)
	c.Assert(osutil.IsDir(tmpDir+"/foo/bar"), check.Equals, true)

	info, err := os.Stat(tmpDir + "/foo")
	c.Assert(err, check.IsNil)
	c.Assert(info.Mode().Perm(), check.Equals, os.FileMode(0o777))
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		c.Assert(int(stat.Uid), check.Equals, uid)
		c.Assert(int(stat.Gid), check.Equals, gid)
	}

	info, err = os.Stat(tmpDir + "/foo/bar")
	c.Assert(err, check.IsNil)
	c.Assert(info.Mode().Perm(), check.Equals, os.FileMode(0o777))
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		c.Assert(int(stat.Uid), check.Equals, uid)
		c.Assert(int(stat.Gid), check.Equals, gid)
	}
}
