// Copyright (c) 2014-2024 Canonical Ltd
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

package servstate_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/overlord/checkstate"
	"github.com/canonical/pebble/internals/overlord/restart"
	"github.com/canonical/pebble/internals/overlord/servstate"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/reaper"
	"github.com/canonical/pebble/internals/servicelog"
	"github.com/canonical/pebble/internals/testutil"
)

const (
	shortOkayDelay = 50 * time.Millisecond
	shortKillDelay = 100 * time.Millisecond
	shortFailDelay = 100 * time.Millisecond
)

func TestMain(m *testing.M) {
	// Used by TestReapZombies
	if os.Getenv("PEBBLE_TEST_CREATE_CHILD") == "1" {
		err := createZombie()
		if err != nil {
			fmt.Fprintf(os.Stderr, "cannot create zombie: %v\n", err)
			os.Exit(1)
		}
		donePath := os.Getenv("PEBBLE_TEST_EXIT_CHILD")
		if donePath == "" {
			fmt.Fprintf(os.Stderr, "PEBBLE_TEST_EXIT_CHILD must be set\n")
			os.Exit(1)
		}
		// Wait until the test signals us to exit the child process. This
		// will transfer ownership of the grandchild to the parent which
		// runs the reaper. Until we trigger this, the grandchild will be in
		// zombie status (it exits immediately) because this child process
		// had no thread wait()-ing to reap the grandchild.
		waitForDone(donePath, func() {
			fmt.Fprintf(os.Stderr, "timed out waiting for exit signal\n")
			os.Exit(1)
		})

		return
	} else if os.Getenv("PEBBLE_TEST_CREATE_GRANDCHILD") == "1" {
		// The moment the grandchild returns it will be in zombie status
		// until some parent reaps it.
		return
	}

	// Used by TestWaitDelay
	if os.Getenv("PEBBLE_TEST_WAITDELAY") == "1" {
		// To get WaitDelay to kick in, we need to start a new process with
		// setsid (to ensure it has a new process group ID) and passing
		// os.Stdout down to the (grand)child process.
		cmd := exec.Command("sleep", "10")
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		cmd.Stdout = os.Stdout
		err := cmd.Run()
		if err != nil {
			panic(err)
		}
		return
	}

	os.Exit(m.Run())
}

func Test(t *testing.T) { TestingT(t) }

type S struct {
	testutil.BaseTest

	// Unique tmp directory for each test
	dir string

	logBuffer    bytes.Buffer
	logBufferMut sync.Mutex
	logOutput    writerFunc

	st *state.State

	manager    *servstate.ServiceManager
	runner     *state.TaskRunner
	stopDaemon chan restart.RestartType

	donePath       string
	plan           *plan.Plan
	planPropagated bool
}

var _ = Suite(&S{})

var setLoggerOnce sync.Once

func (s *S) SetUpSuite(c *C) {
	// This can happen in parallel with tests if -test.count=N with N>1 is specified.
	setLoggerOnce.Do(func() {
		logger.SetLogger(logger.New(os.Stderr, "[test] "))
	})
}

func (s *S) SetUpTest(c *C) {
	err := reaper.Start()
	if err != nil {
		c.Fatalf("cannot start reaper: %v", err)
	}

	s.BaseTest.SetUpTest(c)

	s.dir = c.MkDir()
	s.st = state.New(nil)

	s.logBufferMut.Lock()
	s.logBuffer.Reset()
	s.logBufferMut.Unlock()
	s.logOutput = writerFunc(func(p []byte) (int, error) {
		s.logBufferMut.Lock()
		defer s.logBufferMut.Unlock()
		return s.logBuffer.Write(p)
	})

	s.runner = state.NewTaskRunner(s.st)
	s.stopDaemon = make(chan restart.RestartType, 1)

	restore := servstate.FakeOkayWait(shortOkayDelay)
	s.AddCleanup(restore)
	restore = servstate.FakeKillFailDelay(shortKillDelay, shortFailDelay)
	s.AddCleanup(restore)

	s.plan = &plan.Plan{}
	s.planPropagated = false
	s.manager = nil
}

func (s *S) TearDownTest(c *C) {
	// Not all tests starts a service manager
	if s.manager != nil {
		if s.planPropagated {
			// Only stop if PlanChanged was ever called on the
			// service manager.
			s.stopRunningServices(c)
		}
	}
	// General test cleanup
	s.BaseTest.TearDownTest(c)

	err := reaper.Stop()
	if err != nil {
		c.Fatalf("cannot stop reaper: %v", err)
	}
}

// TestPlanLayer and other plan snippets in this test package use the done
// check mechanism to have a predictable way to know when the expected service
// side effect (e.g. stdout or stderr) can be checked. This is required
// because the service manager can only tell when the service started, not
// when the expected result has been achieved. The done check barrier is
// inserted by adding the {{.NotifyDoneCheck}} placeholder in the
// service command sequence, at the point where the expected side effect
// will be observed by the test result checker.
var testPlanLayer = `
services:
    test1:
        override: replace
        command: /bin/sh -c "echo test1; {{.NotifyDoneCheck}}; sleep 10"
        startup: enabled
        requires:
            - test2
        before:
            - test2

    test2:
        override: replace
        command: /bin/sh -c "echo test2; {{.NotifyDoneCheck}}; sleep 10"

    test3:
        override: replace
        command: some-bad-command

    test4:
        override: replace
        command: echo -e 'too-fast\nsecond line'

    test5:
        override: replace
        command: /bin/sh -c "sleep 10"
        user: nobody
        group: nogroup
`

func (s *S) TestDefaultServiceNames(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)
	s.planChanged(c)

	services, err := s.manager.DefaultServiceNames()
	c.Assert(err, IsNil)
	c.Assert(services, DeepEquals, []string{"test1", "test2"})
}

func (s *S) TestStartStopServices(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)
	s.planChanged(c)

	s.startTestServices(c, true)

	if c.Failed() {
		return
	}

	s.stopTestServices(c)
}

func (s *S) TestStartStopServicesIdempotency(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)
	s.planChanged(c)

	s.startTestServices(c, true)
	if c.Failed() {
		return
	}

	// Do not check the logs again, the service
	// is not actually restarted here.
	s.startTestServices(c, false)
	if c.Failed() {
		return
	}

	s.stopTestServices(c)
	if c.Failed() {
		return
	}

	s.stopTestServicesAlreadyDead(c)
}

func (s *S) TestStopTimeout(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)
	s.planAddLayer(c, `
services:
    test9:
        override: merge
        command: /bin/bash -c "sleep 20;"
        kill-delay: 1h
    test10:
        override: merge
        command: /bin/bash -c "sleep 20;"
        kill-delay: 2h
`)
	s.planChanged(c)

	_, _, err := s.manager.Replan()
	c.Assert(err, IsNil)
	s.startServices(c, [][]string{{"test9"}})
	s.waitUntilService(c, "test9", func(service *servstate.ServiceInfo) bool {
		return service.Current == servstate.StatusActive
	})
	c.Assert(s.manager.StopTimeout(), Equals, time.Minute*60+time.Millisecond*200)
}

func (s *S) TestStopServiceWithinOkayDelay(c *C) {
	// A longer okayDelay is used so that the change for starting the service won't
	// quickly transition into the running state.
	fakeOkayDelay := 20 * shortOkayDelay
	servstate.FakeOkayWait(fakeOkayDelay)

	s.newServiceManager(c)
	serviceName := "test-stop-within-okaywait"
	// The service sleeps for fakeOkayDelay second then creates a side effect (a file at donePath).
	layer := `
services:
    %s:
        override: replace
        command: /bin/sh -c "sleep %g; {{.NotifyDoneCheck}}"
`
	s.planAddLayer(c, fmt.Sprintf(layer, serviceName, fakeOkayDelay.Seconds()))
	s.planChanged(c)

	// Start the service without waiting for change ready.
	s.st.Lock()
	ts, err := servstate.Start(s.st, [][]string{{serviceName}})
	c.Check(err, IsNil)
	chgStart := s.st.NewChange("test", "Start test")
	chgStart.AddAll(ts)
	s.st.Unlock()
	s.runner.Ensure()

	// Wait until the service is in the starting state.
	s.waitUntilService(c, serviceName, func(service *servstate.ServiceInfo) bool {
		return service.Current == servstate.StatusActive
	})

	// Stop the service within okayDelay.
	chg := s.stopServices(c, [][]string{{serviceName}})
	s.st.Lock()
	c.Assert(chg.Err(), IsNil)
	s.st.Unlock()

	waitChangeReady(c, s.runner, chgStart, "Start test")

	s.st.Lock()
	c.Check(chgStart.Status(), Equals, state.ErrorStatus)
	c.Check(chgStart.Err(), ErrorMatches, fmt.Sprintf(`(?s).*stopped before the %s okay delay.*`, fakeOkayDelay))
	s.st.Unlock()

	donePath := filepath.Join(s.dir, serviceName)
	// If the service is stopped within okayDelay and is indeed terminated, donePath should not exist.
	if _, err := os.Stat(donePath); err == nil {
		c.Fatalf("service %s waiting for service output", serviceName)
	}
}

