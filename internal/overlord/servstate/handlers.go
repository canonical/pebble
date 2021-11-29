package servstate

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"gopkg.in/tomb.v2"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/osutil"
	"github.com/canonical/pebble/internal/overlord/restart"
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
type serviceState string

const (
	stateInitial     serviceState = "initial"
	stateStarting    serviceState = "starting"
	stateRunning     serviceState = "running"
	stateTerminating serviceState = "terminating"
	stateKilling     serviceState = "killing"
	stateStopped     serviceState = "stopped"
	stateBackoff     serviceState = "backoff"
)

// serviceData holds the state and other data for a service under our control.
type serviceData struct {
	manager     *ServiceManager
	state       serviceState
	config      *plan.Service
	logs        *servicelog.RingBuffer
	started     chan error
	stopped     chan error
	cmd         *exec.Cmd
	backoffNum  int
	backoffTime time.Duration
	resetTimer  *time.Timer
}

func (m *ServiceManager) doStart(task *state.Task, tomb *tomb.Tomb) error {
	m.state.Lock()
	request, err := TaskServiceRequest(task)
	m.state.Unlock()
	if err != nil {
		return err
	}

	releasePlan, err := m.acquirePlan()
	if err != nil {
		return fmt.Errorf("cannot acquire plan lock: %w", err)
	}
	config, ok := m.plan.Services[request.Name]
	releasePlan()
	if !ok {
		return fmt.Errorf("cannot find service %q in plan", request.Name)
	}

	// Create the service object (or reuse the existing one by name).
	service := m.serviceForStart(task, config)
	if service == nil {
		return nil
	}

	// Start the service and transition to stateStarting.
	err = service.start()
	if err != nil {
		m.removeService(config.Name)
		return err
	}

	// Wait for a small amount of time, and if the service hasn't exited,
	// consider it a success.
	select {
	case err := <-service.started:
		if err != nil {
			m.removeService(config.Name)
			return fmt.Errorf("cannot start service: %w", err)
		}
		// Started successfully (ran for small amount of time without exiting).
		return nil
	case <-tomb.Dying():
		// User tried to abort the start, sending SIGKILL to process is about
		// the best we can do.
		m.removeService(config.Name)
		m.servicesLock.Lock()
		defer m.servicesLock.Unlock()
		err := syscall.Kill(-service.cmd.Process.Pid, syscall.SIGKILL)
		if err != nil {
			return fmt.Errorf("start aborted, but cannot send SIGKILL to process: %v", err)
		}
		return fmt.Errorf("start aborted, sent SIGKILL to process")
	}
}

// serviceForStart looks up the service by name in the services map; it
// creates a new service object if one doesn't exist, returns the existing one
// if it already exists but is stopped, or returns nil if it already exists
// and is running.
func (m *ServiceManager) serviceForStart(task *state.Task, config *plan.Service) *serviceData {
	m.servicesLock.Lock()
	defer m.servicesLock.Unlock()

	service := m.services[config.Name]
	if service == nil {
		// Not already started, create a new service object.
		service = &serviceData{
			manager: m,
			state:   stateInitial,
			config:  config.Copy(),
			logs:    servicelog.NewRingBuffer(maxLogBytes),
			started: make(chan error, 1),
			stopped: make(chan error, 2), // enough for killTimeElapsed to send, and exit if it happens after
		}
		m.services[config.Name] = service
		return service
	}

	switch service.state {
	case stateInitial, stateStarting, stateRunning:
		taskLogf(task, "Service %q already started.", config.Name)
		return nil
	case stateBackoff, stateStopped:
		// Start allowed in "backoff" and "stopped" states.
		service.backoffNum = 0
		service.backoffTime = 0
		service.transition(stateInitial)
		return service
	default:
		// Cannot start service while terminating or killing, handle in start().
		return service
	}
}

func taskLogf(task *state.Task, format string, args ...interface{}) {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	task.Logf(format, args...)
}

