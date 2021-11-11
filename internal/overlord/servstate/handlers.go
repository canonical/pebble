package servstate

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"gopkg.in/tomb.v2"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/osutil"
	"github.com/canonical/pebble/internal/overlord/state"
	"github.com/canonical/pebble/internal/plan"
	"github.com/canonical/pebble/internal/servicelog"
	"github.com/canonical/pebble/internal/strutil/shlex"
)

// TaskServiceRequest extracts the *ServiceRequest that was associated
// with the provided task when it was created, reflecting details of
// the operation requested.
func TaskServiceRequest(task *state.Task) (*ServiceRequest, error) {
	req := &ServiceRequest{}
	err := task.Get("service-request", req)
	if err != nil && err != state.ErrNoState {
		return nil, err
	}
	if err == nil {
		return req, nil
	}

	var id string
	err = task.Get("service-request-task", &id)
	if err != nil {
		return nil, err
	}

	task = task.State().Task(id)
	if task == nil {
		return nil, fmt.Errorf("internal error: missing task referenced (incorrect pruning?)")
	}
	err = task.Get("service-request", req)
	if err != nil {
		return nil, err
	}
	return req, nil
}

var (
	okayWait = 1 * time.Second
	killWait = 5 * time.Second
	failWait = 10 * time.Second
)

const maxLogBytes = 100 * 1024

// serviceState represents the state a service's state machine is in.
type serviceState int

const (
	stateInitial serviceState = iota
	stateStarting
	stateRunning
	stateBackoffWait
	stateTerminating
	stateKilling
	stateStopped
)

