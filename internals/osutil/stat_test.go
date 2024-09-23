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

package osutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	. "gopkg.in/check.v1"
)

type StatTestSuite struct{}

var _ = Suite(&StatTestSuite{})

func (ts *StatTestSuite) TestCanStat(c *C) {
	fname := filepath.Join(c.MkDir(), "foo")
	err := os.WriteFile(fname, []byte(fname), 0644)
	c.Assert(err, IsNil)

	c.Assert(CanStat(fname), Equals, true)
	c.Assert(CanStat("/i-do-not-exist"), Equals, false)
}

func (ts *StatTestSuite) TestCanStatOddPerms(c *C) {
	fname := filepath.Join(c.MkDir(), "foo")
	err := os.WriteFile(fname, []byte(fname), 0100)
	c.Assert(err, IsNil)

	c.Assert(CanStat(fname), Equals, true)
}

func (ts *StatTestSuite) TestIsDir(c *C) {
	dname := filepath.Join(c.MkDir(), "bar")
	err := os.Mkdir(dname, 0700)
	c.Assert(err, IsNil)

	c.Assert(IsDir(dname), Equals, true)
	c.Assert(IsDir("/i-do-not-exist"), Equals, false)
}

func (ts *StatTestSuite) TestIsSymlink(c *C) {
	sname := filepath.Join(c.MkDir(), "symlink")
	err := os.Symlink("/", sname)
	c.Assert(err, IsNil)

	c.Assert(IsSymlink(sname), Equals, true)
	c.Assert(IsSymlink(c.MkDir()), Equals, false)
}

func (ts *StatTestSuite) TestIsExecInPath(c *C) {
	oldPath := os.Getenv("PATH")
	defer os.Setenv("PATH", oldPath)
	d := c.MkDir()
	os.Setenv("PATH", d)
	c.Check(IsExecInPath("xyzzy"), Equals, false)

	fname := filepath.Join(d, "xyzzy")
	c.Assert(os.WriteFile(fname, []byte{}, 0644), IsNil)
	c.Check(IsExecInPath("xyzzy"), Equals, false)

	c.Assert(os.Chmod(fname, 0755), IsNil)
	c.Check(IsExecInPath("xyzzy"), Equals, true)
}

func (s *StatTestSuite) TestLookPathDefaultGivesCorrectPath(c *C) {
	lookPath = func(name string) (string, error) { return "/bin/true", nil }
	c.Assert(LookPathDefault("true", "/bin/foo"), Equals, "/bin/true")
}

func (s *StatTestSuite) TestLookPathDefaultReturnsDefaultWhenNotFound(c *C) {
	lookPath = func(name string) (string, error) { return "", fmt.Errorf("Not found") }
	c.Assert(LookPathDefault("bar", "/bin/bla"), Equals, "/bin/bla")
}

func makeTestPath(c *C, path string, mode os.FileMode) string {
	return makeTestPathInDir(c, c.MkDir(), path, mode)
}

func makeTestPathInDir(c *C, dir string, path string, mode os.FileMode) string {
	mkdir := strings.HasSuffix(path, "/")
	path = filepath.Join(dir, path)

	if mkdir {
		// request for directory
		c.Assert(os.MkdirAll(path, mode), IsNil)
	} else {
		// request for a file
		c.Assert(os.MkdirAll(filepath.Dir(path), 0755), IsNil)
		c.Assert(os.WriteFile(path, nil, mode), IsNil)
	}

	return path
}

func (s *StatTestSuite) TestIsWritableDir(c *C) {
	if os.Getuid() == 0 {
		c.Skip("requires running as non-root user")
	}

	for _, t := range []struct {
		path       string
		mode       os.FileMode
		isWritable bool
	}{
		{"dir/", 0755, true},
		{"dir/", 0555, false},
		{"dir/", 0750, true},
		{"dir/", 0550, false},
		{"dir/", 0700, true},
		{"dir/", 0500, false},

		{"file", 0644, true},
		{"file", 0444, false},
		{"file", 0640, true},
		{"file", 0440, false},
		{"file", 0600, true},
		{"file", 0400, false},
	} {
		writable := IsWritable(makeTestPath(c, t.path, t.mode))
		c.Check(writable, Equals, t.isWritable, Commentf("incorrect result for %q (%s), got %v, expected %v", t.path, t.mode, writable, t.isWritable))
	}
}

func (s *StatTestSuite) TestIsDirNotExist(c *C) {
	for _, e := range []error{
		os.ErrNotExist,
		syscall.ENOENT,
		syscall.ENOTDIR,
		&os.PathError{Err: syscall.ENOENT},
		&os.PathError{Err: syscall.ENOTDIR},
		&os.LinkError{Err: syscall.ENOENT},
		&os.LinkError{Err: syscall.ENOTDIR},
		&os.SyscallError{Err: syscall.ENOENT},
		&os.SyscallError{Err: syscall.ENOTDIR},
	} {
		c.Check(IsDirNotExist(e), Equals, true, Commentf("%#v (%v)", e, e))
	}

	for _, e := range []error{
		nil,
		fmt.Errorf("hello"),
	} {
		c.Check(IsDirNotExist(e), Equals, false)
	}
}

func (s *StatTestSuite) TestExistsIsDir(c *C) {
	for _, t := range []struct {
		make   string
		path   string
		exists bool
		isDir  bool
	}{
		{"", "foo", false, false},
		{"", "foo/bar", false, false},
		{"foo", "foo/bar", false, false},
		{"foo", "foo", true, false},
		{"foo/", "foo", true, true},
	} {
		base := c.MkDir()
		comm := Commentf("path:%q make:%q", t.path, t.make)
		if t.make != "" {
			makeTestPathInDir(c, base, t.make, 0755)
		}
		exists, isDir, err := ExistsIsDir(filepath.Join(base, t.path))
		c.Check(exists, Equals, t.exists, comm)
		c.Check(isDir, Equals, t.isDir, comm)
		c.Check(err, IsNil, comm)
	}

	if os.Getuid() == 0 {
		c.Skip("requires running as non-root user")
	}
	p := makeTestPath(c, "foo/bar", 0)
	c.Assert(os.Chmod(filepath.Dir(p), 0), IsNil)
	defer os.Chmod(filepath.Dir(p), 0755)
	exists, isDir, err := ExistsIsDir(p)
	c.Check(exists, Equals, false)
	c.Check(isDir, Equals, false)
	c.Check(err, NotNil)
}

func (s *StatTestSuite) TestIsExec(c *C) {
	c.Check(IsExec("non-existent"), Equals, false)
	c.Check(IsExec("."), Equals, false)
	dir := c.MkDir()
	c.Check(IsExec(dir), Equals, false)

	for _, tc := range []struct {
		mode os.FileMode
		is   bool
	}{
		{0644, false},
		{0444, false},
		{0444, false},
		{0000, false},
		{0100, true},
		{0010, true},
		{0001, true},
		{0755, true},
	} {
		c.Logf("tc: %v %v", tc.mode, tc.is)
		p := filepath.Join(dir, "foo")
		err := os.Remove(p)
		c.Check(err == nil || os.IsNotExist(err), Equals, true)

		err = os.WriteFile(p, []byte(""), tc.mode)
		c.Assert(err, IsNil)
		c.Check(IsExec(p), Equals, tc.is)
	}
}