func (s *S) TestKillDelayIsUsed(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)
	s.planAddLayer(c, `
services:
    test6:
        override: merge
        command: /bin/bash -c "trap 'sleep 10' SIGTERM; sleep 20;"
        kill-delay: 300ms
`)
	s.planChanged(c)

	_, _, err := s.manager.Replan()
	c.Assert(err, IsNil)

	s.startServices(c, [][]string{{"test6"}})
	s.waitUntilService(c, "test6", func(service *servstate.ServiceInfo) bool {
		return service.Current == servstate.StatusActive
	})

	startTime := time.Now()
	chg := s.stopServices(c, [][]string{{"test6"}})
	s.st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("Error: %v", chg.Err()))
	s.st.Unlock()
	s.waitUntilService(c, "test6", func(service *servstate.ServiceInfo) bool {
		if service.Current == servstate.StatusInactive {
			c.Assert(time.Now().Sub(startTime) > time.Millisecond*300, Equals, true)
			return true
		}
		return false
	})
}

func (s *S) TestReplanServices(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)
	s.planChanged(c)

	s.startTestServices(c, true)
	if c.Failed() {
		return
	}

	s.planAddLayer(c, `
services:
    test2:
        override: merge
        command: /bin/sh -c "echo test2b; sleep 10"
`)
	s.planChanged(c)

	stops, starts, err := s.manager.Replan()
	c.Assert(err, IsNil)
	c.Check(stops, DeepEquals, [][]string{{"test2", "test1"}})
	c.Check(starts, DeepEquals, [][]string{{"test1", "test2"}})

	s.stopTestServices(c)
}

func (s *S) TestReplanUpdatesConfig(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)
	s.planChanged(c)

	s.startTestServices(c, true)
	defer s.stopTestServices(c)

	// Ensure the ServiceManager's config reflects the plan config.
	config := s.manager.Config("test2")
	c.Assert(config, NotNil)
	c.Assert(config.OnSuccess, Equals, plan.ActionUnset)
	c.Assert(config.Summary, Equals, "")
	command := config.Command

	// Add a layer and override a couple of values.
	s.planAddLayer(c, `
services:
    test2:
        override: merge
        summary: A summary!
        on-success: ignore
`)
	s.planChanged(c)

	// Call Replan and ensure the ServiceManager's config has updated.
	_, _, err := s.manager.Replan()
	c.Assert(err, IsNil)
	config = s.manager.Config("test2")
	c.Assert(config, NotNil)
	c.Check(config.OnSuccess, Equals, plan.ActionIgnore)
	c.Check(config.Summary, Equals, "A summary!")
	c.Check(config.Command, Equals, command)
}

func (s *S) TestStopStartUpdatesConfig(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)
	s.planChanged(c)

	s.startTestServices(c, true)
	defer s.stopTestServices(c)

	// Add a layer and override a couple of values.
	s.planAddLayer(c, `
services:
    test2:
        override: merge
        summary: A summary!
        on-success: ignore
`)
	s.planChanged(c)

	// Call Stop and Start and ensure the ServiceManager's config has updated.
	s.stopTestServices(c)
	s.startTestServices(c, true)
	config := s.manager.Config("test2")
	c.Assert(config, NotNil)
	c.Check(config.OnSuccess, Equals, plan.ActionIgnore)
	c.Check(config.Summary, Equals, "A summary!")
}

func (s *S) TestServiceLogs(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)
	s.planChanged(c)

	outputs := map[string]string{
		"test1": `2.* \[test1\] test1\n`,
		"test2": `2.* \[test2\] test2\n`,
	}
	s.testServiceLogs(c, outputs)

	// Run test again, but ensure the logs from the previous run are still in the ring buffer.
	outputs["test1"] += outputs["test1"]
	outputs["test2"] += outputs["test2"]
	s.testServiceLogs(c, outputs)
}

func (s *S) TestStartBadCommand(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)
	s.planChanged(c)

	chg := s.startServices(c, [][]string{{"test3"}})

	s.st.Lock()
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `(?s).*cannot start.*"some-bad-command":.*not found.*`)
	s.st.Unlock()

	svc := s.serviceByName(c, "test3")
	c.Assert(svc.Current, Equals, servstate.StatusInactive)
}

func (s *S) TestCurrentUserGroup(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)

	current, err := user.Current()
	c.Assert(err, IsNil)
	group, err := user.LookupGroupId(current.Gid)
	c.Assert(err, IsNil)

	outputPath := filepath.Join(c.MkDir(), "output")
	layer := `
services:
    usrtest:
        override: replace
        command: /bin/sh -c "id -n -u >%s; {{.NotifyDoneCheck}}; sleep %g"
        user: %s
        group: %s
`
	s.planAddLayer(c, fmt.Sprintf(
		layer,
		outputPath,
		shortOkayDelay.Seconds()+0.01,
		current.Username,
		group.Name,
	))
	s.planChanged(c)

	chg := s.startServices(c, [][]string{{"usrtest"}})
	s.st.Lock()
	c.Assert(chg.Err(), IsNil)
	s.st.Unlock()

	s.waitForDoneCheck(c, "usrtest")

	output, err := os.ReadFile(outputPath)
	c.Assert(err, IsNil)
	c.Check(string(output), Equals, current.Username+"\n")
}

func (s *S) TestUserGroupFails(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)
	s.planChanged(c)

	// Test with non-current user and group will fail due to permission issues
	// (unless running as root)
	if os.Getuid() == 0 {
		c.Skip("requires non-root user")
	}

	var gotUid uint32
	var gotGid uint32
	restore := servstate.FakeSetCmdCredential(func(cmd *exec.Cmd, credential *syscall.Credential) {
		gotUid = credential.Uid
		gotGid = credential.Gid
		cmd.SysProcAttr.Credential = credential
	})
	defer restore()

	chg := s.startServices(c, [][]string{{"test5"}})

	s.st.Lock()
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `.*\n.*cannot start service: .* operation not permitted.*`)
	s.st.Unlock()

	svc := s.serviceByName(c, "test5")
	c.Assert(svc.Current, Equals, servstate.StatusInactive)

	// Ensure that setCmdCredential was called with the correct UID and GID
	u, err := user.Lookup("nobody")
	c.Check(err, IsNil)
	uid, _ := strconv.Atoi(u.Uid)
	c.Check(gotUid, Equals, uint32(uid))
	g, err := user.LookupGroup("nogroup")
	c.Check(err, IsNil)
	gid, _ := strconv.Atoi(g.Gid)
	c.Check(gotGid, Equals, uint32(gid))
}

