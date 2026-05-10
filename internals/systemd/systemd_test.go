// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (c) 2014-2015 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package systemd_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/osutil"
	"github.com/canonical/pebble/internals/osutil/squashfs"
	"github.com/canonical/pebble/internals/systemd"
	"github.com/canonical/pebble/internals/testutil"
)

type testreporter struct {
	msgs []string
}

func (tr *testreporter) Notify(msg string) {
	tr.msgs = append(tr.msgs, msg)
}

// systemd's testsuite
type SystemdTestSuite struct {
	rootDir string

	i      int
	argses [][]string
	errors []error
	outs   [][]byte

	j        int
	jns      []string
	jsvcs    [][]string
	jouts    [][]byte
	jerrs    []error
	jfollows []bool

	rep *testreporter

	restoreServicesDir func()
	restoreSystemctl   func()
	restoreJournalctl  func()
}

func TestSystemdTestSuite(t *testing.T) {
	tc.Run(t, &SystemdTestSuite{})
}

func (s *SystemdTestSuite) SetUpTest(c *tc.C) {
	s.rootDir = c.MkDir()
	s.restoreServicesDir = systemd.FakeServicesDir(filepath.Join(s.rootDir, systemd.ServicesDir))
	err := os.MkdirAll(systemd.ServicesDir, 0755)
	c.Assert(err, tc.ErrorIsNil)

	// force UTC timezone, for reproducible timestamps
	os.Setenv("TZ", "")

	s.restoreSystemctl = systemd.FakeSystemctl(s.myRun)
	s.i = 0
	s.argses = nil
	s.errors = nil
	s.outs = nil

	s.restoreJournalctl = systemd.FakeJournalctl(s.myJctl)
	s.j = 0
	s.jns = nil
	s.jsvcs = nil
	s.jouts = nil
	s.jerrs = nil
	s.jfollows = nil

	s.rep = new(testreporter)
}

func (s *SystemdTestSuite) TearDownTest(c *tc.C) {
	s.restoreServicesDir()
	s.restoreSystemctl()
	s.restoreJournalctl()
}

func (s *SystemdTestSuite) myRun(args ...string) (out []byte, err error) {
	s.argses = append(s.argses, args)
	if s.i < len(s.outs) {
		out = s.outs[s.i]
	}
	if s.i < len(s.errors) {
		err = s.errors[s.i]
	}
	s.i++
	return out, err
}

func (s *SystemdTestSuite) myJctl(svcs []string, n int, follow bool) (io.ReadCloser, error) {
	var err error
	var out []byte

	s.jns = append(s.jns, strconv.Itoa(n))
	s.jsvcs = append(s.jsvcs, svcs)
	s.jfollows = append(s.jfollows, follow)

	if s.j < len(s.jouts) {
		out = s.jouts[s.j]
	}
	if s.j < len(s.jerrs) {
		err = s.jerrs[s.j]
	}
	s.j++

	if out == nil {
		return nil, err
	}

	return io.NopCloser(bytes.NewReader(out)), err
}

func (s *SystemdTestSuite) TestDaemonReload(c *tc.C) {
	err := systemd.New("", systemd.SystemMode, s.rep).DaemonReload()
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(s.argses, tc.DeepEquals, [][]string{{"daemon-reload"}})
}

func (s *SystemdTestSuite) TestStart(c *tc.C) {
	err := systemd.New("", systemd.SystemMode, s.rep).Start("foo")
	c.Assert(err, tc.ErrorIsNil)
	c.Check(s.argses, tc.DeepEquals, [][]string{{"start", "foo"}})
}

func (s *SystemdTestSuite) TestStartMany(c *tc.C) {
	err := systemd.New("", systemd.SystemMode, s.rep).Start("foo", "bar", "baz")
	c.Assert(err, tc.ErrorIsNil)
	c.Check(s.argses, tc.DeepEquals, [][]string{{"start", "foo", "bar", "baz"}})
}

