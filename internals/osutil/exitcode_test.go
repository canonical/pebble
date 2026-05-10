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
	"os"
	"os/exec"
	"testing"

	"github.com/canonical/tc"
)

type ExitCodeTestSuite struct{}

func TestExitCodeTestSuite(t *testing.T) {
	tc.Run(t, &ExitCodeTestSuite{})
}

func (ts *ExitCodeTestSuite) TestExitCode(c *tc.C) {
	cmd := exec.Command("true")
	err := cmd.Run()
	c.Assert(err, tc.ErrorIsNil)

	cmd = exec.Command("false")
	err = cmd.Run()
	c.Assert(err, tc.NotNil)
	e, err := ExitCode(err)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(e, tc.Equals, 1)

	cmd = exec.Command("sh", "-c", "exit 7")
	err = cmd.Run()
	e, err = ExitCode(err)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(e, tc.Equals, 7)

	// ensure that non exec.ExitError values give a error
	_, err = os.Stat("/random/file/that/is/not/there")
	c.Assert(err, tc.NotNil)
	_, err = ExitCode(err)
	c.Assert(err, tc.NotNil)
}
