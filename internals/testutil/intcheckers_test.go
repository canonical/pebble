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
	"testing"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/testutil"
)

type intCheckersSuite struct{}

func TestIntCheckersSuite(t *testing.T) {
	tc.Run(t, &intCheckersSuite{})
}

func (*intCheckersSuite) TestIntChecker(c *tc.C) {
	c.Assert(1, testutil.IntLessThan, 2)
	c.Assert(1, testutil.IntLessEqual, 1)
	c.Assert(1, testutil.IntEqual, 1)
	c.Assert(2, testutil.IntNotEqual, 1)
	c.Assert(2, testutil.IntGreaterThan, 1)
	c.Assert(2, testutil.IntGreaterEqual, 2)

	// Wrong argument types.
	testCheck(c, testutil.IntLessThan, false, "left-hand-side argument must be an int", false, 1)
	testCheck(c, testutil.IntLessThan, false, "right-hand-side argument must be an int", 1, false)

	// Relationship error.
	testCheck(c, testutil.IntLessThan, false, "relation 2 < 1 is not true", 2, 1)
	testCheck(c, testutil.IntLessEqual, false, "relation 2 <= 1 is not true", 2, 1)
	testCheck(c, testutil.IntEqual, false, "relation 2 == 1 is not true", 2, 1)
	testCheck(c, testutil.IntNotEqual, false, "relation 2 != 2 is not true", 2, 2)
	testCheck(c, testutil.IntGreaterThan, false, "relation 1 > 2 is not true", 1, 2)
	testCheck(c, testutil.IntGreaterEqual, false, "relation 1 >= 2 is not true", 1, 2)

	// Unexpected relation.
	unexpected := testutil.UnexpectedIntChecker("===")
	testCheck(c, unexpected, false, `unexpected relation "==="`, 1, 2)
}