func (s *SystemdTestSuite) TestStop(c *tc.C) {
	restore := systemd.FakeStopDelays(time.Millisecond, 25*time.Second)
	defer restore()
	s.outs = [][]byte{
		nil, // for the "stop" itself
		[]byte("ActiveState=whatever\n"),
		[]byte("ActiveState=active\n"),
		[]byte("ActiveState=inactive\n"),
	}
	s.errors = []error{nil, nil, nil, nil, &systemd.Timeout{}}
	err := systemd.New("", systemd.SystemMode, s.rep).Stop("foo", 1*time.Second)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(s.argses, tc.HasLen, 4)
	c.Check(s.argses[0], tc.DeepEquals, []string{"stop", "foo"})
	c.Check(s.argses[1], tc.DeepEquals, []string{"show", "--property=ActiveState", "foo"})
	c.Check(s.argses[1], tc.DeepEquals, s.argses[2])
	c.Check(s.argses[1], tc.DeepEquals, s.argses[3])
}

func (s *SystemdTestSuite) TestStatus(c *tc.C) {
	s.outs = [][]byte{
		[]byte(`
Type=simple
Id=foo.service
ActiveState=active
UnitFileState=enabled

Type=simple
Id=bar.service
ActiveState=reloading
UnitFileState=static

Type=potato
Id=baz.service
ActiveState=inactive
UnitFileState=disabled
`[1:]),
		[]byte(`
Id=some.timer
ActiveState=active
UnitFileState=enabled

Id=other.socket
ActiveState=active
UnitFileState=disabled
`[1:]),
	}
	s.errors = []error{nil}
	out, err := systemd.New("", systemd.SystemMode, s.rep).Status("foo.service", "bar.service", "baz.service", "some.timer", "other.socket")
	c.Assert(err, tc.ErrorIsNil)
	c.Check(out, tc.DeepEquals, []*systemd.UnitStatus{
		{
			Daemon:   "simple",
			UnitName: "foo.service",
			Active:   true,
			Enabled:  true,
		}, {
			Daemon:   "simple",
			UnitName: "bar.service",
			Active:   true,
			Enabled:  true,
		}, {
			Daemon:   "potato",
			UnitName: "baz.service",
			Active:   false,
			Enabled:  false,
		}, {
			UnitName: "some.timer",
			Active:   true,
			Enabled:  true,
		}, {
			UnitName: "other.socket",
			Active:   true,
			Enabled:  false,
		},
	})
	c.Check(s.rep.msgs, tc.IsNil)
	c.Assert(s.argses, tc.DeepEquals, [][]string{
		{"show", "--property=Id,ActiveState,UnitFileState,Type", "foo.service", "bar.service", "baz.service"},
		{"show", "--property=Id,ActiveState,UnitFileState", "some.timer", "other.socket"},
	})
}

func (s *SystemdTestSuite) TestStatusBadNumberOfValues(c *tc.C) {
	s.outs = [][]byte{
		[]byte(`
Type=simple
Id=foo.service
ActiveState=active
UnitFileState=enabled

Type=simple
Id=foo.service
ActiveState=active
UnitFileState=enabled
`[1:]),
	}
	s.errors = []error{nil}
	out, err := systemd.New("", systemd.SystemMode, s.rep).Status("foo.service")
	c.Check(err, tc.ErrorMatches, "cannot get unit status: expected 1 results, got 2")
	c.Check(out, tc.IsNil)
	c.Check(s.rep.msgs, tc.IsNil)
}

func (s *SystemdTestSuite) TestStatusBadLine(c *tc.C) {
	s.outs = [][]byte{
		[]byte(`
Type=simple
Id=foo.service
ActiveState=active
UnitFileState=enabled
Potatoes
`[1:]),
	}
	s.errors = []error{nil}
	out, err := systemd.New("", systemd.SystemMode, s.rep).Status("foo.service")
	c.Assert(err, tc.ErrorMatches, `.* bad line "Potatoes" .*`)
	c.Check(out, tc.IsNil)
}