func (m *ServiceManager) doStop(task *state.Task, tomb *tomb.Tomb) error {
	m.state.Lock()
	request, err := TaskServiceRequest(task)
	m.state.Unlock()
	if err != nil {
		return err
	}

	service := m.serviceForStop(task, request.Name)
	if service == nil {
		return nil
	}

	// Stop service: send SIGTERM, and if that doesn't stop the process in a
	// short time, send SIGKILL.
	err = service.stop()
	if err != nil {
		return err
	}

	for {
		select {
		case err := <-service.stopped:
			if err != nil {
				return fmt.Errorf("cannot stop service: %w", err)
			}
			// Stopped successfully.
			return nil
		case <-tomb.Dying():
			// User tried to abort the stop, but SIGTERM and/or SIGKILL have
			// already been sent to the process, so there's not much more we
			// can do than log it.
			logger.Noticef("Cannot abort stop for service %q, signals already sent", request.Name)
		}
	}
}

// serviceForStop looks up the service by name in the services map; it
// returns the service object if it exists and is running, or nil if it's
// already stopped or has never been started.
func (m *ServiceManager) serviceForStop(task *state.Task, name string) *serviceData {
	m.servicesLock.Lock()
	defer m.servicesLock.Unlock()

	service := m.services[name]
	if service == nil {
		taskLogf(task, "Service %q has never been started.", name)
		return nil
	}

	switch service.state {
	case stateTerminating, stateKilling:
		taskLogf(task, "Service %q already stopping.", name)
		return nil
	case stateStopped:
		taskLogf(task, "Service %q already stopped.", name)
		return nil
	default:
		return service
	}
}

func (m *ServiceManager) removeService(name string) {
	m.servicesLock.Lock()
	defer m.servicesLock.Unlock()

	delete(m.services, name)
}

// transition changes the service's state machine to the given state.
func (s *serviceData) transition(state serviceState) {
	logger.Debugf("Service %q transitioning to state %q", s.config.Name, state)
	s.state = state
}

