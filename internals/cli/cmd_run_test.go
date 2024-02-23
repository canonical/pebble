// Copyright (c) 2024 Canonical Ltd
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

package cli_test

import (
	"io/fs"
	"os"
	"path"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestMaybeCopyPebbleDir(c *C) {
	src := c.MkDir()
	err := os.MkdirAll(path.Join(src, "a", "b", "c"), 0700)
	c.Assert(err, IsNil)
	err = os.WriteFile(path.Join(src, "a", "b", "c", "a.yaml"), []byte("# hi\n"), 0666)
	c.Assert(err, IsNil)
	err = os.WriteFile(path.Join(src, "a.yaml"), []byte("# bye\n"), 0666)
	c.Assert(err, IsNil)

	dst := c.MkDir()
	err = cli.MaybeCopyPebbleDir(dst, src)
	c.Assert(err, IsNil)

	got := map[string]bool{}
	dstFS := os.DirFS(dst)
	err = fs.WalkDir(dstFS, ".", func(path string, d fs.DirEntry, err error) error {
		switch path {
		case ".", "a", "a/b", "a/b/c":
		case "a.yaml":
			c.Check(got[path], Equals, false)
			data, err := fs.ReadFile(dstFS, path)
			c.Check(err, IsNil)
			c.Check(data, DeepEquals, []byte("# bye\n"))
			got[path] = true
		case "a/b/c/a.yaml":
			c.Check(got[path], Equals, false)
			data, err := fs.ReadFile(dstFS, path)
			c.Check(err, IsNil)
			c.Check(data, DeepEquals, []byte("# hi\n"))
			got[path] = true
		default:
			c.Errorf("bad path %s", path)
		}
		return err
	})
	c.Assert(err, IsNil)
	c.Assert(got, DeepEquals, map[string]bool{
		"a.yaml":       true,
		"a/b/c/a.yaml": true,
	})
}

func (s *PebbleSuite) TestMaybeCopyPebbleDirNoCopy(c *C) {
	src := c.MkDir()
	err := os.MkdirAll(path.Join(src, "a", "b", "c"), 0700)
	c.Assert(err, IsNil)
	err = os.WriteFile(path.Join(src, "a", "b", "c", "a.yaml"), []byte("# hi\n"), 0666)
	c.Assert(err, IsNil)
	err = os.WriteFile(path.Join(src, "a.yaml"), []byte("# bye\n"), 0666)
	c.Assert(err, IsNil)

	dst := c.MkDir()
	err = os.WriteFile(path.Join(dst, "a.yaml"), []byte("# no\n"), 0666)
	c.Assert(err, IsNil)

	err = cli.MaybeCopyPebbleDir(dst, src)
	c.Assert(err, IsNil)

	got := map[string]bool{}
	dstFS := os.DirFS(dst)
	err = fs.WalkDir(dstFS, ".", func(path string, d fs.DirEntry, err error) error {
		switch path {
		case ".":
		case "a.yaml":
			c.Check(got[path], Equals, false)
			data, err := fs.ReadFile(dstFS, path)
			c.Check(err, IsNil)
			c.Check(data, DeepEquals, []byte("# no\n"))
			got[path] = true
		default:
			c.Errorf("bad path %s", path)
		}
		return err
	})
	c.Assert(err, IsNil)
	c.Assert(got, DeepEquals, map[string]bool{
		"a.yaml": true,
	})
}