func (s *S) TestUserGroup(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)

	if os.Getuid() != 0 {
		c.Skip("requires running as root")
	}
	username := os.Getenv("PEBBLE_TEST_USER")
	group := os.Getenv("PEBBLE_TEST_GROUP")
	if username == "" || group == "" {
		c.Fatalf("must set PEBBLE_TEST_USER and PEBBLE_TEST_GROUP")
	}

	// Add layer with a service that sets user and group.
	layer := `
services:
    usrgrp:
        override: merge
        command: /bin/sh -c "whoami; echo user=$USER home=$HOME; sleep 10"
        user: %s
        group: %s
`
	s.planAddLayer(c, fmt.Sprintf(
		layer,
		username,
		group,
	))
	s.planChanged(c)

	// Override HOME and USER to ensure service exec updates them.
	os.Setenv("HOME", "x")
	os.Setenv("USER", "y")

	// Start service and ensure it has enough time to write to log.
	chg := s.startServices(c, [][]string{{"usrgrp"}})
	s.waitUntilService(c, "usrgrp", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusActive
	})
	c.Assert(s.manager.BackoffNum("usrgrp"), Equals, 0)
	s.st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus)
	s.st.Unlock()
	time.Sleep(10 * time.Millisecond)
	c.Check(s.readAndClearLogBuffer(), Matches,
		fmt.Sprintf(`(?s).* \[usrgrp\] %[1]s\n.* \[usrgrp\] user=%[1]s home=/home/%[1]s\n`, username))
}

func (s *S) TestStartFastExitCommand(c *C) {
	s.newServiceManager(c)
	var layer = `
services:
    test4:
        override: replace
        command: echo -e 'too-fast\nsecond line'
`
	s.planAddLayer(c, layer)
	s.planChanged(c)

	chg := s.startServices(c, [][]string{{"test4"}})

	s.st.Lock()
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `(?s).*\n- Start service "test4" \(service start attempt: exited quickly with code 0, will restart\)`)
	c.Check(chg.Tasks()[0].Log(), HasLen, 2)
	c.Check(chg.Tasks()[0].Log()[0], Matches, `(?s).* INFO Most recent service output:\n    too-fast\n    second line`)
	c.Check(chg.Tasks()[0].Log()[1], Matches, `.* ERROR service start attempt: exited quickly with code 0, will restart`)
	s.st.Unlock()

	svc := s.serviceByName(c, "test4")
	c.Assert(svc.Current, Equals, servstate.StatusBackoff)
}

func (s *S) TestStartFastExitCommandOnFailureIgnore(c *C) {
	s.newServiceManager(c)
	var layer = `
services:
    test1:
        override: replace
        command: /bin/sh -c "exit 1"
        on-failure: ignore
`
	s.planAddLayer(c, layer)
	s.planChanged(c)

	chg := s.startServices(c, [][]string{{"test1"}})

	s.st.Lock()
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, `(?s).*\n- Start service "test1" \(service start attempt: exited quickly with code 1, will ignore\)`)
	c.Check(chg.Tasks()[0].Log(), HasLen, 2)
	c.Check(chg.Tasks()[0].Log()[0], Matches, `(?s).* INFO Most recent service output:\n    `)
	c.Check(chg.Tasks()[0].Log()[1], Matches, `.* ERROR service start attempt: exited quickly with code 1, will ignore`)
	s.st.Unlock()

	svc := s.serviceByName(c, "test1")
	c.Assert(svc.Current, Equals, servstate.StatusError)
}

func (s *S) TestServices(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)
	s.planChanged(c)

	started := time.Now()
	services, err := s.manager.Services(nil)
	c.Assert(err, IsNil)
	c.Assert(services, DeepEquals, []*servstate.ServiceInfo{
		{Name: "test1", Current: servstate.StatusInactive, Startup: servstate.StartupEnabled},
		{Name: "test2", Current: servstate.StatusInactive, Startup: servstate.StartupDisabled},
		{Name: "test3", Current: servstate.StatusInactive, Startup: servstate.StartupDisabled},
		{Name: "test4", Current: servstate.StatusInactive, Startup: servstate.StartupDisabled},
		{Name: "test5", Current: servstate.StatusInactive, Startup: servstate.StartupDisabled},
	})

	services, err = s.manager.Services([]string{"test2", "test3"})
	c.Assert(err, IsNil)
	c.Assert(services, DeepEquals, []*servstate.ServiceInfo{
		{Name: "test2", Current: servstate.StatusInactive, Startup: servstate.StartupDisabled},
		{Name: "test3", Current: servstate.StatusInactive, Startup: servstate.StartupDisabled},
	})

	// Start a service and ensure it's marked active
	s.startServices(c, [][]string{{"test2"}})

	services, err = s.manager.Services(nil)
	c.Assert(err, IsNil)
	c.Assert(services[1].CurrentSince.After(started) && services[1].CurrentSince.Before(started.Add(5*time.Second)), Equals, true)
	services[1].CurrentSince = time.Time{}
	c.Assert(services, DeepEquals, []*servstate.ServiceInfo{
		{Name: "test1", Current: servstate.StatusInactive, Startup: servstate.StartupEnabled},
		{Name: "test2", Current: servstate.StatusActive, Startup: servstate.StartupDisabled},
		{Name: "test3", Current: servstate.StatusInactive, Startup: servstate.StartupDisabled},
		{Name: "test4", Current: servstate.StatusInactive, Startup: servstate.StartupDisabled},
		{Name: "test5", Current: servstate.StatusInactive, Startup: servstate.StartupDisabled},
	})
}

func (s *S) TestEnvironment(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)

	// Setup new state and add "envtest" layer
	dir := c.MkDir()
	logPath := filepath.Join(dir, "log.txt")
	layer := `
services:
    envtest:
        override: replace
        command: /bin/sh -c "env | grep PEBBLE_ENV_TEST | sort > %s; {{.NotifyDoneCheck}}; sleep 10"
        environment:
            PEBBLE_ENV_TEST_1: foo
            PEBBLE_ENV_TEST_2: bar bazz
`
	s.planAddLayer(c, fmt.Sprintf(
		layer,
		logPath,
	))
	s.planChanged(c)

	// Set environment variables in the current process to ensure we're
	// passing down the parent's environment too, but the layer's config
	// should override these if also set there.
	err := os.Setenv("PEBBLE_ENV_TEST_PARENT", "from-parent")
	c.Assert(err, IsNil)
	err = os.Setenv("PEBBLE_ENV_TEST_1", "should be overridden")
	c.Assert(err, IsNil)

	// Start "envtest" service
	chg := s.startServices(c, [][]string{{"envtest"}})
	s.st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("Error: %v", chg.Err()))
	s.st.Unlock()

	s.waitForDoneCheck(c, "envtest")

	// Ensure it read environment variables correctly
	data, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		c.Fatal("'envtest' service did not run")
	}
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, `
PEBBLE_ENV_TEST_1=foo
PEBBLE_ENV_TEST_2=bar bazz
PEBBLE_ENV_TEST_PARENT=from-parent
`[1:])
}

// TestActionRestart makes sure that the service restart backoff mechanism
// works as designed, including the reset of backoff once a service runs
// continuously for at least the backoff limit duration.
//
// This unit test is very timing sensitive, as as a result require
// conservative delay periods to ensure the test does not fail during
// nondeterministic cpu spikes on the test system.
func (s *S) TestActionRestart(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)

	// Add custom backoff attributes so it auto-restarts quickly. The
	// following backoff pattern will be observed:
	//
	// First service exit:
	//    - Restart after (backoff-delay) = 1ms
	//
	// Second service exit:
	//    - Restart after (1ms X backoff-factor) = 10ms
	//
	// Note that while in backoff state, as soon as the service continues
	// to run successfully for more than backoff-limit (500ms), the
	// backoff state will reset.
	//
	// Note: If the backoff-limit is too short, any test environment related
	// cpu spike that delays the restart of the service by more than
	// backoff-limit will prematurely trigger a backoff reset, which will
	// result in the test failing. Making this less likely requires a more
	// conservative (longer delay) value for backoff-limit, at the slight
	// expense of making the test take longer.
	s.planAddLayer(c, `
services:
    test2:
        override: merge
        command: /bin/sh -c "echo test2; {{.NotifyDoneCheck}}; sleep 10"
        backoff-delay: 1ms
        backoff-limit: 500ms
        backoff-factor: 10
`)
	s.planChanged(c)

	// Start the "test2" service
	chg := s.startServices(c, [][]string{{"test2"}})
	// Wait until "test2" service completes the echo command (so that we
	// know the log buffer contains stdout).
	s.waitForDoneCheck(c, "test2")
	// Verify the backoff counter
	c.Assert(s.manager.BackoffNum("test2"), Equals, 0)
	s.st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus)
	s.st.Unlock()
	c.Check(s.readAndClearLogBuffer(), Matches, `2.* \[test2\] test2\n`)

	// Send signal to process to terminate it early.
	err := s.manager.SendSignal([]string{"test2"}, "SIGTERM")
	c.Assert(err, IsNil)

	// Wait until "test2" service completes the echo command (so that we
	// know the log buffer contains stdout).
	s.waitForDoneCheck(c, "test2")
	c.Assert(s.manager.BackoffNum("test2"), Equals, 1)
	c.Check(s.readAndClearLogBuffer(), Matches, `2.* \[test2\] test2\n`)

	// Send signal to terminate it again.
	err = s.manager.SendSignal([]string{"test2"}, "SIGTERM")
	c.Assert(err, IsNil)

	// Wait until "test2" service completes the echo command (so that we
	// know the log buffer contains stdout).
	s.waitForDoneCheck(c, "test2")
	c.Assert(s.manager.BackoffNum("test2"), Equals, 2)
	c.Check(s.readAndClearLogBuffer(), Matches, `2.* \[test2\] test2\n`)

	// Test that backoff reset time is working. Run the service without
	// interruption for slightly longer than backoff-limit.
	time.Sleep(550 * time.Millisecond)
	c.Check(s.manager.BackoffNum("test2"), Equals, 0)

	// Send signal to process to terminate it early.
	err = s.manager.SendSignal([]string{"test2"}, "SIGTERM")
	c.Assert(err, IsNil)

	// Wait until "test2" service completes the echo command (so that we
	// know the log buffer contains stdout).
	s.waitForDoneCheck(c, "test2")
	c.Check(s.manager.BackoffNum("test2"), Equals, 1)
	c.Check(s.readAndClearLogBuffer(), Matches, `2.* \[test2\] test2\n`)
}

