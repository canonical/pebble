package servstate

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

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

	maxLogBytes = 100 * 1024
)

// Start starts the named service after also starting all of its dependencies.
func (m *ServiceManager) doStart(task *state.Task, tomb *tomb.Tomb) error {
	releasePlan, err := m.acquirePlan()
	if err != nil {
		return fmt.Errorf("cannot acquire plan lock: %w", err)
	}
	defer releasePlan()

	m.state.Lock()
	req, err := TaskServiceRequest(task)
	m.state.Unlock()
	if err != nil {
		return err
	}

	service, ok := m.plan.Services[req.Name]
	if !ok {
		return fmt.Errorf("cannot find service %q in plan", req.Name)
	}

	m.servicesLock.Lock()
	_, ok = m.services[req.Name]
	if ok {
		m.servicesLock.Unlock()
		m.state.Lock()
		task.Logf("Service %q already started.", req.Name)
		m.state.Unlock()
		return nil
	}
	active := &activeService{
		originalPlan: service.Copy(),
	}
	m.services[req.Name] = active
	m.servicesLock.Unlock()

	err = m.startOnce(active, service, m.serviceOutput)
	if err != nil {
		return fmt.Errorf("cannot start service: %w", err)
	}

	releasePlan()

	okay := time.After(okayWait)
	select {
	case <-okay:
		// Service still running after okayWait delay, start goroutine to monitor it.
		go m.monitor(active, service)
		return nil

	case <-active.done:
		// Service died too quickly, return an error.
		m.servicesLock.Lock()
		delete(m.services, req.Name)
		m.servicesLock.Unlock()
		return fmt.Errorf("cannot start service: exited quickly with code %d",
			active.cmd.ProcessState.ExitCode())
	}
}

