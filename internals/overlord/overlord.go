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

// Package overlord is the central control base, and ruler of all things.
package overlord

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/canonical/x-go/randutil"
	"gopkg.in/tomb.v2"

	"github.com/canonical/pebble/internals/osutil"
	"github.com/canonical/pebble/internals/overlord/checkstate"
	"github.com/canonical/pebble/internals/overlord/cmdstate"
	"github.com/canonical/pebble/internals/overlord/logstate"
	"github.com/canonical/pebble/internals/overlord/patch"
	"github.com/canonical/pebble/internals/overlord/restart"
	"github.com/canonical/pebble/internals/overlord/servstate"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/timing"
)

var (
	ensureInterval = 5 * time.Minute
	pruneInterval  = 10 * time.Minute
	pruneWait      = 24 * time.Hour * 1
	abortWait      = 24 * time.Hour * 7

	pruneMaxChanges = 500

	defaultCachedDownloads = 5
)

// Overlord is the central manager of the system, keeping track
// of all available state managers and related helpers.
type Overlord struct {
	pebbleDir string
	stateEng  *StateEngine

	// ensure loop
	loopTomb    *tomb.Tomb
	ensureLock  sync.Mutex
	ensureTimer *time.Timer
	ensureNext  time.Time
	ensureRun   int32
	pruneTicker *time.Ticker

	// managers
	inited           bool
	runner           *state.TaskRunner
	serviceMgr       *servstate.ServiceManager
	commandMgr       *cmdstate.CommandManager
	checkMgr         *checkstate.CheckManager
	logMgr           *logstate.LogManager
	externalManagers map[any]StateManager
}

// ManagerProvider is the interface that ManagerGenerator depends on
//
// Overlord implements ManagerProvider as it provides the necessary
// handles to hook an external manager into the overlord's environment.
type ManagerProvider interface {
	State() *state.State
	TaskRunner() *state.TaskRunner
}

// ManagerGenerator is passed to Overlord to create a manager
// The return value is a key, value pair where the key has to be a unique
// identifier for the manager being created.
type ManagerGenerator func(ManagerProvider) (key any, manager StateManager, err error)

// New creates a  Overlord with all its state managers.
// It can be provided with an optional restart.Handler.
func New(
	pebbleDir string,
	restartHandler restart.Handler,
	serviceOutput io.Writer,
	generators []ManagerGenerator) (*Overlord, error) {

	o := &Overlord{
		pebbleDir:        pebbleDir,
		loopTomb:         new(tomb.Tomb),
		inited:           true,
		externalManagers: make(map[any]StateManager, len(generators)),
	}

	if !filepath.IsAbs(pebbleDir) {
		return nil, fmt.Errorf("directory %q must be absolute", pebbleDir)
	}
	if !osutil.IsDir(pebbleDir) {
		return nil, fmt.Errorf("directory %q does not exist", pebbleDir)
	}
	statePath := filepath.Join(pebbleDir, ".pebble.state")

	backend := &overlordStateBackend{
		path:         statePath,
		ensureBefore: o.ensureBefore,
	}
	s, err := loadState(statePath, restartHandler, backend)
	if err != nil {
		return nil, err
	}

	o.stateEng = NewStateEngine(s)
	o.runner = state.NewTaskRunner(s)

	// any unknown task should be ignored and succeed
	matchAnyUnknownTask := func(_ *state.Task) bool {
		return true
	}
	o.runner.AddOptionalHandler(matchAnyUnknownTask, handleUnknownTask, nil)

	o.logMgr = logstate.NewLogManager()
	o.addManager(o.logMgr)

	o.serviceMgr, err = servstate.NewManager(s, o.runner, o.pebbleDir, serviceOutput, restartHandler, o.logMgr)
	if err != nil {
		return nil, err
	}
	o.addManager(o.serviceMgr)

	o.commandMgr = cmdstate.NewManager(o.runner)
	o.addManager(o.commandMgr)

	o.checkMgr = checkstate.NewManager()

	// Tell check manager about plan updates.
	o.serviceMgr.NotifyPlanChanged(o.checkMgr.PlanChanged)

	// Tell log manager about plan updates.
	o.serviceMgr.NotifyPlanChanged(o.logMgr.PlanChanged)

	// Tell service manager about check failures.
	o.checkMgr.NotifyCheckFailed(o.serviceMgr.CheckFailed)

	for _, gen := range generators {
		tag, manager, err := gen(o)
		if err != nil {
			return nil, err
		}
		o.externalManagers[tag] = manager
		o.addManager(manager)
	}

	// TaskRunner must be the last manager added to the StateEngine,
	// because TaskRunner runs all the tasks required by the managers that ran
	// before it.
	o.stateEng.AddManager(o.runner)

	return o, nil
}

func (o *Overlord) GetExternalManager(tag any) StateManager {
	result, ok := o.externalManagers[tag]
	if !ok {
		panic("Manager tag not found")
	}
	return result
}

func (o *Overlord) addManager(mgr StateManager) {
	o.stateEng.AddManager(mgr)
}

