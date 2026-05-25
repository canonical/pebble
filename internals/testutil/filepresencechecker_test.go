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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/canonical/pebble/internals/testutil"
	"github.com/canonical/tc"
)

type filePresenceCheckerSuite struct{}

func TestFilePresenceCheckerSuite(t *testing.T) {
	tc.Run(t, &filePresenceCheckerSuite{})
}

func (*filePresenceCheckerSuite) TestFilePresent(c *tc.C) {
	d := c.MkDir()
	filename := filepath.Join(d, "foo")
	testInfo(c, testutil.FilePresent, "FilePresent", []string{"filename"})
	testCheck(c, testutil.FilePresent, false, `filename must be a string`, 42)
	testCheck(c, testutil.FilePresent, false, fmt.Sprintf(`file %q is absent but should exist`, filename), filename)
	c.Assert(os.WriteFile(filename, nil, 0644), tc.IsNil)
	testCheck(c, testutil.FilePresent, true, "", filename)
}

func (*filePresenceCheckerSuite) TestFileAbsent(c *tc.C) {
	d := c.MkDir()
	filename := filepath.Join(d, "foo")
	testInfo(c, testutil.FileAbsent, "FileAbsent", []string{"filename"})
	testCheck(c, testutil.FileAbsent, false, `filename must be a string`, 42)
	testCheck(c, testutil.FileAbsent, true, "", filename)
	c.Assert(os.WriteFile(filename, nil, 0644), tc.IsNil)
	testCheck(c, testutil.FileAbsent, false, fmt.Sprintf(`file %q is present but should not exist`, filename), filename)
}