func (m *ServiceManager) startOnce(active *activeService, service *plan.Service, output io.Writer) error {
	args, err := shlex.Split(service.Command)
	if err != nil {
		// Shouldn't happen as it should have failed on parsing, but
		// it does not hurt to double check and report.
		return fmt.Errorf("cannot parse service command: %s", err)
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Start as another user if specified in plan
	uid, gid, err := osutil.NormalizeUidGid(service.UserID, service.GroupID, service.User, service.Group)
	if err != nil {
		return err
	}
	if uid != nil && gid != nil {
		setCmdCredential(cmd, &syscall.Credential{
			Uid: uint32(*uid),
			Gid: uint32(*gid),
		})
	}

	// Pass service description's environment variables to child process
	cmd.Env = os.Environ()
	for k, v := range service.Environment {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	logBuffer := servicelog.NewRingBuffer(maxLogBytes)
	var outputIterator servicelog.Iterator
	if output != nil {
		outputIterator = logBuffer.TailIterator()
	}

	logWriter := servicelog.NewFormatWriter(logBuffer, service.Name)
	cmd.Stdout = logWriter
	cmd.Stderr = logWriter
	err = cmd.Start()
	if err != nil {
		if outputIterator != nil {
			_ = outputIterator.Close()
		}
		_ = logBuffer.Close()
		return fmt.Errorf("cannot start service: %v", err)
	}

	active.cmd = cmd
	active.done = make(chan struct{})
	active.logBuffer = logBuffer

	// Start a goroutine to wait for the process to finish.
	go func() {
		active.err = cmd.Wait()
		_ = active.logBuffer.Close()
		close(active.done)
	}()

	// Start a goroutine to read from the service's log buffer and copy to the output.
	if output != nil {
		go func() {
			defer outputIterator.Close()
			for outputIterator.Next(active.done) {
				_, err := io.Copy(output, outputIterator)
				if err != nil {
					logger.Noticef("service %q log write failed: %v", service.Name, err)
				}
			}
		}()
	}

	return nil
}

func (m *ServiceManager) monitor(_ *activeService, service *plan.Service) {
	logger.Debugf("Service %q monitor starting", service.Name)
	defer logger.Debugf("Service %q monitor finished", service.Name)

	var prevErr error
	stopMonitor := false

	for !stopMonitor {
		m.servicesLock.Lock()
		active, ok := m.services[service.Name]
		m.servicesLock.Unlock()
		if !ok || active.stopping {
			// Service stopped or stopping, stop monitoring.
			break
		}

		// If there is a running process, wait for it to stop.
		if prevErr == nil {
			<-active.done // TODO: cancel if doStop called
		}

		// Wait the next backoff duration. If there are no more backoff durations, stop.
		if active.backoffIndex >= len(active.originalPlan.BackoffDurations) {
			logger.Noticef("Service %q finished %d auto-restart backoffs", service.Name, active.backoffIndex)
			break
		}
		backoff := active.originalPlan.BackoffDurations[active.backoffIndex]
		active.backoffIndex++
		logger.Noticef("Service %q waiting backoff delay %d/%d: %s",
			service.Name, active.backoffIndex, len(active.originalPlan.BackoffDurations), backoff)
		time.Sleep(backoff) // TODO: cancel if doStop called

		successful := false
		if prevErr == nil {
			switch err := active.err.(type) {
			case nil:
				logger.Noticef("Service %q stopped unexpectedly with code 0", service.Name)
				successful = true
			case *exec.ExitError:
				logger.Noticef("Service %q stopped unexpectedly with code %d", service.Name, err.ExitCode())
			default:
				// TODO: handle signals as 128+signalNum as per exec?
				logger.Noticef("Service %q stopped unexpectedly: %v", service.Name, err)
			}
			active.done = nil
		}

		// TODO: factor this out so logic is testable?
		switch {
		case !successful && service.OnFailure != "":
			stopMonitor, prevErr = m.handleAction(active, service.Name, "on-failure", service.OnFailure)
		case successful && service.OnSuccess != "":
			stopMonitor, prevErr = m.handleAction(active, service.Name, "on-success", service.OnSuccess)
		default:
			onExit := service.OnExit
			if onExit == "" {
				onExit = plan.ActionRestart // default for "on-exit"
			}
			stopMonitor, prevErr = m.handleAction(active, service.Name, "on-exit", onExit)
		}
		if prevErr != nil {
			logger.Noticef("Cannot perform action, retrying: %v", prevErr)
		}
	}

	// TODO: should we remove it from services here?
	m.servicesLock.Lock()
	delete(m.services, service.Name)
	m.servicesLock.Unlock()
}

func (m *ServiceManager) handleAction(active *activeService, serviceName, on string, action plan.ServiceAction) (stopMonitor bool, err error) {
	switch action {
	case plan.ActionRestart:
		logger.Noticef("Service %q %s set to %q, restarting service", serviceName, on, action)
		err := m.restart(active, serviceName)
		if err != nil {
			return false, fmt.Errorf("cannot restart service: %w", err)
		}
		return false, nil

	case plan.ActionExitPebble:
		logger.Noticef("Service %q %s set to %q, exiting Pebble daemon", serviceName, on, action)
		os.Exit(1)        // TODO: more graceful exit
		return false, nil // satisfy compiler (need a return on all code paths)

	case plan.ActionLog:
		logger.Noticef("Service %q %s set to %q, not auto-restarting", serviceName, on, action)
		return true, nil

	default:
		return false, fmt.Errorf("internal error: unexpected action %q", action)
	}
}

func (m *ServiceManager) restart(active *activeService, serviceName string) error {
	releasePlan, err := m.acquirePlan()
	if err != nil {
		return fmt.Errorf("cannot acquire plan lock: %w", err)
	}
	defer releasePlan()

	service, ok := m.plan.Services[serviceName]
	if !ok {
		return fmt.Errorf("cannot find service %q in plan", serviceName)
	}

	err = m.startOnce(active, service, m.serviceOutput)
	if err != nil {
		return fmt.Errorf("cannot start service: %w", err)
	}

	return nil
}

func (m *ServiceManager) doStop(task *state.Task, tomb *tomb.Tomb) error {
	m.state.Lock()
	req, err := TaskServiceRequest(task)
	m.state.Unlock()
	if err != nil {
		return fmt.Errorf("cannot create service request: %w", err)
	}

	m.servicesLock.Lock()
	active, ok := m.services[req.Name]
	if !ok {
		m.servicesLock.Unlock()
		m.state.Lock()
		task.Logf("Service %q already stopped.", req.Name)
		m.state.Unlock()
		return nil
	}
	active.stopping = true
	cmd := active.cmd
	m.servicesLock.Unlock()

	syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)

	// TODO Make these timings configurable in the layer itself.
	kill := time.After(killWait)
	fail := time.After(failWait)
	for {
		time.Sleep(250 * time.Millisecond)
		select {
		case <-kill:
			syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)

		case <-fail:
			return fmt.Errorf("process still running after SIGTERM and SIGKILL")

		case <-active.done:
			m.servicesLock.Lock()
			delete(m.services, req.Name)
			m.servicesLock.Unlock()
			return nil
		}
	}
	// unreachable
}

var setCmdCredential = func(cmd *exec.Cmd, credential *syscall.Credential) {
	cmd.SysProcAttr.Credential = credential
}