func (s *S) TestStopDuringBackoff(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)

	// Add custom backoff delay so it auto-restarts quickly.
	s.planAddLayer(c, `
services:
    test2:
        override: merge
        command: sleep 0.1
`)
	s.planChanged(c)

	// Start service and wait till it starts up the first time.
	chg := s.startServices(c, [][]string{{"test2"}})
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusActive
	})
	c.Assert(s.manager.BackoffNum("test2"), Equals, 0)
	s.st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus)
	s.st.Unlock()

	// Wait for it to exit and go into backoff state.
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusBackoff && s.manager.BackoffNum("test2") == 1
	})

	// Ensure it can be stopped successfully.
	chg = s.stopServices(c, [][]string{{"test2"}})
	s.st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("Error: %v", chg.Err()))
	s.st.Unlock()
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusInactive
	})
}

// The aim of this test is to make sure that the actioned check
// failure terminates the service, after which it will first go
// to back-off state and then finally starts again (only once).
// Since the check always fails, it should only ever send an action
// once.
func (s *S) TestOnCheckFailureRestartWhileRunning(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)

	// Create check manager and tell it about plan updates
	checkMgr := checkstate.NewManager(s.st, s.runner, nil)
	defer checkMgr.PlanChanged(&plan.Plan{})

	// Tell service manager about check failures
	checkFailed := make(chan struct{})
	checkMgr.NotifyCheckFailed(func(name string) {
		// Control when the action should be applied
		select {
		case checkFailed <- struct{}{}:
		case <-time.After(10 * time.Second):
			panic("timed out waiting to send on check-failed channel")
		}
		s.manager.CheckFailed(name)
	})

	tempDir := c.MkDir()
	tempFile := filepath.Join(tempDir, "out")
	layer := `
services:
    test2:
        override: replace
        command: /bin/sh -c 'echo x >>%s; {{.NotifyDoneCheck}}; sleep 10'
        backoff-delay: 100ms
        on-check-failure:
            chk1: restart

checks:
    chk1:
         override: replace
         period: 100ms
         threshold: 1
         exec:
             command: will-fail
`
	s.planAddLayer(c, fmt.Sprintf(
		layer,
		tempFile,
	))
	s.planChanged(c)
	checkMgr.PlanChanged(s.plan)

	// Start service and wait till it starts up
	s.startServices(c, [][]string{{"test2"}})

	s.waitForDoneCheck(c, "test2")

	b, err := os.ReadFile(tempFile)
	c.Assert(err, IsNil)
	c.Assert(string(b), Equals, "x\n")

	// Now wait till check happens (it will-fail)
	select {
	case <-checkFailed:
	case <-time.After(10 * time.Second):
		c.Fatalf("timed out waiting for check failure to arrive")
	}
	checks := waitChecks(c, checkMgr, func(checks []*checkstate.CheckInfo) bool {
		return len(checks) == 1 && checks[0].Status == checkstate.CheckStatusDown
	})
	c.Assert(checks[0].Failures >= 1, Equals, true)

	// Check failure should terminate process, backoff, and restart it, so wait for that
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusBackoff
	})

	s.waitForDoneCheck(c, "test2")

	b, err = os.ReadFile(tempFile)
	c.Assert(err, IsNil)
	c.Assert(string(b), Equals, "x\nx\n")

	// Shouldn't be restarted again
	time.Sleep(125 * time.Millisecond)
	b, err = os.ReadFile(tempFile)
	c.Assert(err, IsNil)
	c.Assert(string(b), Equals, "x\nx\n")
	checks = waitChecks(c, checkMgr, func(checks []*checkstate.CheckInfo) bool {
		return len(checks) == 1 && checks[0].Status == checkstate.CheckStatusDown
	})
	svc := s.serviceByName(c, "test2")
	c.Assert(svc.Current, Equals, servstate.StatusActive)
	c.Assert(s.manager.BackoffNum("test2"), Equals, 1)
}

// The aim of this test is to make sure that the actioned check
// failure occurring during service back-off has no effect on
// on that service. The service is expected to restart by itself
// (due to back-off). Since the check always fails, it should only
// ever send an action once.
func (s *S) TestOnCheckFailureRestartDuringBackoff(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)

	// Create check manager and tell it about plan updates
	checkMgr := checkstate.NewManager(s.st, s.runner, nil)
	defer checkMgr.PlanChanged(&plan.Plan{})

	// Tell service manager about check failures
	checkFailed := make(chan struct{})
	checkMgr.NotifyCheckFailed(func(name string) {
		// Control when the action should be applied
		select {
		case checkFailed <- struct{}{}:
		case <-time.After(10 * time.Second):
			panic("timed out waiting to send on check-failed channel")
		}
		s.manager.CheckFailed(name)
	})

	tempDir := c.MkDir()
	tempFile := filepath.Join(tempDir, "out")
	layer := `
services:
    test2:
        override: replace
        command: /bin/sh -c 'echo x >>%s; {{.NotifyDoneCheck}}; sleep 0.1'
        backoff-delay: 100ms
        backoff-factor: 100  # ensure it only backoff-restarts once
        on-check-failure:
            chk1: restart

checks:
    chk1:
         override: replace
         period: 100ms
         threshold: 1
         exec:
             command: will-fail
`
	s.planAddLayer(c, fmt.Sprintf(
		layer,
		tempFile,
	))
	s.planChanged(c)
	checkMgr.PlanChanged(s.plan)

	// Start service and wait till it starts up
	s.startServices(c, [][]string{{"test2"}})

	s.waitForDoneCheck(c, "test2")

	b, err := os.ReadFile(tempFile)
	c.Assert(err, IsNil)
	c.Assert(string(b), Equals, "x\n")

	// Ensure it exits and goes into backoff state
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusBackoff
	})

	// Now wait till check happens (it will-fail)
	select {
	case <-checkFailed:
	case <-time.After(10 * time.Second):
		c.Fatalf("timed out waiting for check failure to arrive")
	}

	s.waitForDoneCheck(c, "test2")

	b, err = os.ReadFile(tempFile)
	c.Assert(err, IsNil)
	c.Assert(string(b), Equals, "x\nx\n")

	svc := s.serviceByName(c, "test2")
	c.Assert(svc.Current, Equals, servstate.StatusActive)
	c.Assert(s.manager.BackoffNum("test2"), Equals, 1)

	// Shouldn't be restarted again
	time.Sleep(125 * time.Millisecond)
	b, err = os.ReadFile(tempFile)
	c.Assert(err, IsNil)
	c.Assert(string(b), Equals, "x\nx\n")
	waitChecks(c, checkMgr, func(checks []*checkstate.CheckInfo) bool {
		return len(checks) == 1 && checks[0].Status == checkstate.CheckStatusDown
	})
}