func (s *SystemdTestSuite) TestStatusBadId(c *tc.C) {
	s.outs = [][]byte{
		[]byte(`
Type=simple
Id=bar.service
ActiveState=active
UnitFileState=enabled
`[1:]),
	}
	s.errors = []error{nil}
	out, err := systemd.New("", systemd.SystemMode, s.rep).Status("foo.service")
	c.Assert(err, tc.ErrorMatches, `.* queried status of "foo.service" but got status of "bar.service"`)
	c.Check(out, tc.IsNil)
}

func (s *SystemdTestSuite) TestStatusBadField(c *tc.C) {
	s.outs = [][]byte{
		[]byte(`
Type=simple
Id=foo.service
ActiveState=active
UnitFileState=enabled
Potatoes=false
`[1:]),
	}
	s.errors = []error{nil}
	out, err := systemd.New("", systemd.SystemMode, s.rep).Status("foo.service")
	c.Assert(err, tc.ErrorMatches, `.* unexpected field "Potatoes" .*`)
	c.Check(out, tc.IsNil)
}

func (s *SystemdTestSuite) TestStatusMissingRequiredFieldService(c *tc.C) {
	s.outs = [][]byte{
		[]byte(`
Id=foo.service
ActiveState=active
`[1:]),
	}
	s.errors = []error{nil}
	out, err := systemd.New("", systemd.SystemMode, s.rep).Status("foo.service")
	c.Assert(err, tc.ErrorMatches, `.* missing UnitFileState, Type .*`)
	c.Check(out, tc.IsNil)
}

func (s *SystemdTestSuite) TestStatusMissingRequiredFieldTimer(c *tc.C) {
	s.outs = [][]byte{
		[]byte(`
Id=foo.timer
ActiveState=active
`[1:]),
	}
	s.errors = []error{nil}
	out, err := systemd.New("", systemd.SystemMode, s.rep).Status("foo.timer")
	c.Assert(err, tc.ErrorMatches, `.* missing UnitFileState .*`)
	c.Check(out, tc.IsNil)
}

func (s *SystemdTestSuite) TestStatusDupeField(c *tc.C) {
	s.outs = [][]byte{
		[]byte(`
Type=simple
Id=foo.service
ActiveState=active
ActiveState=active
UnitFileState=enabled
`[1:]),
	}
	s.errors = []error{nil}
	out, err := systemd.New("", systemd.SystemMode, s.rep).Status("foo.service")
	c.Assert(err, tc.ErrorMatches, `.* duplicate field "ActiveState" .*`)
	c.Check(out, tc.IsNil)
}

func (s *SystemdTestSuite) TestStatusEmptyField(c *tc.C) {
	s.outs = [][]byte{
		[]byte(`
Type=simple
Id=
ActiveState=active
UnitFileState=enabled
`[1:]),
	}
	s.errors = []error{nil}
	out, err := systemd.New("", systemd.SystemMode, s.rep).Status("foo.service")
	c.Assert(err, tc.ErrorMatches, `.* empty field "Id" .*`)
	c.Check(out, tc.IsNil)
}

func (s *SystemdTestSuite) TestStopTimeout(c *tc.C) {
	restore := systemd.FakeStopDelays(time.Millisecond, 25*time.Second)
	defer restore()
	err := systemd.New("", systemd.SystemMode, s.rep).Stop("foo", 10*time.Millisecond)
	c.Assert(err, tc.FitsTypeOf, &systemd.Timeout{})
	c.Assert(len(s.rep.msgs) > 0, tc.Equals, true)
	c.Check(s.rep.msgs[0], tc.Equals, "Waiting for foo to stop.")
}

func (s *SystemdTestSuite) TestDisable(c *tc.C) {
	err := systemd.New("xyzzy", systemd.SystemMode, s.rep).Disable("foo")
	c.Assert(err, tc.ErrorIsNil)
	c.Check(s.argses, tc.DeepEquals, [][]string{{"--root", "xyzzy", "disable", "foo"}})
}

