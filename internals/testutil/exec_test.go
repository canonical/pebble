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

package testutil

import (
	"os/exec"
	"testing"

	"github.com/canonical/tc"
)

type fakeCommandSuite struct{}

func TestFakeCommandSuite(t *testing.T) {
	tc.Run(t, &fakeCommandSuite{})
}

func (s *fakeCommandSuite) TestFakeCommand(c *tc.C) {
	fake := FakeCommand(c, "cmd", "true")
	defer fake.Restore()
	err := exec.Command("cmd", "first-run", "--arg1", "arg2", "a space").Run()
	c.Assert(err, tc.IsNil)
	err = exec.Command("cmd", "second-run", "--arg1", "arg2", "a %s").Run()
	c.Assert(err, tc.IsNil)
	err = exec.Command("cmd", "third-run", "--arg1", "arg2", "").Run()
	c.Assert(err, tc.IsNil)
	err = exec.Command("cmd", "forth-run", "--arg1", "arg2", "", "a %s").Run()
	c.Assert(err, tc.IsNil)
	c.Assert(fake.Calls(), tc.DeepEquals, [][]string{
		{"cmd", "first-run", "--arg1", "arg2", "a space"},
		{"cmd", "second-run", "--arg1", "arg2", "a %s"},
		{"cmd", "third-run", "--arg1", "arg2", ""},
		{"cmd", "forth-run", "--arg1", "arg2", "", "a %s"},
	})
}

func (s *fakeCommandSuite) TestFakeCommandAlso(c *tc.C) {
	fake := FakeCommand(c, "fst", "")
	also := fake.Also("snd", "")
	defer fake.Restore()

	c.Assert(exec.Command("fst").Run(), tc.IsNil)
	c.Assert(exec.Command("snd").Run(), tc.IsNil)
	c.Check(fake.Calls(), tc.DeepEquals, [][]string{{"fst"}, {"snd"}})
	c.Check(fake.Calls(), tc.DeepEquals, also.Calls())
}

func (s *fakeCommandSuite) TestFakeCommandConflictEcho(c *tc.C) {
	fake := FakeCommand(c, "do-not-swallow-echo-args", "")
	defer fake.Restore()

	c.Assert(exec.Command("do-not-swallow-echo-args", "-E", "-n", "-e").Run(), tc.IsNil)
	c.Assert(fake.Calls(), tc.DeepEquals, [][]string{
		{"do-not-swallow-echo-args", "-E", "-n", "-e"},
	})
}
