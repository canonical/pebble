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
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/canonical/x-go/randutil"
	"gopkg.in/tomb.v2"

	"github.com/canonical/pebble/cmd"
	"github.com/canonical/pebble/internals/osutil"
	"github.com/canonical/pebble/internals/overlord/checkstate"
	"github.com/canonical/pebble/internals/overlord/cmdstate"
	"github.com/canonical/pebble/internals/overlord/logstate"
	"github.com/canonical/pebble/internals/overlord/patch"
	"github.com/canonical/pebble/internals/overlord/planstate"
	"github.com/canonical/pebble/internals/overlord/restart"
	"github.com/canonical/pebble/internals/overlord/servstate"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/timing"
)

var (
	ensureInterval = 5 * time.Minute
	pruneInterval  = 10 * time.Minute

	// In snapd this is 24h, but that's too short in the context of Pebble.
	pruneWait = 24 * time.Hour * 7

	// In snapd this is 7d, but also increase that in the context of Pebble.
	abortWait = 24 * time.Hour * 14

	pruneMaxChanges = 500
)

var pruneTickerC = func(t *time.Ticker) <-chan time.Time {
	return t.C
}

// Extension represents an extension of the Overlord.
type Extension interface {
	// ExtraManagers allows additional StateManagers to be used.
	ExtraManagers(o *Overlord) ([]StateManager, error)
}

// Options is the arguments passed to construct an Overlord.
type Options struct {
	// PebbleDir is the path to the pebble directory. It must be provided.
	PebbleDir string
	// LayersDir is the path to the layers directory. It defaults to "<PebbleDir>/layers" if empty.
	LayersDir string
	// RestartHandler is an optional structure to handle restart requests.
	RestartHandler restart.Handler
	// ServiceOutput is an optional output for the logging manager.
	ServiceOutput io.Writer
	// Extension allows extending the overlord with externally defined features.
	Extension Extension
}

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

	startOfOperationTime time.Time

	// managers
	inited     bool
	startedUp  bool
	runner     *state.TaskRunner
	restartMgr *restart.RestartManager
	planMgr    *planstate.PlanManager
	serviceMgr *servstate.ServiceManager
	commandMgr *cmdstate.CommandManager
	checkMgr   *checkstate.CheckManager
	logMgr     *logstate.LogManager

	extension Extension
}