func (s *SystemdTestSuite) TestAvailable(c *tc.C) {
	err := systemd.Available()
	c.Assert(err, tc.ErrorIsNil)
	c.Check(s.argses, tc.DeepEquals, [][]string{{"--version"}})
}

func (s *SystemdTestSuite) TestEnable(c *tc.C) {
	err := systemd.New("xyzzy", systemd.SystemMode, s.rep).Enable("foo")
	c.Assert(err, tc.ErrorIsNil)
	c.Check(s.argses, tc.DeepEquals, [][]string{{"--root", "xyzzy", "enable", "foo"}})
}

func (s *SystemdTestSuite) TestMask(c *tc.C) {
	err := systemd.New("xyzzy", systemd.SystemMode, s.rep).Mask("foo")
	c.Assert(err, tc.ErrorIsNil)
	c.Check(s.argses, tc.DeepEquals, [][]string{{"--root", "xyzzy", "mask", "foo"}})
}

func (s *SystemdTestSuite) TestUnmask(c *tc.C) {
	err := systemd.New("xyzzy", systemd.SystemMode, s.rep).Unmask("foo")
	c.Assert(err, tc.ErrorIsNil)
	c.Check(s.argses, tc.DeepEquals, [][]string{{"--root", "xyzzy", "unmask", "foo"}})
}

func (s *SystemdTestSuite) TestRestart(c *tc.C) {
	restore := systemd.FakeStopDelays(time.Millisecond, 25*time.Second)
	defer restore()
	s.outs = [][]byte{
		nil, // for the "stop" itself
		[]byte("ActiveState=inactive\n"),
		nil, // for the "start"
	}
	s.errors = []error{nil, nil, nil, nil, &systemd.Timeout{}}
	err := systemd.New("", systemd.SystemMode, s.rep).Restart("foo", 100*time.Millisecond)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(s.argses, tc.HasLen, 3)
	c.Check(s.argses[0], tc.DeepEquals, []string{"stop", "foo"})
	c.Check(s.argses[1], tc.DeepEquals, []string{"show", "--property=ActiveState", "foo"})
	c.Check(s.argses[2], tc.DeepEquals, []string{"start", "foo"})
}

func (s *SystemdTestSuite) TestKill(c *tc.C) {
	c.Assert(systemd.New("", systemd.SystemMode, s.rep).Kill("foo", "HUP", ""), tc.IsNil)
	c.Check(s.argses, tc.DeepEquals, [][]string{{"kill", "foo", "-s", "HUP", "--kill-who=all"}})
}

func (s *SystemdTestSuite) TestIsTimeout(c *tc.C) {
	c.Check(systemd.IsTimeout(os.ErrInvalid), tc.Equals, false)
	c.Check(systemd.IsTimeout(&systemd.Timeout{}), tc.Equals, true)
}

func (s *SystemdTestSuite) TestLogErrJctl(c *tc.C) {
	s.jerrs = []error{&systemd.Timeout{}}

	reader, err := systemd.New("", systemd.SystemMode, s.rep).LogReader([]string{"foo"}, 24, false)
	c.Check(err, tc.NotNil)
	c.Check(reader, tc.IsNil)
	c.Check(s.jns, tc.DeepEquals, []string{"24"})
	c.Check(s.jsvcs, tc.DeepEquals, [][]string{{"foo"}})
	c.Check(s.jfollows, tc.DeepEquals, []bool{false})
	c.Check(s.j, tc.Equals, 1)
}

