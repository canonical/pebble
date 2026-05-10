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

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestMaybeCopyPebbleDir(c *tc.C) {
	src := c.MkDir()
	err := os.MkdirAll(path.Join(src, "a", "b", "c"), 0700)
	c.Assert(err, tc.IsNil)
	err = os.WriteFile(path.Join(src, "a", "b", "c", "a.yaml"), []byte("# hi\n"), 0666)
	c.Assert(err, tc.IsNil)
	err = os.WriteFile(path.Join(src, "a.yaml"), []byte("# bye\n"), 0666)
	c.Assert(err, tc.IsNil)

	dst := c.MkDir()
	err = cli.MaybeCopyPebbleDir(dst, src)
	c.Assert(err, tc.IsNil)

	got := map[string]bool{}
	dstFS := os.DirFS(dst)
	err = fs.WalkDir(dstFS, ".", func(path string, d fs.DirEntry, err error) error {
		switch path {
		case ".", "a", "a/b", "a/b/c":
		case "a.yaml":
			c.Check(got[path], tc.Equals, false)
			data, err := fs.ReadFile(dstFS, path)
			c.Check(err, tc.IsNil)
			c.Check(data, tc.DeepEquals, []byte("# bye\n"))
			got[path] = true
		case "a/b/c/a.yaml":
			c.Check(got[path], tc.Equals, false)
			data, err := fs.ReadFile(dstFS, path)
			c.Check(err, tc.IsNil)
			c.Check(data, tc.DeepEquals, []byte("# hi\n"))
			got[path] = true
		default:
			c.Errorf("bad path %s", path)
		}
		return err
	})
	c.Assert(err, tc.IsNil)
	c.Assert(got, tc.DeepEquals, map[string]bool{
		"a.yaml":       true,
		"a/b/c/a.yaml": true,
	})
}

func (s *PebbleSuite) TestMaybeCopyPebbleDirNoCopy(c *tc.C) {
	src := c.MkDir()
	err := os.MkdirAll(path.Join(src, "a", "b", "c"), 0700)
	c.Assert(err, tc.IsNil)
	err = os.WriteFile(path.Join(src, "a", "b", "c", "a.yaml"), []byte("# hi\n"), 0666)
	c.Assert(err, tc.IsNil)
	err = os.WriteFile(path.Join(src, "a.yaml"), []byte("# bye\n"), 0666)
	c.Assert(err, tc.IsNil)

	dst := c.MkDir()
	err = os.WriteFile(path.Join(dst, "a.yaml"), []byte("# no\n"), 0666)
	c.Assert(err, tc.IsNil)

	err = cli.MaybeCopyPebbleDir(dst, src)
	c.Assert(err, tc.IsNil)

	got := map[string]bool{}
	dstFS := os.DirFS(dst)
	err = fs.WalkDir(dstFS, ".", func(path string, d fs.DirEntry, err error) error {
		switch path {
		case ".":
		case "a.yaml":
			c.Check(got[path], tc.Equals, false)
			data, err := fs.ReadFile(dstFS, path)
			c.Check(err, tc.IsNil)
			c.Check(data, tc.DeepEquals, []byte("# no\n"))
			got[path] = true
		default:
			c.Errorf("bad path %s", path)
		}
		return err
	})
	c.Assert(err, tc.IsNil)
	c.Assert(got, tc.DeepEquals, map[string]bool{
		"a.yaml": true,
	})
}

func (s *PebbleSuite) TestMaybeCopyPebbleDirSourceNotExist(c *tc.C) {
	tmpDir := c.MkDir()
	dst := path.Join(tmpDir, "dst")
	err := os.Mkdir(dst, 0o700)
	c.Assert(err, tc.IsNil)
	src := path.Join(tmpDir, "not-exist")
	err = cli.MaybeCopyPebbleDir(dst, src)
	c.Assert(err, tc.IsNil)
}

func (s *PebbleSuite) TestMaybeCopyPebbleDirSourceNotADirectory(c *tc.C) {
	tmpDir := c.MkDir()
	dst := path.Join(tmpDir, "dst")
	err := os.Mkdir(dst, 0o700)
	c.Assert(err, tc.IsNil)
	src := path.Join(tmpDir, "file")
	err = os.WriteFile(src, nil, 0o666)
	c.Assert(err, tc.IsNil)
	err = cli.MaybeCopyPebbleDir(dst, src)
	c.Assert(err, tc.ErrorMatches, ".*not a directory.*")
}
