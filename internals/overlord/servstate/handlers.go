package servstate

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"gopkg.in/tomb.v2"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/metrics"
	"github.com/canonical/pebble/internals/osutil"
	"github.com/canonical/pebble/internals/overlord/restart"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/reaper"
	"github.com/canonical/pebble/internals/servicelog"
)

// TaskServiceRequest extracts the *ServiceRequest that was associated
// with the provided task when it was created, reflecting details of
// the operation requested.
func TaskServiceRequest(task *state.Task) (*ServiceRequest, error) {
	req := &ServiceRequest{}
	err := task.Get("service-request", req)
	if err != nil && !errors.Is(err, state.ErrNoState) {
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
	// okayDelay is the time to wait after starting a service before we conclude
	// that it's running successfully.
	okayDelay = 1 * time.Second

	// killDelayDefault is the duration afforded to services for processing
	// SIGTERM signals and shutting down cleanly if the service hasn't specified
	// their own duration.
	killDelayDefault = 5 * time.Second

	// failDelay is the duration given to services for shutting down when Pebble
	// sends a SIGKILL signal.
	failDelay = 5 * time.Second
)

const (
	maxLogBytes  = 100 * 1024
	lastLogLines = 20
)

// serviceState represents the state a service's state machine is in.
//
// See state-diagram.dot (and the generated state-diagram.svg image) for a
// diagram of the states and transitions. Please try to keep these up to date!
type serviceState string

const (
	stateInitial     serviceState = "initial"
	stateStarting    serviceState = "starting"
	stateRunning     serviceState = "running"
	stateTerminating serviceState = "terminating"
	stateKilling     serviceState = "killing"
	stateStopped     serviceState = "stopped"
	stateBackoff     serviceState = "backoff"
	stateExited      serviceState = "exited"
)

// serviceData holds the state and other data for a service under our control.
type serviceData struct {
	manager      *ServiceManager
	state        serviceState
	config       *plan.Service
	logs         *servicelog.RingBuffer
	started      chan error
	stopped      chan error
	cmd          *exec.Cmd
	backoffNum   int
	backoffTime  time.Duration
	resetTimer   *time.Timer
	restarting   bool
	currentSince time.Time
	startCount   atomic.Int64
}

func (m *ServiceManager) doStart(task *state.Task, tomb *tomb.Tomb) error {
	m.state.Lock()
	request, err := TaskServiceRequest(task)
	m.state.Unlock()
	if err != nil {
		return err
	}

	currentPlan := m.getPlan()
	config, ok := currentPlan.Services[request.Name]
	if !ok {
		return fmt.Errorf("cannot find service %q in plan", request.Name)
	}

	// Create the service object (or reuse the existing one by name).
	service, taskLog := m.serviceForStart(config)
	if taskLog != "" {
		addTaskLog(task, taskLog)
	}
	if service == nil {
		return nil
	}

	// Start the service and transition to stateStarting.
	err = service.start()
	if err != nil {
		return err
	}

	// Wait for a small amount of time, and if the service hasn't exited,
	// consider it a success.
	select {
	case err := <-service.started:
		if err != nil {
			addLastLogs(task, service.logs)
			// Do not remove the service so that Pebble will restart it if the action is restart,
			// and the logs are still accessible for failed services if the action is ignore.
			return fmt.Errorf("service start attempt: %w", err)
		}
		// Started successfully (ran for small amount of time without exiting).
		return nil
	case <-tomb.Dying():
		// User tried to abort the start, sending SIGKILL to process is about
		// the best we can do.
		m.servicesLock.Lock()
		defer m.servicesLock.Unlock()
		service.transition(stateStopped)
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
//
// It also returns a message to add to the task's log, or empty string if none.
func (m *ServiceManager) serviceForStart(config *plan.Service) (service *serviceData, taskLog string) {
	m.servicesLock.Lock()
	defer m.servicesLock.Unlock()

	service = m.services[config.Name]
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
		return service, ""
	}

	// Ensure config is up-to-date from the plan whenever the user starts a service.
	service.config = config.Copy()

	switch service.state {
	case stateInitial, stateStarting, stateRunning:
		return nil, fmt.Sprintf("Service %q already started.", config.Name)
	case stateBackoff, stateStopped, stateExited:
		// Start allowed when service is backing off, was stopped, or has exited.
		service.backoffNum = 0
		service.backoffTime = 0
		service.transition(stateInitial)
		return service, ""
	default:
		// Cannot start service while terminating or killing, handle in start().
		return service, ""
	}
}

func addTaskLog(task *state.Task, message string) {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	task.Logf("%s", message)
}

func (m *ServiceManager) doStop(task *state.Task, tomb *tomb.Tomb) error {
	m.state.Lock()
	request, err := TaskServiceRequest(task)
	m.state.Unlock()
	if err != nil {
		return err
	}

	service, taskLog := m.serviceForStop(request.Name)
	if taskLog != "" {
		addTaskLog(task, taskLog)
	}
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
//
// It also returns a message to add to the task's log, or empty string if none.
func (m *ServiceManager) serviceForStop(name string) (service *serviceData, taskLog string) {
	m.servicesLock.Lock()
	defer m.servicesLock.Unlock()

	service = m.services[name]
	if service == nil {
		return nil, fmt.Sprintf("Service %q has never been started.", name)
	}

	switch service.state {
	case stateTerminating, stateKilling:
		return nil, fmt.Sprintf("Service %q already stopping.", name)
	case stateStopped:
		return nil, fmt.Sprintf("Service %q already stopped.", name)
	case stateExited:
		service.transition(stateStopped)
		return nil, fmt.Sprintf("Service %q had already exited.", name)
	default:
		return service, ""
	}
}

// transition changes the service's state machine to the given state.
func (s *serviceData) transition(state serviceState) {
	logger.Debugf("Service %q transitioning to state %q", s.config.Name, state)
	s.transitionRestarting(state, false)
}

// transitionRestarting changes the service's state and also sets the restarting flag.
func (s *serviceData) transitionRestarting(state serviceState, restarting bool) {
	// Update current-since time if derived status is changing.
	oldStatus := stateToStatus(s.state)
	newStatus := stateToStatus(state)
	if oldStatus != newStatus {
		s.currentSince = time.Now()
	}

	s.state = state
	s.restarting = restarting
}

// start is called to transition from the initial state and start the service.
func (s *serviceData) start() error {
	s.manager.servicesLock.Lock()
	defer s.manager.servicesLock.Unlock()

	switch s.state {
	case stateInitial:
		err := s.startInternal()
		if err != nil {
			s.transition(stateStopped)
			return err
		}
		s.transition(stateStarting)
		time.AfterFunc(okayDelay, func() { logError(s.okayWaitElapsed()) })

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
	base, extra, err := s.config.ParseCommand()
	if err != nil {
		return err
	}
	args := append(base, extra...)
	s.cmd = exec.Command(args[0], args[1:]...)
	s.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Copy environment to avoid updating original.
	environment := make(map[string]string)
	for k, v := range s.config.Environment {
		environment[k] = v
	}

	s.cmd.Dir = s.config.WorkingDir

	// Start as another user if specified in plan.
	uid, gid, err := osutil.NormalizeUidGid(s.config.UserID, s.config.GroupID, s.config.User, s.config.Group)
	if err != nil {
		return err
	}
	if uid != nil && gid != nil {
		isCurrent, err := osutil.IsCurrent(*uid, *gid)
		if err != nil {
			logger.Debugf("Cannot determine if uid %d gid %d is current user", *uid, *gid)
		}
		if !isCurrent {
			setCmdCredential(s.cmd, &syscall.Credential{
				Uid: uint32(*uid),
				Gid: uint32(*gid),
			})
		}
	}

	// Also set HOME and USER if not explicitly specified in config.
	if uid != nil && (environment["HOME"] == "" || environment["USER"] == "") {
		u, err := user.LookupId(strconv.Itoa(*uid))
		if err != nil {
			logger.Noticef("Cannot look up user %d: %v", *uid, err)
		} else {
			if environment["HOME"] == "" {
				environment["HOME"] = u.HomeDir
			}
			if environment["USER"] == "" {
				environment["USER"] = u.Username
			}
		}
	}

	// Pass service description's environment variables to child process.
	s.cmd.Env = os.Environ()
	for k, v := range environment {
		s.cmd.Env = append(s.cmd.Env, k+"="+v)
	}

	// Set up stdout and stderr to write to log ring buffer.
	var outputIterator servicelog.Iterator
	if s.manager.serviceOutput != nil {
		// Use the head iterator so that we copy from where this service
		// started (previous logs have already been copied).
		outputIterator = s.logs.HeadIterator(0)
	}
	serviceName := s.config.Name
	logWriter := servicelog.NewFormatWriter(s.logs, serviceName)
	s.cmd.Stdout = logWriter
	s.cmd.Stderr = logWriter

	// Add WaitDelay to ensure cmd.Wait() returns in a reasonable timeframe if
	// the goroutines that cmd.Start() uses to copy Stdin/Stdout/Stderr are
	// blocked when copying due to a sub-subprocess holding onto them. This
	// only happens if the sub-subprocess uses setsid or setpgid to change
	// the process group. Read more details in these issues:
	//
	// - https://github.com/golang/go/issues/23019
	// - https://github.com/golang/go/issues/50436
	//
	// This isn't the original intent of kill-delay, but it seems reasonable
	// to reuse it in this context. Use a value slightly less than the kill
	// delay (90% of it) to avoid racing with trying to send SIGKILL (to a
	// process that has already exited).
	s.cmd.WaitDelay = s.killDelay() * 9 / 10 // will only overflow if kill-delay is 32 years!

	// Start the process!
	logger.Noticef("Service %q starting: %s", serviceName, s.config.Command)
	err = reaper.StartCommand(s.cmd)
	if err != nil {
		if outputIterator != nil {
			_ = outputIterator.Close()
		}
		return fmt.Errorf("cannot start service: %w", err)
	}
	logger.Debugf("Service %q started with PID %d", serviceName, s.cmd.Process.Pid)
	s.resetTimer = time.AfterFunc(s.config.BackoffLimit.Value, func() { logError(s.backoffResetElapsed()) })

	// Start a goroutine to wait for the process to finish.
	done := make(chan struct{})
	cmd := s.cmd
	go func() {
		exitCode, waitErr := reaper.WaitCommand(cmd)
		if waitErr != nil {
			logger.Noticef("Cannot wait for service %q: %v", serviceName, waitErr)
		} else {
			logger.Debugf("Service %q exited with code %d.", serviceName, exitCode)
		}
		close(done)
		err := s.exited(exitCode)
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
					logger.Noticef("Service %q log write failed: %v", serviceName, err)
				}
			}
		}()
	}

	// Pass buffer reference to logMgr to start log forwarding
	s.manager.logMgr.ServiceStarted(s.config, s.logs)
	s.startCount.Add(1)
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
func (s *serviceData) exited(exitCode int) error {
	s.manager.servicesLock.Lock()
	defer s.manager.servicesLock.Unlock()

	if s.resetTimer != nil {
		s.resetTimer.Stop()
	}

	switch s.state {
	case stateStarting:
		// Send error to select waiting in doStart, then fall through to perform action.
		action, _ := getAction(s.config, exitCode == 0)
		s.started <- fmt.Errorf("exited quickly with code %d, will %s", exitCode, action)
		fallthrough

	case stateRunning:
		logger.Noticef("Service %q stopped unexpectedly with code %d", s.config.Name, exitCode)
		action, onType := getAction(s.config, exitCode == 0)
		switch action {
		case plan.ActionIgnore:
			logger.Noticef("Service %q %s action is %q, not doing anything further", s.config.Name, onType, action)
			// On success we transition to state stopped.
			if exitCode == 0 {
				s.transition(stateStopped)
				break
			}
			s.transition(stateExited)

		case plan.ActionShutdown:
			shutdownStr := "success"
			restartType := restart.RestartDaemon
			if exitCode != 0 {
				shutdownStr = "failure"
				restartType = restart.RestartServiceFailure
			}
			logger.Noticef("Service %q %s action is %q, triggering %s shutdown",
				s.config.Name, onType, action, shutdownStr)
			s.manager.restarter.HandleRestart(restartType)
			s.transition(stateExited)

		case plan.ActionSuccessShutdown:
			logger.Noticef("Service %q %s action is %q, triggering success shutdown", s.config.Name, onType, action)
			s.manager.restarter.HandleRestart(restart.RestartDaemon)
			s.transition(stateExited)

		case plan.ActionFailureShutdown:
			logger.Noticef("Service %q %s action is %q, triggering failure shutdown", s.config.Name, onType, action)
			s.manager.restarter.HandleRestart(restart.RestartServiceFailure)
			s.transition(stateExited)

		case plan.ActionRestart:
			s.doBackoff(action, onType)

		default:
			return fmt.Errorf("internal error: unexpected action %q", action)
		}

	case stateTerminating, stateKilling:
		if s.restarting {
			logger.Noticef("Service %q exited after check failure, restarting", s.config.Name)
			s.doBackoff(plan.ActionRestart, "on-check-failure")
		} else {
			logger.Noticef("Service %q stopped", s.config.Name)
			s.stopped <- nil
			s.transition(stateStopped)
		}

	default:
		return fmt.Errorf("internal error: exited invalid in state %q", s.state)
	}
	return nil
}

// addLastLogs adds the last few lines of service output to the task's log.
func addLastLogs(task *state.Task, logBuffer *servicelog.RingBuffer) {
	st := task.State()
	st.Lock()
	defer st.Unlock()

	logs, err := servicelog.LastLines(logBuffer, lastLogLines, "    ", true)
	if err != nil {
		task.Errorf("Cannot read service logs: %v", err)
	}
	if logs != "" {
		// Add last few lines of service output to the task's log.
		task.Logf("Most recent service output:\n%s", logs)
	}
}

func (s *serviceData) doBackoff(action plan.ServiceAction, onType string) {
	s.backoffNum++
	s.backoffTime = calculateNextBackoff(s.config, s.backoffTime)
	logger.Noticef("Service %q %s action is %q, waiting ~%s before restart (backoff %d)",
		s.config.Name, onType, action, s.backoffTime, s.backoffNum)
	s.transition(stateBackoff)
	duration := s.backoffTime + s.manager.getJitter(s.backoffTime)
	time.AfterFunc(duration, func() { logError(s.backoffTimeElapsed()) })
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
		sig := unix.SignalNum(signal)
		if sig == 0 {
			return fmt.Errorf("invalid signal name %q", signal)
		}
		logger.Noticef("Sending %s to service %q", signal, s.config.Name)
		err := syscall.Kill(s.cmd.Process.Pid, sig)
		if err != nil {
			return err
		}

	case stateBackoff, stateTerminating, stateKilling, stateStopped, stateExited:
		return fmt.Errorf("service is not running")

	default:
		return fmt.Errorf("invalid in state %q", s.state)
	}
	return nil
}

// killDelay reports the duration that this service should be given when being
// asked to shut down gracefully before being force-terminated. The value
// returned will either be the service's pre-configured value, or the default
// kill delay if that is not set.
func (s *serviceData) killDelay() time.Duration {
	if s.config.KillDelay.IsSet {
		return s.config.KillDelay.Value
	}
	return killDelayDefault
}

// stop is called to stop a running (or backing off) service.
func (s *serviceData) stop() error {
	s.manager.servicesLock.Lock()
	defer s.manager.servicesLock.Unlock()

	switch s.state {
	case stateStarting:
		s.started <- fmt.Errorf("stopped before the %s okay delay", okayDelay)
		fallthrough

	case stateRunning:
		logger.Debugf("Attempting to stop service %q by sending SIGTERM", s.config.Name)
		// First send SIGTERM to try to terminate it gracefully.
		err := syscall.Kill(-s.cmd.Process.Pid, syscall.SIGTERM)
		if err != nil {
			logger.Noticef("Cannot send SIGTERM to process: %v", err)
		}
		s.transition(stateTerminating)
		time.AfterFunc(s.killDelay(), func() { logError(s.terminateTimeElapsed()) })

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

		s.transitionRestarting(stateKilling, s.restarting)
		time.AfterFunc(failDelay, func() { logError(s.killTimeElapsed()) })

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
		if s.restarting {
			logger.Noticef("Service %q still running after SIGTERM and SIGKILL", s.config.Name)
			s.transition(stateStopped)
		} else {
			logger.Noticef("Service %q still running after SIGTERM and SIGKILL", s.config.Name)
			s.stopped <- fmt.Errorf("process still running after SIGTERM and SIGKILL")
			s.transition(stateStopped)
		}

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

// checkFailed handles a health check failure (from the check manager).
func (s *serviceData) checkFailed(action plan.ServiceAction) {
	switch s.state {
	case stateRunning, stateBackoff, stateExited:
		onType := "on-check-failure"
		switch action {
		case plan.ActionIgnore:
			logger.Debugf("Service %q %s action is %q, remaining in current state", s.config.Name, onType, action)

		case plan.ActionShutdown:
			logger.Noticef("Service %q %s action is %q, triggering failure shutdown", s.config.Name, onType, action)
			s.manager.restarter.HandleRestart(restart.RestartCheckFailure)

		case plan.ActionSuccessShutdown:
			logger.Noticef("Service %q %s action is %q, triggering success shutdown", s.config.Name, onType, action)
			s.manager.restarter.HandleRestart(restart.RestartDaemon)

		case plan.ActionRestart:
			switch s.state {
			case stateRunning:
				logger.Noticef("Service %q %s action is %q, terminating process before restarting",
					s.config.Name, onType, action)
				err := syscall.Kill(-s.cmd.Process.Pid, syscall.SIGTERM)
				if err != nil {
					logger.Noticef("Cannot send SIGTERM to process: %v", err)
				}
				s.transitionRestarting(stateTerminating, true)
				time.AfterFunc(s.killDelay(), func() { logError(s.terminateTimeElapsed()) })
			case stateBackoff:
				logger.Noticef("Service %q %s action is %q, waiting for current backoff",
					s.config.Name, onType, action)
				return
			case stateExited:
				s.doBackoff(action, onType)
			}

		default:
			logger.Noticef("Internal error: unexpected action %q handling check failure for service %q",
				action, s.config.Name)
		}

	default:
		logger.Debugf("Service %q: ignoring on-check-failure action %q in state %s",
			s.config.Name, action, s.state)
	}
}

// writeMetrics writes the service's metrics.
func (d *serviceData) writeMetrics(writer metrics.Writer) error {
	err := writer.Write(metrics.Metric{
		Name:       "pebble_service_start_count",
		Type:       metrics.TypeCounterInt,
		ValueInt64: d.startCount.Load(),
		Comment:    "Number of times the service has started",
		Labels:     []metrics.Label{metrics.NewLabel("service", d.config.Name)},
	})
	if err != nil {
		return err
	}

	active := 0
	if stateToStatus(d.state) == StatusActive {
		active = 1
	}
	err = writer.Write(metrics.Metric{
		Name:       "pebble_service_active",
		Type:       metrics.TypeGaugeInt,
		ValueInt64: int64(active),
		Comment:    "Whether the service is currently active (1) or not (0)",
		Labels:     []metrics.Label{metrics.NewLabel("service", d.config.Name)},
	})
	if err != nil {
		return err
	}

	return nil
}

var setCmdCredential = func(cmd *exec.Cmd, credential *syscall.Credential) {
	cmd.SysProcAttr.Credential = credential
}
