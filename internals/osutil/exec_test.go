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

package osutil_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	"github.com/canonical/tc"
	"gopkg.in/tomb.v2"

	"github.com/canonical/pebble/internals/osutil"
)

type execSuite struct{}

func TestExecSuite(t *testing.T) {
	tc.Run(t, &execSuite{})
}

func (s *execSuite) TestRunAndWaitRunsAndWaits(c *tc.C) {
	buf, err := osutil.RunAndWait([]string{"sh", "-c", "echo hello; sleep .1"}, nil, time.Second, &tomb.Tomb{})
	c.Assert(err, tc.IsNil)
	c.Check(string(buf), tc.Equals, "hello\n")
}

func (s *execSuite) TestRunAndWaitRunsSetsEnviron(c *tc.C) {
	buf, err := osutil.RunAndWait([]string{"sh", "-c", "echo $FOO"}, []string{"FOO=42"}, time.Second, &tomb.Tomb{})
	c.Assert(err, tc.IsNil)
	c.Check(string(buf), tc.Equals, "42\n")
}

func (s *execSuite) TestRunAndWaitRunsAndKillsOnTimeout(c *tc.C) {
	buf, err := osutil.RunAndWait([]string{"sleep", "1s"}, nil, time.Millisecond, &tomb.Tomb{})
	c.Check(err, tc.ErrorMatches, "exceeded maximum runtime.*")
	c.Check(string(buf), tc.Matches, "(?s).*exceeded maximum runtime.*")
}

func (s *execSuite) TestRunAndWaitRunsAndKillsOnAbort(c *tc.C) {
	tmb := &tomb.Tomb{}
	go func() {
		time.Sleep(10 * time.Millisecond)
		tmb.Kill(nil)
	}()
	buf, err := osutil.RunAndWait([]string{"sleep", "1s"}, nil, time.Second, tmb)
	c.Check(err, tc.ErrorMatches, "aborted.*")
	c.Check(string(buf), tc.Matches, "(?s).*aborted.*")
}

func (s *execSuite) TestRunAndWaitKillImpatient(c *tc.C) {
	defer osutil.FakeSyscallKill(func(int, syscall.Signal) error { return nil })()
	defer osutil.FakeCmdWaitTimeout(time.Millisecond)()

	buf, err := osutil.RunAndWait([]string{"sleep", "1s"}, nil, time.Millisecond, &tomb.Tomb{})
	c.Check(err, tc.ErrorMatches, ".* did not stop")
	c.Check(string(buf), tc.Equals, "")
}

func (s *execSuite) TestRunAndWaitExposesKillallError(c *tc.C) {
	defer osutil.FakeSyscallKill(func(p int, s syscall.Signal) error {
		syscall.Kill(p, s)
		return fmt.Errorf("xyzzy")
	})()
	defer osutil.FakeCmdWaitTimeout(time.Millisecond)()

	_, err := osutil.RunAndWait([]string{"sleep", "1s"}, nil, time.Millisecond, &tomb.Tomb{})
	c.Check(err, tc.ErrorMatches, "cannot abort: xyzzy")
}

func (s *execSuite) TestKillProcessGroupKillsProcessGroup(c *tc.C) {
	pid := 0
	ppid := 0
	defer osutil.FakeSyscallGetpgid(func(p int) (int, error) {
		ppid = p
		return syscall.Getpgid(p)
	})()
	defer osutil.FakeSyscallKill(func(p int, s syscall.Signal) error {
		pid = p
		return syscall.Kill(p, s)
	})()

	cmd := exec.Command("sleep", "1m")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Start()
	defer cmd.Process.Kill()

	err := osutil.KillProcessGroup(cmd)
	c.Assert(err, tc.IsNil)
	// process groups are passed to kill as negative numbers
	c.Check(pid, tc.Equals, -ppid)
}

func (s *execSuite) TestKillProcessGroupShyOfInit(c *tc.C) {
	defer osutil.FakeSyscallGetpgid(func(int) (int, error) { return 1, nil })()

	cmd := exec.Command("sleep", "1m")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Start()
	defer cmd.Process.Kill()

	err := osutil.KillProcessGroup(cmd)
	c.Assert(err, tc.ErrorMatches, "cannot kill pgid 1")
}

func (s *execSuite) TestStreamCommandHappy(c *tc.C) {
	var buf bytes.Buffer
	stdout, err := osutil.StreamCommand("sh", "-c", "echo hello; sleep .1; echo bye")
	c.Assert(err, tc.IsNil)
	_, err = io.Copy(&buf, stdout)
	c.Assert(err, tc.IsNil)
	c.Check(buf.String(), tc.Equals, "hello\nbye\n")

	wrf, wrc := osutil.WaitingReaderGuts(stdout)
	c.Assert(wrf, tc.FitsTypeOf, &os.File{})
	// Depending on golang version the error is one of the two.
	c.Check(wrf.(*os.File).Close(), tc.ErrorMatches, "invalid argument|file already closed")
	c.Check(wrc.ProcessState, tc.NotNil) // i.e. already waited for
}

func (s *execSuite) TestStreamCommandSad(c *tc.C) {
	var buf bytes.Buffer
	stdout, err := osutil.StreamCommand("false")
	c.Assert(err, tc.IsNil)
	_, err = io.Copy(&buf, stdout)
	c.Assert(err, tc.ErrorMatches, "exit status 1")
	c.Check(buf.String(), tc.Equals, "")

	wrf, wrc := osutil.WaitingReaderGuts(stdout)
	c.Assert(wrf, tc.FitsTypeOf, &os.File{})
	// Depending on golang version the error is one of the two.
	c.Check(wrf.(*os.File).Close(), tc.ErrorMatches, "invalid argument|file already closed")
	c.Check(wrc.ProcessState, tc.NotNil) // i.e. already waited for
}