// The aim of this test is to make sure that the actioned check
// failure is ignored, and as a result the service keeps on
// running. Since the check always fails, it should only ever
// send an action once.
func (s *S) TestOnCheckFailureIgnore(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)

	// Create check manager and tell it about plan updates
	checkMgr := checkstate.NewManager(s.st, s.runner, nil)
	defer checkMgr.PlanChanged(&plan.Plan{})

	// Tell service manager about check failures
	checkFailed := make(chan struct{})
	checkMgr.NotifyCheckFailed(func(name string) {
		// Control when the action should be applied
		select {
		case checkFailed <- struct{}{}:
		case <-time.After(10 * time.Second):
			panic("timed out waiting to send on check-failed channel")
		}
		s.manager.CheckFailed(name)
	})

	tempDir := c.MkDir()
	tempFile := filepath.Join(tempDir, "out")
	layer := `
services:
    test2:
        override: replace
        command: /bin/sh -c 'echo x >>%s; {{.NotifyDoneCheck}}; sleep 10'
        on-check-failure:
            chk1: ignore

checks:
    chk1:
         override: replace
         period: 100ms
         threshold: 1
         exec:
             command: will-fail
`
	s.planAddLayer(c, fmt.Sprintf(
		layer,
		tempFile,
	))
	s.planChanged(c)
	checkMgr.PlanChanged(s.plan)

	// Start service and wait till it starts up (output file is written to)
	s.startServices(c, [][]string{{"test2"}})

	s.waitForDoneCheck(c, "test2")

	b, err := os.ReadFile(tempFile)
	c.Assert(err, IsNil)
	c.Assert(string(b), Equals, "x\n")

	// Now wait till check happens (it will-fail)
	select {
	case <-checkFailed:
	case <-time.After(10 * time.Second):
		c.Fatalf("timed out waiting for check failure to arrive")
	}
	checks := waitChecks(c, checkMgr, func(checks []*checkstate.CheckInfo) bool {
		return len(checks) == 1 && checks[0].Status == checkstate.CheckStatusDown
	})
	c.Assert(checks[0].Failures >= 1, Equals, true)

	// Service shouldn't have been restarted
	time.Sleep(125 * time.Millisecond)
	b, err = os.ReadFile(tempFile)
	c.Assert(err, IsNil)
	c.Assert(string(b), Equals, "x\n")
	checks = waitChecks(c, checkMgr, func(checks []*checkstate.CheckInfo) bool {
		return len(checks) == 1 && checks[0].Status == checkstate.CheckStatusDown
	})
	svc := s.serviceByName(c, "test2")
	c.Assert(svc.Current, Equals, servstate.StatusActive)
}

func (s *S) TestOnCheckFailureShutdown(c *C) {
	s.testOnCheckFailureShutdown(c, "shutdown", restart.RestartCheckFailure)
}

func (s *S) TestOnCheckFailureSuccessShutdown(c *C) {
	s.testOnCheckFailureShutdown(c, "success-shutdown", restart.RestartDaemon)
}

func (s *S) testOnCheckFailureShutdown(c *C, action string, restartType restart.RestartType) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)

	// Create check manager and tell it about plan updates
	checkMgr := checkstate.NewManager(s.st, s.runner, nil)
	defer checkMgr.PlanChanged(&plan.Plan{})

	// Tell service manager about check failures
	checkFailed := make(chan struct{})
	checkMgr.NotifyCheckFailed(func(name string) {
		// Control when the action should be applied
		select {
		case checkFailed <- struct{}{}:
		case <-time.After(10 * time.Second):
			panic("timed out waiting to send on check-failed channel")
		}
		s.manager.CheckFailed(name)
	})

	tempDir := c.MkDir()
	tempFile := filepath.Join(tempDir, "out")
	layer := `
services:
    test2:
        override: replace
        command: /bin/sh -c 'echo x >>%s; {{.NotifyDoneCheck}}; sleep 10'
        on-check-failure:
            chk1: %s
checks:
    chk1:
         override: replace
         period: 100ms
         threshold: 1
         exec:
             command: will-fail
`
	s.planAddLayer(c, fmt.Sprintf(
		layer,
		tempFile,
		action,
	))
	s.planChanged(c)
	checkMgr.PlanChanged(s.plan)

	// Start service and wait till it starts up (output file is written to)
	s.startServices(c, [][]string{{"test2"}})

	s.waitForDoneCheck(c, "test2")

	b, err := os.ReadFile(tempFile)
	c.Assert(err, IsNil)
	c.Assert(string(b), Equals, "x\n")

	// Now wait till check happens (it will-fail)
	select {
	case <-checkFailed:
	case <-time.After(10 * time.Second):
		c.Fatalf("timed out waiting for check failure to arrive")
	}
	checks := waitChecks(c, checkMgr, func(checks []*checkstate.CheckInfo) bool {
		return len(checks) == 1 && checks[0].Status == checkstate.CheckStatusDown
	})
	c.Assert(checks[0].Failures >= 1, Equals, true)

	// It should have closed the stopDaemon channel.
	select {
	case t := <-s.stopDaemon:
		c.Assert(t, Equals, restartType)
	case <-time.After(time.Second):
		c.Fatalf("timed out waiting for stop-daemon channel")
	}
}

func (s *S) TestOnSuccessShutdown(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, `
services:
    test2:
        override: replace
        command: sleep 0.15
        on-success: shutdown
`)
	s.planChanged(c)

	// Start service and wait till it starts up the first time.
	s.startServices(c, [][]string{{"test2"}})
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusActive
	})

	// Wait till it terminates.
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusError
	})

	// It should have closed the stopDaemon channel.
	select {
	case restartType := <-s.stopDaemon:
		c.Assert(restartType, Equals, restart.RestartDaemon)
	case <-time.After(time.Second):
		c.Fatalf("timed out waiting for stop-daemon channel")
	}
}

func (s *S) TestOnFailureShutdown(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, `
services:
    test2:
        override: replace
        command: /bin/sh -c 'sleep 0.15; exit 7'
        on-failure: shutdown
`)
	s.planChanged(c)

	// Start service and wait till it starts up the first time.
	s.startServices(c, [][]string{{"test2"}})
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusActive
	})

	// Wait till it terminates.
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusError
	})

	// It should have closed the stopDaemon channel.
	select {
	case restartType := <-s.stopDaemon:
		c.Assert(restartType, Equals, restart.RestartServiceFailure)
	case <-time.After(time.Second):
		c.Fatalf("timed out waiting for stop-daemon channel")
	}
}

func (s *S) TestOnSuccessFailureShutdown(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, `
services:
    test2:
        override: replace
        command: sleep 0.15
        on-success: failure-shutdown
`)
	s.planChanged(c)

	// Start service and wait till it starts up the first time.
	s.startServices(c, [][]string{{"test2"}})
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusActive
	})

	// Wait till it terminates.
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusError
	})

	// It should have closed the stopDaemon channel.
	select {
	case restartType := <-s.stopDaemon:
		c.Assert(restartType, Equals, restart.RestartServiceFailure)
	case <-time.After(time.Second):
		c.Fatalf("timed out waiting for stop-daemon channel")
	}
}

func (s *S) TestOnFailureSuccessShutdown(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, `
services:
    test2:
        override: replace
        command: /bin/sh -c 'sleep 0.15; exit 7'
        on-failure: success-shutdown
`)
	s.planChanged(c)

	// Start service and wait till it starts up the first time.
	s.startServices(c, [][]string{{"test2"}})
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusActive
	})

	// Wait till it terminates.
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusError
	})

	// It should have closed the stopDaemon channel.
	select {
	case restartType := <-s.stopDaemon:
		c.Assert(restartType, Equals, restart.RestartDaemon)
	case <-time.After(time.Second):
		c.Fatalf("timed out waiting for stop-daemon channel")
	}
}

func (s *S) TestActionIgnore(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, `
services:
    test2:
        override: replace
        command: sleep 0.15
        on-success: ignore
`)
	s.planChanged(c)

	// Start service and wait till it starts up the first time.
	s.startServices(c, [][]string{{"test2"}})
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusActive
	})

	// Wait till it terminates.
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusInactive
	})
}

