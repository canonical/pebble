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
	"testing"

	"github.com/canonical/tc"
)

type outputErrSuite struct{}

func TestOutputErrSuite(t *testing.T) {
	tc.Run(t, &outputErrSuite{})
}

func (ts *outputErrSuite) TestOutputErrOutputWithoutNewlines(c *tc.C) {
	output := "test output"
	err := fmt.Errorf("test error")
	formattedErr := OutputErr([]byte(output), err)
	c.Check(formattedErr, tc.ErrorMatches, output)
}

func (ts *outputErrSuite) TestOutputErrOutputWithNewlines(c *tc.C) {
	output := "output line1\noutput line2"
	err := fmt.Errorf("test error")
	formattedErr := OutputErr([]byte(output), err)
	c.Check(formattedErr.Error(), tc.Equals, `
-----
output line1
output line2
-----`)
}

func (ts *outputErrSuite) TestOutputErrNoOutput(c *tc.C) {
	err := fmt.Errorf("test error")
	formattedErr := OutputErr([]byte{}, err)
	c.Check(formattedErr, tc.Equals, err)
}
