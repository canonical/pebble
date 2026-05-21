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

package progress_test

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/progress"
	"github.com/canonical/pebble/internals/progress/progresstest"
)

type ProgressTestSuite struct {
}

func TestProgressTestSuite(t *testing.T) {
	tc.Run(t, &ProgressTestSuite{})
}

func (ts *ProgressTestSuite) testNotify(c *tc.C, t progress.Meter, desc, expected string) {
	var buf bytes.Buffer
	defer progress.MockStdout(&buf)()

	comment := tc.Commentf(desc)

	t.Notify("blah blah")

	c.Check(buf.String(), tc.Equals, expected, comment)
}

func (ts *ProgressTestSuite) TestQuietNotify(c *tc.C) {
	ts.testNotify(c, &progress.QuietMeter{}, "quiet", "blah blah\n")
}

func (ts *ProgressTestSuite) TestANSINotify(c *tc.C) {
	expected := fmt.Sprint("\r", progress.ExitAttributeMode, progress.ClrEOL, "blah blah\n")
	ts.testNotify(c, &progress.ANSIMeter{}, "ansi", expected)
}

func (ts *ProgressTestSuite) TestTestingNotify(c *tc.C) {
	p := &progresstest.Meter{}
	ts.testNotify(c, p, "test", "")
	c.Check(p.Notices, tc.DeepEquals, []string{"blah blah"})
}
