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
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/cli"
	"github.com/canonical/pebble/internals/overlord"
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

func (s *PebbleSuite) TestMaybeCopyPebbleDirSourceNotExist(c *C) {
	tmpDir := c.MkDir()
	dst := path.Join(tmpDir, "dst")
	err := os.Mkdir(dst, 0o700)
	c.Assert(err, IsNil)
	src := path.Join(tmpDir, "not-exist")
	err = cli.MaybeCopyPebbleDir(dst, src)
	c.Assert(err, IsNil)
}

func (s *PebbleSuite) TestMaybeCopyPebbleDirSourceNotADirectory(c *C) {
	tmpDir := c.MkDir()
	dst := path.Join(tmpDir, "dst")
	err := os.Mkdir(dst, 0o700)
	c.Assert(err, IsNil)
	src := path.Join(tmpDir, "file")
	err = os.WriteFile(src, nil, 0o666)
	c.Assert(err, IsNil)
	err = cli.MaybeCopyPebbleDir(dst, src)
	c.Assert(err, ErrorMatches, ".*not a directory.*")
}

func (s *PebbleSuite) TestSetupTLSOptionsHTTPSNotSet(c *C) {
	pebbleDir := c.MkDir()

	// No HTTPS configured: no identity should be loaded or generated, and
	// the persist mode must not matter.
	opts, err := cli.SetupTLSOptions(pebbleDir, "", overlord.PersistDefault)
	c.Assert(err, IsNil)
	c.Check(opts.Signer, IsNil)

	opts, err = cli.SetupTLSOptions(pebbleDir, "", overlord.PersistNever)
	c.Assert(err, IsNil)
	c.Check(opts.Signer, IsNil)

	// Also confirm idkey.Get was never called by checking no "identity"
	// directory was created in pebbleDir.
	_, err = os.Stat(filepath.Join(pebbleDir, "identity"))
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *PebbleSuite) TestSetupTLSOptionsHTTPSWithPersistNever(c *C) {
	pebbleDir := c.MkDir()

	opts, err := cli.SetupTLSOptions(pebbleDir, ":8443", overlord.PersistNever)
	c.Assert(err, ErrorMatches, `cannot use --https with PEBBLE_PERSIST=never: identity key requires persistent state`)
	c.Check(opts.Signer, IsNil)

	// No identity directory or key should have been created.
	_, err = os.Stat(filepath.Join(pebbleDir, "identity"))
	c.Check(os.IsNotExist(err), Equals, true)
}

func (s *PebbleSuite) TestSetupTLSOptionsHTTPSWithPersistDefault(c *C) {
	pebbleDir := c.MkDir()

	opts, err := cli.SetupTLSOptions(pebbleDir, ":8443", overlord.PersistDefault)
	c.Assert(err, IsNil)
	c.Assert(opts.Signer, NotNil)
	c.Check(opts.Signer.Fingerprint(), Not(Equals), "")

	// The identity key was persisted to disk.
	info, err := os.Stat(filepath.Join(pebbleDir, "identity", "key.pem"))
	c.Assert(err, IsNil)
	c.Check(info.Mode().IsRegular(), Equals, true)

	// Calling again should reuse the existing key (same fingerprint).
	opts2, err := cli.SetupTLSOptions(pebbleDir, ":8443", overlord.PersistDefault)
	c.Assert(err, IsNil)
	c.Assert(opts2.Signer, NotNil)
	c.Check(opts2.Signer.Fingerprint(), Equals, opts.Signer.Fingerprint())
}