func (s *S) TestGetAction(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)
	s.planChanged(c)

	tests := []struct {
		onSuccess plan.ServiceAction
		onFailure plan.ServiceAction
		success   bool
		action    string
		onType    string
	}{
		{onSuccess: "", onFailure: "", success: false, action: "restart", onType: "on-failure"},
		{onSuccess: "", onFailure: "", success: true, action: "restart", onType: "on-success"},
		{onSuccess: "", onFailure: "restart", success: false, action: "restart", onType: "on-failure"},
		{onSuccess: "", onFailure: "restart", success: true, action: "restart", onType: "on-success"},
		{onSuccess: "", onFailure: "shutdown", success: false, action: "shutdown", onType: "on-failure"},
		{onSuccess: "", onFailure: "shutdown", success: true, action: "restart", onType: "on-success"},
		{onSuccess: "", onFailure: "ignore", success: false, action: "ignore", onType: "on-failure"},
		{onSuccess: "", onFailure: "ignore", success: true, action: "restart", onType: "on-success"},
		{onSuccess: "restart", onFailure: "", success: false, action: "restart", onType: "on-failure"},
		{onSuccess: "restart", onFailure: "", success: true, action: "restart", onType: "on-success"},
		{onSuccess: "restart", onFailure: "restart", success: false, action: "restart", onType: "on-failure"},
		{onSuccess: "restart", onFailure: "restart", success: true, action: "restart", onType: "on-success"},
		{onSuccess: "restart", onFailure: "shutdown", success: false, action: "shutdown", onType: "on-failure"},
		{onSuccess: "restart", onFailure: "shutdown", success: true, action: "restart", onType: "on-success"},
		{onSuccess: "restart", onFailure: "ignore", success: false, action: "ignore", onType: "on-failure"},
		{onSuccess: "restart", onFailure: "ignore", success: true, action: "restart", onType: "on-success"},
		{onSuccess: "shutdown", onFailure: "", success: false, action: "restart", onType: "on-failure"},
		{onSuccess: "shutdown", onFailure: "", success: true, action: "shutdown", onType: "on-success"},
		{onSuccess: "shutdown", onFailure: "restart", success: false, action: "restart", onType: "on-failure"},
		{onSuccess: "shutdown", onFailure: "restart", success: true, action: "shutdown", onType: "on-success"},
		{onSuccess: "shutdown", onFailure: "shutdown", success: false, action: "shutdown", onType: "on-failure"},
		{onSuccess: "shutdown", onFailure: "shutdown", success: true, action: "shutdown", onType: "on-success"},
		{onSuccess: "shutdown", onFailure: "ignore", success: false, action: "ignore", onType: "on-failure"},
		{onSuccess: "shutdown", onFailure: "ignore", success: true, action: "shutdown", onType: "on-success"},
		{onSuccess: "ignore", onFailure: "", success: false, action: "restart", onType: "on-failure"},
		{onSuccess: "ignore", onFailure: "", success: true, action: "ignore", onType: "on-success"},
		{onSuccess: "ignore", onFailure: "restart", success: false, action: "restart", onType: "on-failure"},
		{onSuccess: "ignore", onFailure: "restart", success: true, action: "ignore", onType: "on-success"},
		{onSuccess: "ignore", onFailure: "shutdown", success: false, action: "shutdown", onType: "on-failure"},
		{onSuccess: "ignore", onFailure: "shutdown", success: true, action: "ignore", onType: "on-success"},
		{onSuccess: "ignore", onFailure: "ignore", success: false, action: "ignore", onType: "on-failure"},
		{onSuccess: "ignore", onFailure: "ignore", success: true, action: "ignore", onType: "on-success"},
	}
	for _, test := range tests {
		config := &plan.Service{
			OnFailure: test.onFailure,
			OnSuccess: test.onSuccess,
		}
		action, onType := servstate.GetAction(config, test.success)
		c.Check(string(action), Equals, test.action, Commentf("onSuccess=%q, onFailure=%q, success=%v",
			test.onSuccess, test.onFailure, test.success))
		c.Check(onType, Equals, test.onType, Commentf("onSuccess=%q, onFailure=%q, success=%v",
			test.onSuccess, test.onFailure, test.success))
	}
}

func (s *S) TestGetJitter(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)
	s.planChanged(c)

	// It's tricky to test a function that generates randomness, but ensure all
	// the values are in range, and that the number of values distributed across
	// each of 3 buckets is reasonable.
	var buckets [3]int
	for i := 0; i < 3000; i++ {
		jitter := s.manager.GetJitter(3 * time.Second)
		c.Assert(jitter >= 0 && jitter < 300*time.Millisecond, Equals, true)
		switch {
		case jitter >= 0 && jitter < 100*time.Millisecond:
			buckets[0]++
		case jitter >= 100*time.Millisecond && jitter < 200*time.Millisecond:
			buckets[1]++
		case jitter >= 200*time.Millisecond && jitter < 300*time.Millisecond:
			buckets[2]++
		default:
			c.Errorf("jitter %s outside range [0, 300ms)", jitter)
		}
	}
	for i := 0; i < 3; i++ {
		if buckets[i] < 800 || buckets[i] > 1200 { // exceedingly unlikely to be outside this range
			c.Errorf("bucket[%d] has too few or too many values in it (%d)", i, buckets[i])
		}
	}
}

func (s *S) TestCalculateNextBackoff(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)
	s.planChanged(c)

	tests := []struct {
		delay   time.Duration
		factor  float64
		limit   time.Duration
		current time.Duration
		next    time.Duration
	}{
		{delay: 500 * time.Millisecond, factor: 2, limit: 30 * time.Second, current: 0, next: 500 * time.Millisecond},
		{delay: 500 * time.Millisecond, factor: 2, limit: 30 * time.Second, current: 500 * time.Millisecond, next: time.Second},
		{delay: 500 * time.Millisecond, factor: 2, limit: 30 * time.Second, current: time.Second, next: 2 * time.Second},
		{delay: 500 * time.Millisecond, factor: 2, limit: 30 * time.Second, current: 16 * time.Second, next: 30 * time.Second},
		{delay: 500 * time.Millisecond, factor: 2, limit: 30 * time.Second, current: 30 * time.Second, next: 30 * time.Second},
		{delay: 500 * time.Millisecond, factor: 2, limit: 30 * time.Second, current: 1000 * time.Second, next: 30 * time.Second},

		{delay: time.Second, factor: 1.5, limit: 60 * time.Second, current: 0, next: time.Second},
		{delay: time.Second, factor: 1.5, limit: 60 * time.Second, current: time.Second, next: 1500 * time.Millisecond},
		{delay: time.Second, factor: 1.5, limit: 60 * time.Second, current: 1500 * time.Millisecond, next: 2250 * time.Millisecond},
		{delay: time.Second, factor: 1.5, limit: 60 * time.Second, current: 50 * time.Second, next: 60 * time.Second},
		{delay: time.Second, factor: 1.5, limit: 60 * time.Second, current: 60 * time.Second, next: 60 * time.Second},
		{delay: time.Second, factor: 1.5, limit: 60 * time.Second, current: 70 * time.Second, next: 60 * time.Second},
	}
	for _, test := range tests {
		config := &plan.Service{
			BackoffDelay:  plan.OptionalDuration{Value: test.delay},
			BackoffFactor: plan.OptionalFloat{Value: test.factor},
			BackoffLimit:  plan.OptionalDuration{Value: test.limit},
		}
		next := servstate.CalculateNextBackoff(config, test.current)
		c.Check(next, Equals, test.next, Commentf("delay=%s, factor=%g, limit=%s, current=%s",
			test.delay, test.factor, test.limit, test.current))
	}
}