func (s serviceState) String() string {
	switch s {
	case stateInitial:
		return "initial"
	case stateStarting:
		return "starting"
	case stateRunning:
		return "running"
	case stateBackoffWait:
		return "backoff-wait"
	case stateTerminating:
		return "terminating"
	case stateKilling:
		return "killing"
	case stateStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// service holds the information for a service under our control.
type service struct {
	lock         sync.Mutex
	manager      *ServiceManager
	state        serviceState
	config       *plan.Service
	logs         *servicelog.RingBuffer
	started      chan *int
	stopped      chan error
	cmd          *exec.Cmd
	backoffIndex int
	resetTimer   *time.Timer
}

func (m *ServiceManager) doStart(task *state.Task, tomb *tomb.Tomb) error {
	m.state.Lock()
	request, err := TaskServiceRequest(task)
	m.state.Unlock()
	if err != nil {
		return fmt.Errorf("cannot get service request: %w", err)
	}

	releasePlan, err := m.acquirePlan()
	if err != nil {
		return fmt.Errorf("cannot acquire plan lock: %w", err)
	}
	config, ok := m.plan.Services[request.Name]
	if !ok {
		releasePlan()
		return fmt.Errorf("cannot find service %q in plan", request.Name)
	}
	releasePlan()

	m.servicesLock.Lock()
	s := m.services[config.Name]
	if s != nil && s.state != stateStopped {
		m.servicesLock.Unlock()
		m.state.Lock()
		task.Logf("Service %q already started.", config.Name)
		m.state.Unlock()
		return nil
	}
	if s == nil {
		s = &service{
			manager: m,
			state:   stateInitial,
			config:  config.Copy(),
			logs:    servicelog.NewRingBuffer(maxLogBytes),
		}
		m.services[config.Name] = s
	} else {
		s.backoffIndex = 0
		s.transition(stateInitial)
	}
	s.started = make(chan *int, 1)
	m.servicesLock.Unlock()

	// Start the service and transition to stateStarting.
	err = s.start()
	if err != nil {
		m.removeService(config.Name)
		return err
	}

	// Wait for a small amount of time, and if the service hasn't exited,
	// consider it a success.
	timeout := time.After(okayWait + 100*time.Millisecond)
	for {
		select {
		case exitCode := <-s.started:
			if exitCode != nil {
				m.removeService(config.Name)
				return fmt.Errorf("cannot start service: exited quickly with code %d", *exitCode)
			}
			// Started successfully (ran for small amount of time without exiting).
			return nil
		case <-tomb.Dying():
			// Start cancelled (shouldn't really happen, so disallow for simplicity).
			logger.Noticef("Cannot cancel start (service %q)", config.Name)
		case <-timeout:
			// Should never happen, but don't block just in case we get something wrong.
			m.removeService(config.Name)
			return fmt.Errorf("internal error: timed out waiting for start")
		}
	}
}

func (m *ServiceManager) doStop(task *state.Task, tomb *tomb.Tomb) error {
	m.state.Lock()
	request, err := TaskServiceRequest(task)
	m.state.Unlock()
	if err != nil {
		return fmt.Errorf("cannot get service request: %w", err)
	}

	m.servicesLock.Lock()
	s := m.services[request.Name]
	if s == nil || s.state == stateStopped {
		m.servicesLock.Unlock()
		m.state.Lock()
		task.Logf("Service %q already stopped.", request.Name)
		m.state.Unlock()
		return nil
	}
	m.servicesLock.Unlock()

	// Stop service: send SIGTERM, and if that doesn't stop the process in a
	// short time, send SIGKILL.
	err = s.stop()
	if err != nil {
		return err
	}

	timeout := time.After(failWait + 100*time.Millisecond)
	for {
		select {
		case err := <-s.stopped:
			if err != nil {
				return fmt.Errorf("cannot stop service: %w", err)
			}
			// Stopped successfully.
			return nil
		case <-tomb.Dying():
			// Stop cancelled (shouldn't really happen, so disallow for simplicity).
			logger.Noticef("Cannot cancel stop (service %q)", request.Name)
		case <-timeout:
			// Should never happen, but don't block just in case we get something wrong.
			return fmt.Errorf("internal error: timed out waiting for stop")
		}
	}
}

func (m *ServiceManager) removeService(name string) {
	m.servicesLock.Lock()
	delete(m.services, name)
	m.servicesLock.Unlock()
}

// transition changes the service's state machine to the given state.
func (s *service) transition(state serviceState) {
	logger.Debugf("Service %q transitioning to state %q", s.config.Name, state)
	s.state = state
}

// start is called to transition from the initial state and start the service.
func (s *service) start() error {
	s.lock.Lock()
	defer s.lock.Unlock()

	switch s.state {
	case stateInitial:
		err := s.startInternal()
		if err != nil {
			return err
		}
		s.transition(stateStarting)
		time.AfterFunc(okayWait, func() { logError(s.okayWaitElapsed()) })

	default:
		return fmt.Errorf("start invalid in state %q", s.state)
	}
	return nil
}

func logError(err error) {
	if err != nil {
		logger.Noticef("%s", err)
	}
}

// startInternal is an internal helper used to actually start (or restart) the
// command. It assumes the caller has ensures the service is in a valid state,
// and it sets s.cmd and other relevant fields.
func (s *service) startInternal() error {
	args, err := shlex.Split(s.config.Command)
	if err != nil {
		// Shouldn't happen as it should have failed on parsing, but
		// it does not hurt to double check and report.
		return fmt.Errorf("cannot parse service command: %s", err)
	}
	s.cmd = exec.Command(args[0], args[1:]...)
	s.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Start as another user if specified in plan.
	uid, gid, err := osutil.NormalizeUidGid(s.config.UserID, s.config.GroupID, s.config.User, s.config.Group)
	if err != nil {
		return err
	}
	if uid != nil && gid != nil {
		setCmdCredential(s.cmd, &syscall.Credential{
			Uid: uint32(*uid),
			Gid: uint32(*gid),
		})
	}

	// Pass service description's environment variables to child process.
	s.cmd.Env = os.Environ()
	for k, v := range s.config.Environment {
		s.cmd.Env = append(s.cmd.Env, k+"="+v)
	}

	// Set up stdout and stderr to write to log ring buffer.
	var outputIterator servicelog.Iterator
	if s.manager.serviceOutput != nil {
		outputIterator = s.logs.TailIterator()
	}
	logWriter := servicelog.NewFormatWriter(s.logs, s.config.Name)
	s.cmd.Stdout = logWriter
	s.cmd.Stderr = logWriter

	// Start the process!
	err = s.cmd.Start()
	if err != nil {
		if outputIterator != nil {
			_ = outputIterator.Close()
		}
		_ = s.logs.Close()
		return fmt.Errorf("cannot start service: %v", err)
	}
	startTime, _ := s.config.ParseStartTime() // ignore error; it's already been validated
	s.resetTimer = time.AfterFunc(startTime, func() { logError(s.startTimeElapsed()) })

	// Start a goroutine to wait for the process to finish.
	done := make(chan struct{})
	go func() {
		waitErr := s.cmd.Wait()
		close(done)
		err := s.exited(waitErr)
		if err != nil {
			logger.Noticef("Cannot execute exited action: %v", err)
		}
	}()

	// Start a goroutine to read from the service's log buffer and copy to the output.
	if s.manager.serviceOutput != nil {
		go func() {
			defer outputIterator.Close()
			for outputIterator.Next(done) {
				_, err := io.Copy(s.manager.serviceOutput, outputIterator)
				if err != nil {
					logger.Noticef("service %q log write failed: %v", s.config.Name, err)
				}
			}
		}()
	}

	return nil
}

// okayWaitElapsed is called when the okay-wait timer has elapsed (and the
// service is considered running successfully).
func (s *service) okayWaitElapsed() error {
	s.lock.Lock()
	defer s.lock.Unlock()

	switch s.state {
	case stateStarting:
		s.started <- nil // still running fine after short duration, no error
		s.transition(stateRunning)

	default:
		// Ignore if timer elapsed in any other state.
		return nil
	}
	return nil
}

// exited is called when the service's process exits.
func (s *service) exited(err error) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	if s.resetTimer != nil {
		s.resetTimer.Stop()
	}

	switch s.state {
	case stateStarting:
		// TODO: make exit code +128 if signalled, see exec code
		exitCode := s.cmd.ProcessState.ExitCode()
		s.started <- &exitCode

	case stateRunning:
		logger.Noticef("Service %q stopped unexpectedly with code %d", s.config.Name, s.cmd.ProcessState.ExitCode())
		action, onType := s.getAction(err)
		switch action {
		case plan.ActionLog:
			logger.Debugf("Service %q %s action is %q, transitioning to stopped state", s.config.Name, onType, action)
			s.transition(stateStopped)

		case plan.ActionExitPebble:
			logger.Noticef("Service %q %s action is %q, exiting server", s.config.Name, onType, action)
			os.Exit(1) // TODO: figure out more graceful way to tell the daemon to exit

		case plan.ActionRestart:
			backoffDurations, _ := s.config.ParseBackoff() // ignore error; it's already been validated
			if s.backoffIndex >= len(backoffDurations) {
				// No more backoffs, transition to stopped state.
				logger.Noticef("Service %q %s action is %q: no more backoffs", s.config.Name, onType, action)
				s.transition(stateStopped)
				return nil
			}
			duration := backoffDurations[s.backoffIndex]
			logger.Noticef("Service %q %s action is %q, waiting %s before restart (backoff %d/%d)",
				s.config.Name, onType, action, duration, s.backoffIndex+1, len(backoffDurations))
			s.backoffIndex++
			s.transition(stateBackoffWait)
			time.AfterFunc(duration, func() { logError(s.backoffWaitElapsed()) })

		default:
			return fmt.Errorf("internal error: unexpected action %q", action)
		}

	case stateTerminating, stateKilling:
		s.stopped <- nil
		s.transition(stateStopped)

	default:
		return fmt.Errorf("exited invalid in state %q", s.state)
	}
	return nil
}

// getAction returns the correct action to perform from the plan, given an exit error.
func (s *service) getAction(err error) (action plan.ServiceAction, onType string) {
	switch {
	case err != nil && s.config.OnFailure != "":
		return s.config.OnFailure, "on-failure"
	case err == nil && s.config.OnSuccess != "":
		return s.config.OnSuccess, "on-success"
	default:
		onExit := s.config.OnExit
		if onExit == "" {
			onExit = plan.ActionRestart // default for "on-exit"
		}
		return onExit, "on-exit"
	}
}

// sendSignal sends the given signal to a running service.
func (s *service) sendSignal(signal string) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	switch s.state {
	case stateStarting, stateRunning:
		err := syscall.Kill(-s.cmd.Process.Pid, unix.SignalNum(signal))
		if err != nil {
			return err
		}

	case stateBackoffWait, stateTerminating, stateKilling, stateStopped:
		return fmt.Errorf("cannot send signal while service is stopped or stopping")

	default:
		return fmt.Errorf("sendSignal invalid in state %q", s.state)
	}
	return nil
}

