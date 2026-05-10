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

package testutil_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/testutil"
)

type fileContentCheckerSuite struct{}

func TestFileContentCheckerSuite(t *testing.T) {
	tc.Run(t, &fileContentCheckerSuite{})
}

type myStringer struct{ str string }

func (m myStringer) String() string { return m.str }

func (s *fileContentCheckerSuite) TestFileEquals(c *tc.C) {
	d := c.MkDir()
	content := "not-so-random-string"
	filename := filepath.Join(d, "canary")
	c.Assert(os.WriteFile(filename, []byte(content), 0644), tc.IsNil)

	testInfo(c, testutil.FileEquals, "FileEquals", []string{"filename", "contents"})
	testCheck(c, testutil.FileEquals, true, "", filename, content)
	testCheck(c, testutil.FileEquals, true, "", filename, []byte(content))
	testCheck(c, testutil.FileEquals, true, "", filename, myStringer{content})

	twofer := content + content
	testCheck(c, testutil.FileEquals, false, "Cannot match with file contents:\nnot-so-random-string", filename, twofer)
	testCheck(c, testutil.FileEquals, false, "Cannot match with file contents:\n<binary data>", filename, []byte(twofer))
	testCheck(c, testutil.FileEquals, false, "Cannot match with file contents:\nnot-so-random-string", filename, myStringer{twofer})

	testCheck(c, testutil.FileEquals, false, `Cannot read file "": open : no such file or directory`, "", "")
	testCheck(c, testutil.FileEquals, false, "Filename must be a string", 42, "")
	testCheck(c, testutil.FileEquals, false, "Cannot compare file contents with something of type int", filename, 1)
}

func (s *fileContentCheckerSuite) TestFileContains(c *tc.C) {
	d := c.MkDir()
	content := "not-so-random-string"
	filename := filepath.Join(d, "canary")
	c.Assert(os.WriteFile(filename, []byte(content), 0644), tc.IsNil)

	testInfo(c, testutil.FileContains, "FileContains", []string{"filename", "contents"})
	testCheck(c, testutil.FileContains, true, "", filename, content[1:])
	testCheck(c, testutil.FileContains, true, "", filename, []byte(content[1:]))
	testCheck(c, testutil.FileContains, true, "", filename, myStringer{content[1:]})
	// undocumented
	testCheck(c, testutil.FileContains, true, "", filename, regexp.MustCompile(".*"))

	twofer := content + content
	testCheck(c, testutil.FileContains, false, "Cannot match with file contents:\nnot-so-random-string", filename, twofer)
	testCheck(c, testutil.FileContains, false, "Cannot match with file contents:\n<binary data>", filename, []byte(twofer))
	testCheck(c, testutil.FileContains, false, "Cannot match with file contents:\nnot-so-random-string", filename, myStringer{twofer})
	// undocumented
	testCheck(c, testutil.FileContains, false, "Cannot match with file contents:\nnot-so-random-string", filename, regexp.MustCompile("^$"))

	testCheck(c, testutil.FileContains, false, `Cannot read file "": open : no such file or directory`, "", "")
	testCheck(c, testutil.FileContains, false, "Filename must be a string", 42, "")
	testCheck(c, testutil.FileContains, false, "Cannot compare file contents with something of type int", filename, 1)
}

func (s *fileContentCheckerSuite) TestFileMatches(c *tc.C) {
	d := c.MkDir()
	content := "not-so-random-string"
	filename := filepath.Join(d, "canary")
	c.Assert(os.WriteFile(filename, []byte(content), 0644), tc.IsNil)

	testInfo(c, testutil.FileMatches, "FileMatches", []string{"filename", "regex"})
	testCheck(c, testutil.FileMatches, true, "", filename, ".*")
	testCheck(c, testutil.FileMatches, true, "", filename, "^"+regexp.QuoteMeta(content)+"$")

	testCheck(c, testutil.FileMatches, false, "Cannot match with file contents:\nnot-so-random-string", filename, "^$")
	testCheck(c, testutil.FileMatches, false, "Cannot match with file contents:\nnot-so-random-string", filename, "123"+regexp.QuoteMeta(content))

	testCheck(c, testutil.FileMatches, false, `Cannot read file "": open : no such file or directory`, "", "")
	testCheck(c, testutil.FileMatches, false, "Filename must be a string", 42, ".*")
	testCheck(c, testutil.FileMatches, false, "Regex must be a string", filename, 1)
}