func (s *S) TestReapZombies(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)

	// Ensure we've been set as a child subreaper
	isSubreaper, err := getChildSubreaper()
	c.Assert(err, IsNil)
	c.Assert(isSubreaper, Equals, true)

	// Start a service which runs this test executable with an environment
	// variable set so the "service" knows to create a zombie child.
	testExecutable, err := os.Executable()
	c.Assert(err, IsNil)
	exitChildPath := filepath.Join(s.dir, "exit-child")
	layer := `
services:
    test2:
        override: replace
        command: %s
        environment:
            PEBBLE_TEST_CREATE_CHILD: 1
            PEBBLE_TEST_EXIT_CHILD: %s
        on-success: ignore
`
	s.planAddLayer(c, fmt.Sprintf(
		layer,
		testExecutable,
		exitChildPath,
	))
	s.planChanged(c)

	s.startServices(c, [][]string{{"test2"}})

	// The child process creates a grandchild and will print the
	// PID of the grandchild for us to inspect here. We need to
	// wait until we observe this on stdout.
	childPidRe := regexp.MustCompile(`.* childPid (\d+)`)
	childPid := 0
	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
pid:
	for {
		select {
		case <-ticker.C:
			// Get the log without resetting it on read
			matches := childPidRe.FindStringSubmatch(s.readLogBuffer())
			if len(matches) == 2 {
				childPid, err = strconv.Atoi(matches[1])
				if err == nil {
					// Valid grandchild PID
					break pid
				}
			}
		case <-timeout:
			c.Fatalf("timed out waiting for grandchild pid on stdout")
		}
	}
	s.clearLogBuffer()

	// Wait until the grandchild is zombified
	timeout = time.After(10 * time.Second)
zombi:
	for {
		select {
		case <-ticker.C:
			stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", childPid))
			c.Assert(err, IsNil)
			statFields := strings.Fields(string(stat))
			c.Assert(len(statFields) >= 3, Equals, true)
			if statFields[2] == "Z" {
				break zombi
			}

		case <-timeout:
			c.Fatalf("timed out waiting for grandchild to zombify")
		}
	}

	// Wait till the child terminates (test2 exits)
	fd, err := os.OpenFile(exitChildPath, os.O_RDONLY|os.O_CREATE, 0666)
	c.Assert(err, IsNil)
	fd.Close()
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusInactive
	})

	// Wait till the zombie has been reaped (no longer in the process table)
	timeout = time.After(10 * time.Second)
reap:
	for {
		select {
		case <-ticker.C:
			_, err := os.Stat(fmt.Sprintf("/proc/%d/stat", childPid))
			if os.IsNotExist(err) {
				break reap
			}
		case <-timeout:
			c.Fatalf("timed out waiting for zombie to be reaped")
		}
	}
}

func (s *S) TestStopRunning(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)
	s.planChanged(c)

	s.startServices(c, [][]string{{"test2"}})
	s.waitUntilService(c, "test2", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusActive
	})

	// Create and execute a change for stopping the running services.
	taskSet, err := servstate.StopRunning(s.st, s.manager)
	c.Assert(err, IsNil)
	s.st.Lock()
	change := s.st.NewChange("stop", "Stop all running services")
	change.AddAll(taskSet)
	s.st.Unlock()

	// Wait for it to complete.
	waitChangeReady(c, s.runner, change, "services to stop")

	// Ensure that the service has actually been stopped.
	svc := s.serviceByName(c, "test2")
	c.Assert(svc.Current, Equals, servstate.StatusInactive)

	// Ensure that the change and tasks are marked Done.
	s.st.Lock()
	defer s.st.Unlock()
	c.Check(change.Status(), Equals, state.DoneStatus)
	tasks := change.Tasks()
	c.Assert(tasks, HasLen, 1)
	c.Check(tasks[0].Kind(), Equals, "stop")
	c.Check(tasks[0].Status(), Equals, state.DoneStatus)
}

func (s *S) TestStopRunningNoServices(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)
	s.planChanged(c)

	taskSet, err := servstate.StopRunning(s.st, s.manager)
	c.Assert(err, IsNil)
	c.Assert(taskSet, IsNil)
}

func (s *S) TestNoWorkingDir(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)

	outputPath := filepath.Join(c.MkDir(), "output")
	layer := `
services:
    nowrkdir:
        override: replace
        command: /bin/sh -c "pwd >%s; {{.NotifyDoneCheck}}; sleep %g"
`
	s.planAddLayer(c, fmt.Sprintf(
		layer,
		outputPath,
		shortOkayDelay.Seconds()+0.01,
	))
	s.planChanged(c)

	// Service command should run in current directory (package directory)
	// if "working-dir" config option not set.
	chg := s.startServices(c, [][]string{{"nowrkdir"}})
	s.st.Lock()
	c.Assert(chg.Err(), IsNil)
	s.st.Unlock()

	s.waitForDoneCheck(c, "nowrkdir")

	output, err := os.ReadFile(outputPath)
	c.Assert(err, IsNil)
	c.Check(string(output), Matches, ".*/overlord/servstate\n")
}

func (s *S) TestWorkingDir(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)

	dir := c.MkDir()
	outputPath := filepath.Join(dir, "output")
	layer := `
services:
    wrkdir:
        override: replace
        command: /bin/sh -c "pwd >%s; {{.NotifyDoneCheck}}; sleep %g"
        working-dir: %s
`
	s.planAddLayer(c, fmt.Sprintf(
		layer,
		outputPath,
		shortOkayDelay.Seconds()+0.01,
		dir,
	))
	s.planChanged(c)

	chg := s.startServices(c, [][]string{{"wrkdir"}})
	s.st.Lock()
	c.Assert(chg.Err(), IsNil)
	s.st.Unlock()

	s.waitForDoneCheck(c, "wrkdir")

	output, err := os.ReadFile(outputPath)
	c.Assert(err, IsNil)
	c.Check(string(output), Equals, dir+"\n")
}

func (s *S) TestWaitDelay(c *C) {
	s.newServiceManager(c)
	s.planAddLayer(c, testPlanLayer)

	// Run the test binary with PEBBLE_TEST_WAITDELAY=1 (see TestMain).
	testExecutable, err := os.Executable()
	c.Assert(err, IsNil)
	layer := `
services:
    waitdelay:
        override: replace
        command: %s
        environment:
            PEBBLE_TEST_WAITDELAY: 1
        kill-delay: 50ms
`
	s.planAddLayer(c, fmt.Sprintf(
		layer,
		testExecutable,
	))
	s.planChanged(c)

	// Start service and wait for it to be started
	chg := s.startServices(c, [][]string{{"waitdelay"}})
	s.st.Lock()
	c.Assert(chg.Err(), IsNil)
	s.st.Unlock()
	s.waitUntilService(c, "waitdelay", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusActive
	})

	// Try to stop the service; it will only stop if WaitDelay logic is working,
	// otherwise the goroutine waiting for the child's stdout will never finish.
	chg = s.stopServices(c, [][]string{{"waitdelay"}})
	s.st.Lock()
	c.Assert(chg.Err(), IsNil)
	s.st.Unlock()
	s.waitUntilService(c, "waitdelay", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusInactive
	})
}

func (s *S) newServiceManager(c *C) {
	var err error
	s.manager, err = servstate.NewManager(s.st, s.runner, s.logOutput, testRestarter{s.stopDaemon}, fakeLogManager{})
	c.Assert(err, IsNil)
}

func (s *S) planChanged(c *C) {
	c.Assert(s.plan, NotNil)
	s.manager.PlanChanged(s.plan)
	s.planPropagated = true
}

func (s *S) planAddLayer(c *C, layerYAML string) {
	cnt := len(s.plan.Layers)
	layer, err := plan.ParseLayer(cnt, fmt.Sprintf("test-plan-layer-%v", cnt), []byte(layerYAML))
	c.Assert(err, IsNil)
	// Resolve {{.NotifyDoneCheck}}
	s.insertDoneChecks(c, layer)
	layers := append(s.plan.Layers, layer)
	combined, err := plan.CombineLayers(layers...)
	c.Assert(err, IsNil)
	s.plan = &plan.Plan{
		Layers:     layers,
		Services:   combined.Services,
		Checks:     combined.Checks,
		LogTargets: combined.LogTargets,
	}
}

// Make sure services are all stopped before the next test starts.
func (s *S) stopRunningServices(c *C) {
	taskSet, err := servstate.StopRunning(s.st, s.manager)
	c.Assert(err, IsNil)

	if taskSet == nil {
		return
	}

	// One change to stop them all.
	s.st.Lock()
	chg := s.st.NewChange("stop", "Stop all running services")
	chg.AddAll(taskSet)
	s.st.EnsureBefore(0) // start operation right away
	s.st.Unlock()

	// Wait for a limited amount of time for them to stop.
	waitChangeReady(c, s.runner, chg, "services to stop")
}