// stop is called to stop a running (and backing off) service.
func (s *service) stop() error {
	s.lock.Lock()
	defer s.lock.Unlock()

	switch s.state {
	case stateRunning:
		// First send SIGTERM to try to terminate it gracefully.
		err := syscall.Kill(-s.cmd.Process.Pid, syscall.SIGTERM)
		if err != nil {
			logger.Noticef("cannot send SIGTERM to process: %v", err)
		}
		s.stopped = make(chan error, 1)
		s.transition(stateTerminating)
		time.AfterFunc(killWait, func() { logError(s.terminateTimeElapsed()) })

	case stateBackoffWait:
		s.transition(stateStopped)

	default:
		return fmt.Errorf("stop invalid in state %q", s.state)
	}
	return nil
}

// backoffWaitElapsed is called when the current backoff's timer has elapsed,
// to restart the service.
func (s *service) backoffWaitElapsed() error {
	s.lock.Lock()
	defer s.lock.Unlock()

	switch s.state {
	case stateBackoffWait:
		logger.Debugf("Restarting service %q", s.config.Name)
		err := s.startInternal()
		if err != nil {
			return err
		}
		s.transition(stateRunning)

	default:
		// Ignore if timer elapsed in any other state.
		return nil
	}
	return nil
}

// terminateTimeElapsed is called after stop sends SIGTERM and the service
// still hasn't exited (and we then send SIGTERM).
func (s *service) terminateTimeElapsed() error {
	s.lock.Lock()
	defer s.lock.Unlock()

	switch s.state {
	case stateTerminating:
		// Process hasn't exited after SIGTERM, try SIGKILL.
		err := syscall.Kill(-s.cmd.Process.Pid, syscall.SIGKILL)
		if err != nil {
			logger.Noticef("Cannot send SIGKILL to process: %v", err)
		}
		s.transition(stateKilling)
		time.AfterFunc(failWait-killWait, func() { logError(s.killTimeElapsed()) })

	default:
		// Ignore if timer elapsed in any other state.
		return nil
	}
	return nil
}

// killTimeElapsed is called some time after we've send SIGKILL to acknowledge
// to stop's caller that we can't seem to stop the service.
func (s *service) killTimeElapsed() error {
	s.lock.Lock()
	defer s.lock.Unlock()

	switch s.state {
	case stateKilling:
		s.stopped <- fmt.Errorf("process still running after SIGTERM and SIGKILL")

	default:
		// Ignore if timer elapsed in any other state.
		return nil
	}
	return nil
}

// startTimeElapsed is called after the plan's start-time has elapsed,
// indicating the service is running successfully.
func (s *service) startTimeElapsed() error {
	s.lock.Lock()
	defer s.lock.Unlock()

	switch s.state {
	case stateRunning:
		logger.Debugf("Start time elapsed, resetting backoff counter (was %d)", s.backoffIndex)
		s.backoffIndex = 0

	default:
		// Ignore if timer elapsed in any other state.
		return nil
	}
	return nil
}

var setCmdCredential = func(cmd *exec.Cmd, credential *syscall.Credential) {
	cmd.SysProcAttr.Credential = credential
}