// New creates an Overlord with all its state managers.
func New(opts *Options) (*Overlord, error) {

	o := &Overlord{
		pebbleDir: opts.PebbleDir,
		loopTomb:  new(tomb.Tomb),
		inited:    true,
		extension: opts.Extension,
	}

	if !filepath.IsAbs(o.pebbleDir) {
		return nil, fmt.Errorf("directory %q must be absolute", o.pebbleDir)
	}
	if !osutil.IsDir(o.pebbleDir) {
		return nil, fmt.Errorf("directory %q does not exist", o.pebbleDir)
	}
	if !osutil.IsWritable(o.pebbleDir) {
		return nil, fmt.Errorf("directory %q not writable", o.pebbleDir)
	}

	statePath := filepath.Join(o.pebbleDir, cmd.StateFile)

	backend := &overlordStateBackend{
		path:         statePath,
		ensureBefore: o.ensureBefore,
	}
	s, restartMgr, err := loadState(statePath, opts.RestartHandler, backend)
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

	o.restartMgr = restartMgr
	o.stateEng.AddManager(restartMgr)

	layersDir := opts.LayersDir
	if layersDir == "" {
		layersDir = filepath.Join(opts.PebbleDir, "layers")
	}
	o.planMgr, err = planstate.NewManager(layersDir)
	if err != nil {
		return nil, fmt.Errorf("cannot create plan manager: %w", err)
	}
	o.stateEng.AddManager(o.planMgr)

	o.logMgr = logstate.NewLogManager()

	o.serviceMgr, err = servstate.NewManager(
		s,
		o.runner,
		opts.ServiceOutput,
		opts.RestartHandler,
		o.logMgr)
	if err != nil {
		return nil, fmt.Errorf("cannot create service manager: %w", err)
	}

	// Tell service manager about plan updates.
	o.planMgr.AddChangeListener(o.serviceMgr.PlanChanged)

	o.stateEng.AddManager(o.serviceMgr)
	// The log manager should be stopped after the service manager, because
	// ServiceManager.Stop closes the service ring buffers, which signals to the
	// log manager that it's okay to stop log forwarding.
	o.stateEng.AddManager(o.logMgr)

	o.commandMgr = cmdstate.NewManager(o.runner)
	o.stateEng.AddManager(o.commandMgr)

	o.checkMgr = checkstate.NewManager(s, o.runner, o.planMgr)
	o.stateEng.AddManager(o.checkMgr)

	// Tell check manager about plan updates.
	o.planMgr.AddChangeListener(o.checkMgr.PlanChanged)

	// Tell log manager about plan updates.
	o.planMgr.AddChangeListener(o.logMgr.PlanChanged)

	// Tell service manager about check failures.
	o.checkMgr.NotifyCheckFailed(o.serviceMgr.CheckFailed)

	if o.extension != nil {
		extraManagers, err := o.extension.ExtraManagers(o)
		if err != nil {
			return nil, fmt.Errorf("cannot add extra managers: %w", err)
		}
		for _, manager := range extraManagers {
			o.stateEng.AddManager(manager)
		}
	}

	// TaskRunner must be the last manager added to the StateEngine,
	// because TaskRunner runs all the tasks required by the managers that ran
	// before it.
	o.stateEng.AddManager(o.runner)

	// Load the plan from the Pebble layers directory (which may be missing
	// or have no layers, resulting in an empty plan), and propagate PlanChanged
	// notifications to all notification subscribers.
	err = o.planMgr.Load()
	if err != nil {
		return nil, fmt.Errorf("cannot load plan: %w", err)
	}

	return o, nil
}

func (o *Overlord) Extension() Extension {
	return o.extension
}

func loadState(statePath string, restartHandler restart.Handler, backend state.Backend) (*state.State, *restart.RestartManager, error) {
	timings := timing.Start("", "", map[string]string{"startup": "load-state"})

	curBootID, err := osutil.BootID()
	if err != nil {
		return nil, nil, fmt.Errorf("fatal: cannot find current boot ID: %v", err)
	}
	// If pebble is PID 1 we don't care about /proc/sys/kernel/random/boot_id
	// as we are most likely running in a container. LXD mounts it's own boot_id
	// to correctly emulate the boot_id behaviour of non-containerized systems.
	// Within containerd/docker, boot_id is consistent with the host, which provides
	// us no context of restarts, so instead fallback to /proc/sys/kernel/random/uuid.
	if os.Getpid() == 1 {
		curBootID, err = randutil.RandomKernelUUID()
		if err != nil {
			return nil, nil, fmt.Errorf("fatal: cannot generate psuedo boot-id: %v", err)
		}
	}

	if !osutil.CanStat(statePath) {
		// fail fast, mostly interesting for tests, this dir is set up by pebble
		stateDir := filepath.Dir(statePath)
		if !osutil.IsDir(stateDir) {
			return nil, nil, fmt.Errorf("fatal: directory %q must be present", stateDir)
		}
		s := state.New(backend)
		restartMgr, err := initRestart(s, curBootID, restartHandler)
		if err != nil {
			return nil, nil, err
		}
		patch.Init(s)
		return s, restartMgr, nil
	}
	r, err := os.Open(statePath)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot read the state file: %s", err)
	}
	defer r.Close()

	var s *state.State
	span := timings.StartNested("read-state", "read state from disk")
	s, err = state.ReadState(backend, r)
	span.Stop()
	if err != nil {
		return nil, nil, err
	}

	timings.Stop()
	// TODO Implement function to save timings.
	//s.Lock()
	//perfTimings.Save(s)
	//s.Unlock()

	restartMgr, err := initRestart(s, curBootID, restartHandler)
	if err != nil {
		return nil, nil, err
	}

	// one-shot migrations
	err = patch.Apply(s)
	if err != nil {
		return nil, nil, err
	}
	return s, restartMgr, nil
}