// start is called to transition from the initial state and start the service.
func (s *serviceData) start() error {
	s.manager.servicesLock.Lock()
	defer s.manager.servicesLock.Unlock()

	switch s.state {
	case stateInitial:
		err := s.startInternal()
		if err != nil {
			return err
		}
		s.transition(stateStarting)
		time.AfterFunc(okayWait, func() { logError(s.okayWaitElapsed()) })

	default:
		return fmt.Errorf("cannot start service while %s", s.state)
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
func (s *serviceData) startInternal() error {
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
		// Use the head iterator so that we copy from where this service
		// started (previous logs have already been copied).
		outputIterator = s.logs.HeadIterator(0)
	}
	logWriter := servicelog.NewFormatWriter(s.logs, s.config.Name)
	s.cmd.Stdout = logWriter
	s.cmd.Stderr = logWriter

	// Start the process!
	logger.Noticef("Service %q starting: %s", s.config.Name, s.config.Command)
	err = s.cmd.Start()
	if err != nil {
		if outputIterator != nil {
			_ = outputIterator.Close()
		}
		_ = s.logs.Close()
		return fmt.Errorf("cannot start service: %w", err)
	}
	s.resetTimer = time.AfterFunc(s.config.BackoffLimit.Value, func() { logError(s.backoffResetElapsed()) })

	// Start a goroutine to wait for the process to finish.
	done := make(chan struct{})
	go func() {
		waitErr := s.cmd.Wait()
		close(done)
		err := s.exited(waitErr)
		if err != nil {
			logger.Noticef("Cannot transition state after service exit: %v", err)
		}
	}()

	// Start a goroutine to read from the service's log buffer and copy to the output.
	if s.manager.serviceOutput != nil {
		go func() {
			defer outputIterator.Close()
			for outputIterator.Next(done) {
				_, err := io.Copy(s.manager.serviceOutput, outputIterator)
				if err != nil {
					logger.Noticef("Service %q log write failed: %v", s.config.Name, err)
				}
			}
		}()
	}

	return nil
}

// okayWaitElapsed is called when the okay-wait timer has elapsed (and the
// service is considered running successfully).
func (s *serviceData) okayWaitElapsed() error {
	s.manager.servicesLock.Lock()
	defer s.manager.servicesLock.Unlock()

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
func (s *serviceData) exited(waitErr error) error {
	s.manager.servicesLock.Lock()
	defer s.manager.servicesLock.Unlock()

	if s.resetTimer != nil {
		s.resetTimer.Stop()
	}

	switch s.state {
	case stateStarting:
		s.started <- fmt.Errorf("exited quickly with code %d", exitCode(s.cmd))
		s.transition(stateStopped) // not strictly necessary as doStart will return, but doesn't hurt

	case stateRunning:
		logger.Noticef("Service %q stopped unexpectedly with code %d", s.config.Name, exitCode(s.cmd))
		action, onType := getAction(s.config, waitErr == nil)
		switch action {
		case plan.ActionIgnore:
			logger.Debugf("Service %q %s action is %q, transitioning to stopped state", s.config.Name, onType, action)
			s.transition(stateStopped)

		case plan.ActionHalt:
			logger.Noticef("Service %q %s action is %q, triggering server exit", s.config.Name, onType, action)
			s.manager.restarter.HandleRestart(restart.RestartDaemon)
			s.transition(stateStopped)

		case plan.ActionRestart:
			s.backoffNum++
			s.backoffTime = calculateNextBackoff(s.config, s.backoffTime)
			logger.Noticef("Service %q %s action is %q, waiting ~%s before restart (backoff %d)",
				s.config.Name, onType, action, s.backoffTime, s.backoffNum)
			s.transition(stateBackoff)
			duration := s.backoffTime + s.manager.getJitter(s.backoffTime)
			time.AfterFunc(duration, func() { logError(s.backoffTimeElapsed()) })

		default:
			return fmt.Errorf("internal error: unexpected action %q", action)
		}

	case stateTerminating, stateKilling:
		logger.Noticef("Service %q stopped", s.config.Name)
		s.stopped <- nil
		s.transition(stateStopped)

	default:
		return fmt.Errorf("internal error: exited invalid in state %q", s.state)
	}
	return nil
}

func calculateNextBackoff(config *plan.Service, current time.Duration) time.Duration {
	if current == 0 {
		// First backoff time
		return config.BackoffDelay.Value
	}
	if current >= config.BackoffLimit.Value {
		// We've already hit the limit.
		return config.BackoffLimit.Value
	}
	// Multiply current time by backoff factor. If it has exceeded the limit, clamp it.
	nextSeconds := current.Seconds() * config.BackoffFactor.Value
	next := time.Duration(nextSeconds * float64(time.Second))
	if next > config.BackoffLimit.Value {
		next = config.BackoffLimit.Value
	}
	return next
}

// getJitter returns a randomized time jitter amount from 0-10% of the duration.
func (m *ServiceManager) getJitter(duration time.Duration) time.Duration {
	m.randLock.Lock()
	defer m.randLock.Unlock()

	maxJitter := duration.Seconds() * 0.1
	jitter := m.rand.Float64() * maxJitter
	return time.Duration(jitter * float64(time.Second))
}

// exitCode returns the exit code of the given command, or 128+signal if it
// exited via a signal.
func exitCode(cmd *exec.Cmd) int {
	status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if ok {
		if status.Signaled() {
			return 128 + int(status.Signal())
		}
		return status.ExitStatus()
	}
	return cmd.ProcessState.ExitCode()
}

// getAction returns the correct action to perform from the plan and whether
// or not the service exited with a success exit code (0).
func getAction(config *plan.Service, success bool) (action plan.ServiceAction, onType string) {
	if success {
		action = config.OnSuccess
		onType = "on-success"
	} else {
		action = config.OnFailure
		onType = "on-failure"
	}
	if action == plan.ActionUnset {
		action = plan.ActionRestart // default for "on-success" and "on-failure"
	}
	return action, onType
}

// sendSignal sends the given signal to a running service. Note that this
// function doesn't lock; it assumes the caller will.
func (s *serviceData) sendSignal(signal string) error {
	switch s.state {
	case stateStarting, stateRunning:
		logger.Noticef("Sending %s to service %q", signal, s.config.Name)
		err := syscall.Kill(-s.cmd.Process.Pid, unix.SignalNum(signal))
		if err != nil {
			return err
		}

	case stateBackoff, stateTerminating, stateKilling, stateStopped:
		return fmt.Errorf("cannot send signal while service is stopped or stopping")

	default:
		return fmt.Errorf("sending signal invalid in state %q", s.state)
	}
	return nil
}

// stop is called to stop a running (or backing off) service.
func (s *serviceData) stop() error {
	s.manager.servicesLock.Lock()
	defer s.manager.servicesLock.Unlock()

	switch s.state {
	case stateRunning:
		logger.Debugf("Attempting to stop service %q by sending SIGTERM", s.config.Name)
		// First send SIGTERM to try to terminate it gracefully.
		err := syscall.Kill(-s.cmd.Process.Pid, syscall.SIGTERM)
		if err != nil {
			logger.Noticef("Cannot send SIGTERM to process: %v", err)
		}
		s.transition(stateTerminating)
		time.AfterFunc(killWait, func() { logError(s.terminateTimeElapsed()) })

	case stateBackoff:
		logger.Noticef("Service %q stopped while waiting for backoff", s.config.Name)
		s.stopped <- nil
		s.transition(stateStopped)

	default:
		return fmt.Errorf("cannot stop service while %s", s.state)
	}
	return nil
}

// backoffTimeElapsed is called when the current backoff's timer has elapsed,
// to restart the service.
func (s *serviceData) backoffTimeElapsed() error {
	s.manager.servicesLock.Lock()
	defer s.manager.servicesLock.Unlock()

	switch s.state {
	case stateBackoff:
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
func (s *serviceData) terminateTimeElapsed() error {
	s.manager.servicesLock.Lock()
	defer s.manager.servicesLock.Unlock()

	switch s.state {
	case stateTerminating:
		logger.Debugf("Attempting to stop service %q again by sending SIGKILL", s.config.Name)
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
func (s *serviceData) killTimeElapsed() error {
	s.manager.servicesLock.Lock()
	defer s.manager.servicesLock.Unlock()

	switch s.state {
	case stateKilling:
		logger.Noticef("Service %q still running after SIGTERM and SIGKILL", s.config.Name)
		s.stopped <- fmt.Errorf("process still running after SIGTERM and SIGKILL")
		s.transition(stateStopped)

	default:
		// Ignore if timer elapsed in any other state.
		return nil
	}
	return nil
}

// backoffResetElapsed is called after the plan's backoff reset has elapsed
// (set to the backoff-limit value), indicating we should reset the backoff
// time because the service is running successfully.
func (s *serviceData) backoffResetElapsed() error {
	s.manager.servicesLock.Lock()
	defer s.manager.servicesLock.Unlock()

	switch s.state {
	case stateRunning:
		logger.Debugf("Service %q backoff reset elapsed, resetting backoff state (was %d: %s)",
			s.config.Name, s.backoffNum, s.backoffTime)
		s.backoffNum = 0
		s.backoffTime = 0

	default:
		// Ignore if timer elapsed in any other state.
		return nil
	}
	return nil
}

var setCmdCredential = func(cmd *exec.Cmd, credential *syscall.Credential) {
	cmd.SysProcAttr.Credential = credential
}