func (s *SystemdTestSuite) TestLogs(c *tc.C) {
	expected := `{"a": 1}
{"a": 2}
`
	s.jouts = [][]byte{[]byte(expected)}

	reader, err := systemd.New("", systemd.SystemMode, s.rep).LogReader([]string{"foo"}, 24, false)
	c.Assert(err, tc.ErrorIsNil)
	logs, err := io.ReadAll(reader)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(string(logs), tc.Equals, expected)
	c.Check(s.jns, tc.DeepEquals, []string{"24"})
	c.Check(s.jsvcs, tc.DeepEquals, [][]string{{"foo"}})
	c.Check(s.jfollows, tc.DeepEquals, []bool{false})
	c.Check(s.j, tc.Equals, 1)
}

func (s *SystemdTestSuite) TestLogPID(c *tc.C) {
	c.Check(systemd.Log{}.PID(), tc.Equals, "-")
	c.Check(systemd.Log{"_PID": "99"}.PID(), tc.Equals, "99")
	c.Check(systemd.Log{"SYSLOG_PID": "99"}.PID(), tc.Equals, "99")
	// things starting with underscore are "trusted", so we trust
	// them more than the user-settable ones:
	c.Check(systemd.Log{"_PID": "42", "SYSLOG_PID": "99"}.PID(), tc.Equals, "42")
}

func (s *SystemdTestSuite) TestTime(c *tc.C) {
	t, err := systemd.Log{}.Time()
	c.Check(t.IsZero(), tc.Equals, true)
	c.Check(err, tc.ErrorMatches, "no timestamp")

	t, err = systemd.Log{"__REALTIME_TIMESTAMP": "what"}.Time()
	c.Check(t.IsZero(), tc.Equals, true)
	c.Check(err, tc.ErrorMatches, `timestamp not a decimal number: "what"`)

	t, err = systemd.Log{"__REALTIME_TIMESTAMP": "0"}.Time()
	c.Assert(err, tc.ErrorIsNil)
	c.Check(t.String(), tc.Equals, "1970-01-01 00:00:00 +0000 UTC")

	t, err = systemd.Log{"__REALTIME_TIMESTAMP": "42"}.Time()
	c.Assert(err, tc.ErrorIsNil)
	c.Check(t.String(), tc.Equals, "1970-01-01 00:00:00.000042 +0000 UTC")
}

func (s *SystemdTestSuite) TestMountUnitPath(c *tc.C) {
	c.Assert(systemd.MountUnitPath("/apps/hello/1.1"), tc.Equals, filepath.Join(systemd.ServicesDir, "apps-hello-1.1.mount"))
}

func makeFakeFile(c *tc.C, path string) {
	err := os.MkdirAll(filepath.Dir(path), 0755)
	c.Assert(err, tc.ErrorIsNil)
	err = os.WriteFile(path, nil, 0644)
	c.Assert(err, tc.ErrorIsNil)
}

func (s *SystemdTestSuite) TestAddMountUnit(c *tc.C) {
	restore := squashfs.FakeUseFuse(false)
	defer restore()

	fakeSnapPath := filepath.Join(c.MkDir(), "/var/lib/snappy/snaps/foo_1.0.snap")
	makeFakeFile(c, fakeSnapPath)

	mountUnitName, err := systemd.New(s.rootDir, systemd.SystemMode, nil).AddMountUnitFile("foo", "42", fakeSnapPath, "/snap/snapname/123", "squashfs")
	c.Assert(err, tc.ErrorIsNil)
	defer os.Remove(mountUnitName)

	c.Assert(filepath.Join(systemd.ServicesDir, mountUnitName), testutil.FileEquals, fmt.Sprintf(`
[Unit]
Description=Mount unit for foo, revision 42
Before=snapd.service

[Mount]
What=%s
Where=/snap/snapname/123
Type=squashfs
Options=nodev,ro,x-gdu.hide

[Install]
WantedBy=multi-user.target
`[1:], fakeSnapPath))

	c.Assert(s.argses, tc.DeepEquals, [][]string{
		{"daemon-reload"},
		{"--root", s.rootDir, "enable", "snap-snapname-123.mount"},
		{"start", "snap-snapname-123.mount"},
	})
}

