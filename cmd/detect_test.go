// Copyright (c) 2014-2023 Canonical Ltd
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

package cmd_test

import (
	"io/fs"
	"io/ioutil"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/cmd"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type cmdTestSuite struct{}

var _ = Suite(&cmdTestSuite{})

// createProcPid2Status creates a /proc/<pid>/status file.
func createProcPid2Status(c *C, data string, perm fs.FileMode) string {
	path := filepath.Join(c.MkDir(), "status")
	err := ioutil.WriteFile(path, []byte(data), perm)
	c.Assert(err, IsNil)
	return path
}

func (s *cmdTestSuite) TestNoKernelPathNotFound(c *C) {
	defer cmd.MockPid2ProcPath("/1/2/3/4/5")()
	v, err := cmd.NoKernel()
	// We expect true because we cannot "see" the kernel.
	// As stated in the function description, this is one
	// of the expected cases, because we want to support
	// systems without /proc mounted.
	c.Assert(v, Equals, true)
	c.Assert(err, IsNil)
}

func (s *cmdTestSuite) TestNoKernelPathError(c *C) {
	path := createProcPid2Status(c, "", 0o000)
	defer cmd.MockPid2ProcPath(path)()
	v, err := cmd.NoKernel()
	c.Assert(v, Equals, false)
	c.Assert(err, ErrorMatches, "*permission denied")
}

func (s *cmdTestSuite) TestNoKernelValidPath(c *C) {

	for _, d := range []struct {
		status    string
		container bool
		err       string
	}{
		// Note the /proc/<pid>/status format is:
		// <key>:\t<value>
		// The delimiter is a tab, not spaces.
		{`
Pid:	2
PPid:	0
Something:	32`, false, ""},
		{`
Pid:	2
PPid:	1
Something:	32`, true, ""},
		{`
Pid:	2
PPid:	str
Something:	32`, false, "*invalid syntax*"},
		{`
something
1 2 3 4`, true, ""},
	} {
		cmd.ResetContainerInit()
		path := createProcPid2Status(c, d.status, 0o644)
		defer cmd.MockPid2ProcPath(path)()
		v, err := cmd.NoKernel()
		c.Assert(v, Equals, d.container)
		if d.err == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, d.err)
		}
	}
}