func waitChangeReady(c *C, runner *state.TaskRunner, change *state.Change, message string) {
	timeout := time.After(10 * time.Second)
	for {
		runner.Ensure()
		select {
		case <-change.Ready():
			return
		case <-timeout:
			c.Fatalf("timeout waiting for %s", message)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

type testRestarter struct {
	ch chan restart.RestartType
}

func (r testRestarter) HandleRestart(t restart.RestartType) {
	r.ch <- t
}

// readAndClearLogBuffer reads and clears the current log buffer. If you need
// to poll for a specific stdout/stderr message, this is not suitable.
func (s *S) readAndClearLogBuffer() string {
	s.logBufferMut.Lock()
	defer s.logBufferMut.Unlock()
	str := s.logBuffer.String()
	s.logBuffer.Reset()
	return str
}

// readLogBuffer reads current log buffer without clearing it. Use this
// version to poll for a specific stdout/stderr message from a service.
func (s *S) readLogBuffer() string {
	s.logBufferMut.Lock()
	defer s.logBufferMut.Unlock()
	str := s.logBuffer.String()
	return str
}

func (s *S) clearLogBuffer() {
	s.logBufferMut.Lock()
	defer s.logBufferMut.Unlock()
	s.logBuffer.Reset()
}

func (s *S) testServiceLogs(c *C, outputs map[string]string) {
	s.startTestServices(c, true)

	if c.Failed() {
		return
	}

	iterators, err := s.manager.ServiceLogs([]string{"test1", "test2"}, -1)
	c.Assert(err, IsNil)
	c.Assert(iterators, HasLen, 2)

	for serviceName, it := range iterators {
		buf := &bytes.Buffer{}
		for it.Next(nil) {
			_, err = io.Copy(buf, it)
			c.Assert(err, IsNil)
		}

		c.Assert(buf.String(), Matches, outputs[serviceName])

		err = it.Close()
		c.Assert(err, IsNil)
	}

	s.stopTestServices(c)
}

func (s *S) startServices(c *C, lanes [][]string) *state.Change {
	s.st.Lock()
	ts, err := servstate.Start(s.st, lanes)
	c.Check(err, IsNil)
	chg := s.st.NewChange("test", "Start test")
	chg.AddAll(ts)
	s.st.Unlock()
	waitChangeReady(c, s.runner, chg, "services to start")
	return chg
}

func (s *S) stopServices(c *C, lanes [][]string) *state.Change {
	s.st.Lock()
	ts, err := servstate.Stop(s.st, lanes)
	c.Check(err, IsNil)
	chg := s.st.NewChange("test", "Stop test")
	chg.AddAll(ts)
	s.st.Unlock()
	waitChangeReady(c, s.runner, chg, "services to stop")
	return chg
}

func (s *S) serviceByName(c *C, name string) *servstate.ServiceInfo {
	services, err := s.manager.Services([]string{name})
	c.Assert(err, IsNil)
	c.Assert(services, HasLen, 1)
	return services[0]
}

func (s *S) startTestServices(c *C, logCheck bool) {
	chg := s.startServices(c, [][]string{{"test1", "test2"}})
	s.st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("Error: %v", chg.Err()))
	s.st.Unlock()

	cmds := s.manager.RunningCmds()
	c.Check(cmds, HasLen, 2)

	// When this helper is used for testing idempotence
	// the services are not actually started unless they
	// they are not running. In this case we have to disable
	// the log checks, as no new entries are expected.
	if logCheck {
		s.waitForDoneCheck(c, "test1")
		s.waitForDoneCheck(c, "test2")

		c.Assert(s.readAndClearLogBuffer(), Matches, "(?s).*test1\n.*test2\n")
	}
}

func (s *S) stopTestServices(c *C) {
	cmds := s.manager.RunningCmds()
	c.Check(cmds, HasLen, 2)

	chg := s.stopServices(c, [][]string{{"test1", "test2"}})

	// Ensure processes are gone indeed.
	c.Assert(cmds, HasLen, 2)
	for name, cmd := range cmds {
		err := cmd.Process.Signal(syscall.Signal(0))
		if err == nil {
			c.Fatalf("Process for %q did not stop properly", name)
		} else {
			c.Check(err, ErrorMatches, ".*process already finished.*")
		}
	}

	s.st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("Error: %v", chg.Err()))
	s.st.Unlock()
}

func (s *S) stopTestServicesAlreadyDead(c *C) {
	cmds := s.manager.RunningCmds()
	c.Check(cmds, HasLen, 0)

	chg := s.stopServices(c, [][]string{{"test1", "test2"}})

	c.Assert(cmds, HasLen, 0)

	s.st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("Error: %v", chg.Err()))
	s.st.Unlock()
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) {
	return f(p)
}

func (s *S) waitUntilService(c *C, service string, f func(svc *servstate.ServiceInfo) bool) {
	for i := 0; i < 310; i++ {
		svc := s.serviceByName(c, service)
		if f(svc) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	c.Fatalf("timed out waiting for service")
}

func getChildSubreaper() (bool, error) {
	var i uintptr
	err := unix.Prctl(unix.PR_GET_CHILD_SUBREAPER, uintptr(unsafe.Pointer(&i)), 0, 0, 0)
	if err != nil {
		return false, err
	}
	return i != 0, nil
}

func createZombie() error {
	// Run the test binary with PEBBLE_TEST_ZOMBIE_CHILD=1 (see TestMain)
	testExecutable, err := os.Executable()
	if err != nil {
		return err
	}
	procAttr := syscall.ProcAttr{
		Env: []string{"PEBBLE_TEST_CREATE_GRANDCHILD=1"},
	}
	childPid, err := syscall.ForkExec(testExecutable, []string{"zombie-child"}, &procAttr)
	if err != nil {
		return err
	}
	fmt.Printf("childPid %d\n", childPid)
	time.Sleep(shortOkayDelay + 25*time.Millisecond)
	return nil
}

type fakeLogManager struct{}

func (f fakeLogManager) ServiceStarted(service *plan.Service, logs *servicelog.RingBuffer) {
	// no-op
}

// insertDoneChecks modifies layer service commands which contains a
// {{.NotifyDoneCheck}} barrier placeholder. The placeholder is replaced
// with a command which writes a service specific file to a test
// directory, allowing waitForDoneCheck to detect service side effect
// completion.
func (s *S) insertDoneChecks(c *C, layer *plan.Layer) {
	for _, service := range layer.Services {
		doneCheck := fmt.Sprintf("sync; touch %s", filepath.Join(s.dir, service.Name))
		service.Command = strings.Replace(service.Command, "{{.NotifyDoneCheck}}", doneCheck, -1)
	}
}

// waitForDoneCheck waits until the done checks mechanism indicated
// that the service test side effect is complete, and the test can
// continue to evaluate the expected result.
func (s *S) waitForDoneCheck(c *C, service string) {
	donePath := filepath.Join(s.dir, service)
	waitForDone(donePath, func() {
		c.Fatal("timeout waiting for service output")
	})
}

// Return on timeout or when the file appears. This is used to determine
// when the expected service output is actually available, not when the
// service starts to run.
func waitForDone(donePath string, timeoutHandler func()) {
	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(time.Millisecond)
	for {
		select {
		case <-timeout:
			timeoutHandler()

		case <-ticker.C:
			stat, err := os.Stat(donePath)
			if err == nil && stat.Mode().IsRegular() {
				// Delete it so we can reuse this feature
				// in the same test once a service gets
				// restarted.
				os.Remove(donePath)
				return
			}
		}
	}
}

func waitChecks(c *C, checkMgr *checkstate.CheckManager, f func(checks []*checkstate.CheckInfo) bool) []*checkstate.CheckInfo {
	for start := time.Now(); time.Since(start) < 10*time.Second; {
		checks, err := checkMgr.Checks()
		c.Assert(err, IsNil)
		if f(checks) {
			return checks
		}
		time.Sleep(time.Millisecond)
	}
	c.Fatal("timed out waiting for checks to settle")
	return nil
}