func (s *SystemdTestSuite) TestAddMountUnitForDirs(c *tc.C) {
	restore := squashfs.FakeUseFuse(false)
	defer restore()

	// a directory instead of a file produces a different output
	snapDir := c.MkDir()
	mountUnitName, err := systemd.New("", systemd.SystemMode, nil).AddMountUnitFile("foodir", "x1", snapDir, "/snap/snapname/x1", "squashfs")
	c.Assert(err, tc.ErrorIsNil)
	defer os.Remove(mountUnitName)

	c.Assert(filepath.Join(systemd.ServicesDir, mountUnitName), testutil.FileEquals, fmt.Sprintf(`
[Unit]
Description=Mount unit for foodir, revision x1
Before=snapd.service

[Mount]
What=%s
Where=/snap/snapname/x1
Type=none
Options=nodev,ro,x-gdu.hide,bind

[Install]
WantedBy=multi-user.target
`[1:], snapDir))

	c.Assert(s.argses, tc.DeepEquals, [][]string{
		{"daemon-reload"},
		{"--root", "", "enable", "snap-snapname-x1.mount"},
		{"start", "snap-snapname-x1.mount"},
	})
}

func (s *SystemdTestSuite) TestFuseInContainer(c *tc.C) {
	if !osutil.CanStat("/dev/fuse") {
		c.Skip("No /dev/fuse on the system")
	}

	systemdCmd := testutil.FakeCommand(c, "systemd-detect-virt", `
echo lxc
exit 0
	`)
	defer systemdCmd.Restore()

	fuseCmd := testutil.FakeCommand(c, "squashfuse", `
exit 0
	`)
	defer fuseCmd.Restore()

	fakeSnapPath := filepath.Join(c.MkDir(), "/var/lib/snappy/snaps/foo_1.0.snap")
	err := os.MkdirAll(filepath.Dir(fakeSnapPath), 0755)
	c.Assert(err, tc.ErrorIsNil)
	err = os.WriteFile(fakeSnapPath, nil, 0644)
	c.Assert(err, tc.ErrorIsNil)

	mountUnitName, err := systemd.New("", systemd.SystemMode, nil).AddMountUnitFile("foo", "x1", fakeSnapPath, "/snap/snapname/123", "squashfs")
	c.Assert(err, tc.ErrorIsNil)
	defer os.Remove(mountUnitName)

	c.Check(filepath.Join(systemd.ServicesDir, mountUnitName), testutil.FileEquals, fmt.Sprintf(`
[Unit]
Description=Mount unit for foo, revision x1
Before=snapd.service

[Mount]
What=%s
Where=/snap/snapname/123
Type=fuse.squashfuse
Options=nodev,ro,x-gdu.hide,allow_other

[Install]
WantedBy=multi-user.target
`[1:], fakeSnapPath))
}

func (s *SystemdTestSuite) TestFuseOutsideContainer(c *tc.C) {
	systemdCmd := testutil.FakeCommand(c, "systemd-detect-virt", `
echo none
exit 0
	`)
	defer systemdCmd.Restore()

	fuseCmd := testutil.FakeCommand(c, "squashfuse", `
exit 0
	`)
	defer fuseCmd.Restore()

	fakeSnapPath := filepath.Join(c.MkDir(), "/var/lib/snappy/snaps/foo_1.0.snap")
	err := os.MkdirAll(filepath.Dir(fakeSnapPath), 0755)
	c.Assert(err, tc.ErrorIsNil)
	err = os.WriteFile(fakeSnapPath, nil, 0644)
	c.Assert(err, tc.ErrorIsNil)

	mountUnitName, err := systemd.New("", systemd.SystemMode, nil).AddMountUnitFile("foo", "x1", fakeSnapPath, "/snap/snapname/123", "squashfs")
	c.Assert(err, tc.ErrorIsNil)
	defer os.Remove(mountUnitName)

	c.Assert(filepath.Join(systemd.ServicesDir, mountUnitName), testutil.FileEquals, fmt.Sprintf(`
[Unit]
Description=Mount unit for foo, revision x1
Before=snapd.service

[Mount]
What=%s
Where=/snap/snapname/123
Type=squashfs
Options=nodev,ro,x-gdu.hide

[Install]
WantedBy=multi-user.target
`[1:], fakeSnapPath))
}