func loadState(statePath string, restartHandler restart.Handler, backend state.Backend) (*state.State, error) {
	timings := timing.Start("", "", map[string]string{"startup": "load-state"})

	curBootID, err := osutil.BootID()
	if err != nil {
		return nil, fmt.Errorf("fatal: cannot find current boot ID: %v", err)
	}
	// If pebble is PID 1 we don't care about /proc/sys/kernel/random/boot_id
	// as we are most likely running in a container. LXD mounts it's own boot_id
	// to correctly emulate the boot_id behaviour of non-containerized systems.
	// Within containerd/docker, boot_id is consistent with the host, which provides
	// us no context of restarts, so instead fallback to /proc/sys/kernel/random/uuid.
	if os.Getpid() == 1 {
		curBootID, err = randutil.RandomKernelUUID()
		if err != nil {
			return nil, fmt.Errorf("fatal: cannot generate psuedo boot-id: %v", err)
		}
	}

	if !osutil.CanStat(statePath) {
		// fail fast, mostly interesting for tests, this dir is set up by pebble
		stateDir := filepath.Dir(statePath)
		if !osutil.IsDir(stateDir) {
			return nil, fmt.Errorf("fatal: directory %q must be present", stateDir)
		}
		s := state.New(backend)
		initRestart(s, curBootID, restartHandler)
		patch.Init(s)
		return s, nil
	}

	r, err := os.Open(statePath)
	if err != nil {
		return nil, fmt.Errorf("cannot read the state file: %s", err)
	}
	defer r.Close()

	var s *state.State
	span := timings.StartNested("read-state", "read state from disk")
	s, err = state.ReadState(backend, r)
	span.Stop()
	if err != nil {
		return nil, err
	}

	timings.Stop()
	// TODO Implement function to save timings.
	//s.Lock()
	//perfTimings.Save(s)
	//s.Unlock()

	err = initRestart(s, curBootID, restartHandler)
	if err != nil {
		return nil, err
	}

	// one-shot migrations
	err = patch.Apply(s)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func initRestart(s *state.State, curBootID string, restartHandler restart.Handler) error {
	s.Lock()
	defer s.Unlock()
	return restart.Init(s, curBootID, restartHandler)
}

func (o *Overlord) ensureTimerSetup() {
	o.ensureLock.Lock()
	defer o.ensureLock.Unlock()
	o.ensureTimer = time.NewTimer(ensureInterval)
	o.ensureNext = time.Now().Add(ensureInterval)
	o.pruneTicker = time.NewTicker(pruneInterval)
}

func (o *Overlord) ensureTimerReset() time.Time {
	o.ensureLock.Lock()
	defer o.ensureLock.Unlock()
	now := time.Now()
	o.ensureTimer.Reset(ensureInterval)
	o.ensureNext = now.Add(ensureInterval)
	return o.ensureNext
}

func (o *Overlord) ensureBefore(d time.Duration) {
	o.ensureLock.Lock()
	defer o.ensureLock.Unlock()
	if o.ensureTimer == nil {
		panic("cannot use EnsureBefore before Overlord.Loop")
	}
	now := time.Now()
	next := now.Add(d)
	if next.Before(o.ensureNext) {
		o.ensureTimer.Reset(d)
		o.ensureNext = next
		return
	}

	if o.ensureNext.Before(now) {
		// timer already expired, it will be reset in Loop() and
		// next Ensure() will be called shortly.
		if !o.ensureTimer.Stop() {
			return
		}
		o.ensureTimer.Reset(0)
		o.ensureNext = now
	}
}

// Loop runs a loop in a goroutine to ensure the current state regularly through StateEngine Ensure.
func (o *Overlord) Loop() {
	o.ensureTimerSetup()
	o.loopTomb.Go(func() error {
		for {
			// TODO: pass a proper context into Ensure
			o.ensureTimerReset()
			// in case of errors engine logs them,
			// continue to the next Ensure() try for now
			o.stateEng.Ensure()
			o.ensureDidRun()
			select {
			case <-o.loopTomb.Dying():
				return nil
			case <-o.ensureTimer.C:
			case <-o.pruneTicker.C:
				st := o.State()
				st.Lock()
				st.Prune(pruneWait, abortWait, pruneMaxChanges)
				st.Unlock()
			}
		}
	})
}

func (o *Overlord) ensureDidRun() {
	atomic.StoreInt32(&o.ensureRun, 1)
}

func (o *Overlord) CanStandby() bool {
	run := atomic.LoadInt32(&o.ensureRun)
	return run != 0
}

// Stop stops the ensure loop and the managers under the StateEngine.
func (o *Overlord) Stop() error {
	o.loopTomb.Kill(nil)
	err := o.loopTomb.Wait()
	o.stateEng.Stop()
	return err
}

func (o *Overlord) settle(timeout time.Duration, beforeCleanups func()) error {
	func() {
		o.ensureLock.Lock()
		defer o.ensureLock.Unlock()
		if o.ensureTimer != nil {
			panic("cannot use Settle concurrently with other Settle or Loop calls")
		}
		o.ensureTimer = time.NewTimer(0)
	}()

	defer func() {
		o.ensureLock.Lock()
		defer o.ensureLock.Unlock()
		o.ensureTimer.Stop()
		o.ensureTimer = nil
	}()

	t0 := time.Now()
	done := false
	var errs []error
	for !done {
		if timeout > 0 && time.Since(t0) > timeout {
			err := fmt.Errorf("Settle is not converging")
			if len(errs) != 0 {
				return &ensureError{append(errs, err)}
			}
			return err
		}
		next := o.ensureTimerReset()
		err := o.stateEng.Ensure()
		switch ee := err.(type) {
		case nil:
		case *ensureError:
			errs = append(errs, ee.errs...)
		default:
			errs = append(errs, err)
		}
		o.stateEng.Wait()
		o.ensureLock.Lock()
		done = o.ensureNext.Equal(next)
		o.ensureLock.Unlock()
		if done {
			if beforeCleanups != nil {
				beforeCleanups()
				beforeCleanups = nil
			}
			// we should wait also for cleanup handlers
			st := o.State()
			st.Lock()
			for _, chg := range st.Changes() {
				if chg.IsReady() && !chg.IsClean() {
					done = false
					break
				}
			}
			st.Unlock()
		}
	}
	if len(errs) != 0 {
		return &ensureError{errs}
	}
	return nil
}

// Settle runs first a state engine Ensure and then wait for
// activities to settle. That's done by waiting for all managers'
// activities to settle while making sure no immediate further Ensure
// is scheduled. It then waits similarly for all ready changes to
// reach the clean state. Chiefly for tests. Cannot be used in
// conjunction with Loop. If timeout is non-zero and settling takes
// longer than timeout, returns an error.
func (o *Overlord) Settle(timeout time.Duration) error {
	return o.settle(timeout, nil)
}

// SettleObserveBeforeCleanups runs first a state engine Ensure and
// then wait for activities to settle. That's done by waiting for all
// managers' activities to settle while making sure no immediate
// further Ensure is scheduled. It then waits similarly for all ready
// changes to reach the clean state, but calls once the provided
// callback before doing that. Chiefly for tests. Cannot be used in
// conjunction with Loop. If timeout is non-zero and settling takes
// longer than timeout, returns an error.
func (o *Overlord) SettleObserveBeforeCleanups(timeout time.Duration, beforeCleanups func()) error {
	return o.settle(timeout, beforeCleanups)
}

// State returns the system state managed by the overlord.
func (o *Overlord) State() *state.State {
	return o.stateEng.State()
}

// StateEngine returns the state engine used by overlord.
func (o *Overlord) StateEngine() *StateEngine {
	return o.stateEng
}

// TaskRunner returns the shared task runner responsible for running
// tasks for all managers under the overlord.
func (o *Overlord) TaskRunner() *state.TaskRunner {
	return o.runner
}

// ServiceManager returns the service manager responsible for services
// under the overlord.
func (o *Overlord) ServiceManager() *servstate.ServiceManager {
	return o.serviceMgr
}

// CommandManager returns the command manager responsible for executing
// commands under the overlord.
func (o *Overlord) CommandManager() *cmdstate.CommandManager {
	return o.commandMgr
}

// CheckManager returns the check manager responsible for running health
// checks under the overlord.
func (o *Overlord) CheckManager() *checkstate.CheckManager {
	return o.checkMgr
}

// Fake creates an Overlord without any managers and with a backend
// not using disk. Managers can be added with AddManager. For testing.
func Fake() *Overlord {
	return FakeWithState(nil)
}

// FakeWithState creates an Overlord without any managers and
// with a backend not using disk. Managers can be added with AddManager. For
// testing.
func FakeWithState(handleRestart func(restart.RestartType)) *Overlord {
	o := &Overlord{
		loopTomb: new(tomb.Tomb),
		inited:   false,
	}
	s := state.New(fakeBackend{o: o})
	o.stateEng = NewStateEngine(s)
	o.runner = state.NewTaskRunner(s)
	return o
}

// AddManager adds a manager to a fake overlord. It cannot be used for
// a normally initialized overlord those are already fully populated.
func (o *Overlord) AddManager(mgr StateManager) {
	if o.inited {
		panic("internal error: cannot add managers to a fully initialized Overlord")
	}
	o.addManager(mgr)
}

type fakeBackend struct {
	o *Overlord
}

func (mb fakeBackend) Checkpoint(data []byte) error {
	return nil
}

func (mb fakeBackend) EnsureBefore(d time.Duration) {
	mb.o.ensureLock.Lock()
	timer := mb.o.ensureTimer
	mb.o.ensureLock.Unlock()
	if timer == nil {
		return
	}

	mb.o.ensureBefore(d)
}

func (mb fakeBackend) RequestRestart(t restart.RestartType) {
	panic("SHOULD NOT BE REACHED")
}