func initRestart(s *state.State, curBootID string, restartHandler restart.Handler) (*restart.RestartManager, error) {
	s.Lock()
	defer s.Unlock()
	return restart.Manager(s, curBootID, restartHandler)
}

func (o *Overlord) StartUp() error {
	if o.startedUp {
		return nil
	}
	o.startedUp = true

	var err error
	st := o.State()
	st.Lock()
	o.startOfOperationTime, err = o.StartOfOperationTime()
	st.Unlock()
	if err != nil {
		return fmt.Errorf("cannot get start of operation time: %s", err)
	}
	return o.stateEng.StartUp()
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
		// While the timer is not setup we have not yet entered the overlord loop.
		// Since the overlord loop will unconditionally perform an ensure on entry,
		// the ensure is already scheduled.
		return
	}
	now := time.Now()
	next := now.Add(d)

	// If this requested ensure must take place before the currently scheduled
	// ensure time, let's reschedule the pending ensure.
	if next.Before(o.ensureNext) {
		o.ensureTimer.Reset(d)
		o.ensureNext = next
		return
	}

	// Go timers do not take sleep/suspend time into account (CLOCK_MONOTONIC,
	// not CLOCK_BOOTTIME). This means that following a wakeup, the timer will
	// only then continue to countdown, while the o.ensureNext wallclock time
	// could point to a time that already expired.
	// https://github.com/golang/go/issues/24595
	// 1. https://github.com/canonical/snapd/pull/1150
	// 2. https://github.com/canonical/snapd/pull/6472
	//
	// If we detect a wake-up condition where the scheduled expiry time is in
	// the past, let's reschedule the ensure to happen right now.
	if o.ensureNext.Before(now) {
		// We have to know if the timer already expired. If this is true then
		// it means a channel write has already taken place, and no further
		// action is required. The overlord loop will ensure soon after this:
		//
		// https://go.dev/wiki/Go123Timer:
		// <  Go 1.23: buffered channel   (overlord loop may not have observed yet)
		// >= Go 1.23: unbuffered channel (overlord loop already observed)
		//
		// In both these cases, the overlord loop ensure will still take place.
		if !o.ensureTimer.Stop() {
			return
		}
		// Since the scheduled time was reached, and the timer did not expire
		// before we called stop, we know that due to a sleep/suspend activity
		// the real timer will expire at some arbitrary point in the future.
		// Instead, let's get that ensure completed right now.
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
			pruneC := pruneTickerC(o.pruneTicker)
			select {
			case <-o.loopTomb.Dying():
				return nil
			case <-o.ensureTimer.C:
			case <-pruneC:
				st := o.State()
				st.Lock()
				st.Prune(o.startOfOperationTime, pruneWait, abortWait, pruneMaxChanges)
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
	if err := o.StartUp(); err != nil {
		return err
	}

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
// longer than timeout, returns an error. Calls StartUp as well.
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
// longer than timeout, returns an error. Calls StartUp as well.
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

// RestartManager returns the manager responsible for restart state.
func (o *Overlord) RestartManager() *restart.RestartManager {
	return o.restartMgr
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

// PlanManager returns the plan manager responsible for managing the global
// system configuration
func (o *Overlord) PlanManager() *planstate.PlanManager {
	return o.planMgr
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
	o.stateEng.AddManager(mgr)
}

var timeNow = time.Now

func (m *Overlord) StartOfOperationTime() (time.Time, error) {
	var opTime time.Time
	err := m.State().Get("start-of-operation-time", &opTime)
	if err == nil {
		return opTime, nil
	}
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return opTime, err
	}
	opTime = timeNow()

	m.State().Set("start-of-operation-time", opTime)
	return opTime, nil
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