func (s *SystemdTestSuite) TestJctl(c *tc.C) {
	var args []string
	var err error
	systemd.FakeOsutilStreamCommand(func(name string, myargs ...string) (io.ReadCloser, error) {
		c.Check(cap(myargs) <= len(myargs)+2, tc.Equals, true, tc.Commentf("cap:%d, len:%d", cap(myargs), len(myargs)))
		args = myargs
		return nil, nil
	})

	_, err = systemd.Jctl([]string{"foo", "bar"}, 10, false)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(args, tc.DeepEquals, []string{"-o", "json", "--no-pager", "-n", "10", "-u", "foo", "-u", "bar"})
	_, err = systemd.Jctl([]string{"foo", "bar", "baz"}, 99, true)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(args, tc.DeepEquals, []string{"-o", "json", "--no-pager", "-n", "99", "-f", "-u", "foo", "-u", "bar", "-u", "baz"})
	_, err = systemd.Jctl([]string{"foo", "bar"}, -1, false)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(args, tc.DeepEquals, []string{"-o", "json", "--no-pager", "--no-tail", "-u", "foo", "-u", "bar"})
}

func (s *SystemdTestSuite) TestIsActiveIsInactive(c *tc.C) {
	sysErr := &systemd.Error{}
	sysErr.SetExitCode(1)
	sysErr.SetMsg([]byte("inactive\n"))
	s.errors = []error{sysErr}

	active, err := systemd.New("xyzzy", systemd.SystemMode, s.rep).IsActive("foo")
	c.Assert(active, tc.Equals, false)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(s.argses, tc.DeepEquals, [][]string{{"--root", "xyzzy", "is-active", "foo"}})
}

func (s *SystemdTestSuite) TestIsActiveIsActive(c *tc.C) {
	s.errors = []error{nil}

	active, err := systemd.New("xyzzy", systemd.SystemMode, s.rep).IsActive("foo")
	c.Assert(active, tc.Equals, true)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(s.argses, tc.DeepEquals, [][]string{{"--root", "xyzzy", "is-active", "foo"}})
}

func (s *SystemdTestSuite) TestIsActiveErr(c *tc.C) {
	sysErr := &systemd.Error{}
	sysErr.SetExitCode(1)
	sysErr.SetMsg([]byte("random-failure\n"))
	s.errors = []error{sysErr}

	active, err := systemd.New("xyzzy", systemd.SystemMode, s.rep).IsActive("foo")
	c.Assert(active, tc.Equals, false)
	c.Assert(err, tc.ErrorMatches, ".* failed with exit status 1: random-failure\n")
}

func makeFakeMountUnit(c *tc.C, mountDir string) string {
	mountUnit := systemd.MountUnitPath(mountDir)
	err := os.WriteFile(mountUnit, nil, 0644)
	c.Assert(err, tc.ErrorIsNil)
	return mountUnit
}

// FIXME: also test for the "IsMounted" case
func (s *SystemdTestSuite) TestRemoveMountUnit(c *tc.C) {
	mountDir := s.rootDir + "/snap/foo/42"
	mountUnit := makeFakeMountUnit(c, "/snap/foo/42")
	err := systemd.New(s.rootDir, systemd.SystemMode, nil).RemoveMountUnitFile(mountDir)
	c.Assert(err, tc.ErrorIsNil)

	// the file is gone
	c.Check(osutil.CanStat(mountUnit), tc.Equals, false)
	// and the unit is disabled and the daemon reloaded
	c.Check(s.argses, tc.DeepEquals, [][]string{
		{"--root", s.rootDir, "disable", "snap-foo-42.mount"},
		{"daemon-reload"},
	})
}

