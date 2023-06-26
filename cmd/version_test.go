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
func createProcPid2Status(c *C, data string) string {
	path := filepath.Join(c.MkDir(), "status")
	err := ioutil.WriteFile(path, []byte(data), 0o644)
	c.Assert(err, IsNil)
	return path
}

func (s *cmdTestSuite) SetUpTest(c *C) {
	// Allow each test to trigger a check
	cmd.ResetContainerInit()
}

func (s *cmdTestSuite) TestContainerisedInvalidPath(c *C) {
	// This path is not valid so the test must therefore
	// assume the PID2 process does not exist, and therefore
	// we are inside a container. This may trigger a false
	// positive if /proc is not mounted before this is called.
	defer cmd.MockPid2ProcPath("/1/2/3/4/5")()
	c.Assert(cmd.Containerised(), Equals, true)
}

// TestContainerisedValidPath runs individual tests in the loop
// resetting before each test to prevent the sync.Once from
// loading a previously cached value.
func (s *cmdTestSuite) TestContainerisedValidPath(c *C) {

	for _, d := range []struct {
		status    string
		container bool
	}{
		// Note the /proc/<pid>/status format is:
		// <key>:\t<value>
		// The delimiter is a tab, not spaces.
		{`
Pid:	2
PPid:	0
Something:	32`, false},
		{`
Pid:	2
PPid:	1
Something:	32`, true},
		{`
something
1 2 3 4`, true},
	} {
		cmd.ResetContainerInit()
		path := createProcPid2Status(c, d.status)
		defer cmd.MockPid2ProcPath(path)()
		c.Assert(cmd.Containerised(), Equals, d.container)
	}
}

// TestContainerisedCaching ensures we do not redo detection as
// the container state could be used more than once in the codebase.
func (s *cmdTestSuite) TestContainerisedCaching(c *C) {
	// Note the /proc/<pid>/status format is:
	// <key>:\t<value>
	// The delimiter is a tab, not spaces.
	path := createProcPid2Status(c, `
Pid:	2
PPid:	0
Something:	32`)
	defer cmd.MockPid2ProcPath(path)()
	c.Assert(cmd.Containerised(), Equals, false)

	path = createProcPid2Status(c, `
Pid:	2
PPid:	1
Something:	32`)
	defer cmd.MockPid2ProcPath(path)()
	// This occurrence should not read the file, and return the cached value
	c.Assert(cmd.Containerised(), Equals, false)
}

// TestInitProcess checks if the init detection is plumbed in correctly.
func (s *cmdTestSuite) TestInitProcess(c *C) {
	defer cmd.MockPid(1234)()
	c.Assert(cmd.InitProcess(), Equals, false)
	defer cmd.MockPid(1)()
	c.Assert(cmd.InitProcess(), Equals, true)
}