func (s *SystemdTestSuite) TestDaemonReloadMutex(c *tc.C) {
	sysd := systemd.New(s.rootDir, systemd.SystemMode, nil)

	fakeSnapPath := filepath.Join(c.MkDir(), "/var/lib/snappy/snaps/foo_1.0.snap")
	makeFakeFile(c, fakeSnapPath)

	// create a go-routine that will try to daemon-reload like crazy
	stopCh := make(chan bool, 1)
	stoppedCh := make(chan bool, 1)
	go func() {
		for {
			sysd.DaemonReload()
			select {
			case <-stopCh:
				close(stoppedCh)
				return
			default:
				//pass
			}
		}
	}()

	// tc.And now add a mount unit file while the go-routine tries to
	// daemon-reload. This will be serialized, if not this would
	// panic because systemd.daemonReloadNoLock ensures the lock is
	// taken when this happens.
	_, err := sysd.AddMountUnitFile("foo", "42", fakeSnapPath, "/snap/foo/42", "squashfs")
	c.Assert(err, tc.ErrorIsNil)
	close(stopCh)
	<-stoppedCh
}

func (s *SystemdTestSuite) TestUserMode(c *tc.C) {
	sysd := systemd.New(s.rootDir, systemd.UserMode, nil)

	c.Assert(sysd.Enable("foo"), tc.IsNil)
	c.Check(s.argses[0], tc.DeepEquals, []string{"--user", "--root", s.rootDir, "enable", "foo"})
	c.Assert(sysd.Start("foo"), tc.IsNil)
	c.Check(s.argses[1], tc.DeepEquals, []string{"--user", "start", "foo"})
}

func (s *SystemdTestSuite) TestGlobalUserMode(c *tc.C) {
	sysd := systemd.New(s.rootDir, systemd.GlobalUserMode, nil)

	c.Assert(sysd.Enable("foo"), tc.IsNil)
	c.Check(s.argses[0], tc.DeepEquals, []string{"--user", "--global", "--root", s.rootDir, "enable", "foo"})
	c.Assert(sysd.Disable("foo"), tc.IsNil)
	c.Check(s.argses[1], tc.DeepEquals, []string{"--user", "--global", "--root", s.rootDir, "disable", "foo"})
	c.Assert(sysd.Mask("foo"), tc.IsNil)
	c.Check(s.argses[2], tc.DeepEquals, []string{"--user", "--global", "--root", s.rootDir, "mask", "foo"})
	c.Assert(sysd.Unmask("foo"), tc.IsNil)
	c.Check(s.argses[3], tc.DeepEquals, []string{"--user", "--global", "--root", s.rootDir, "unmask", "foo"})

	// Commands that don't make sense for GlobalUserMode panic
	c.Check(sysd.DaemonReload, tc.Panics, "cannot call daemon-reload with GlobalUserMode")
	c.Check(func() { sysd.Start("foo") }, tc.Panics, "cannot call start with GlobalUserMode")
	c.Check(func() { sysd.StartNoBlock("foo") }, tc.Panics, "cannot call start with GlobalUserMode")
	c.Check(func() { sysd.Stop("foo", 0) }, tc.Panics, "cannot call stop with GlobalUserMode")
	c.Check(func() { sysd.Restart("foo", 0) }, tc.Panics, "cannot call restart with GlobalUserMode")
	c.Check(func() { sysd.Kill("foo", "HUP", "") }, tc.Panics, "cannot call kill with GlobalUserMode")
	c.Check(func() { sysd.Status("foo") }, tc.Panics, "cannot call status with GlobalUserMode")
	c.Check(func() { sysd.IsEnabled("foo") }, tc.Panics, "cannot call is-enabled with GlobalUserMode")
	c.Check(func() { sysd.IsActive("foo") }, tc.Panics, "cannot call is-active with GlobalUserMode")
}
