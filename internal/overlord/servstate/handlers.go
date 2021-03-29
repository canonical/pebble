package servstate

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/canonical/pebble/internal/overlord/state"
	"github.com/canonical/pebble/internal/strutil"
	"github.com/canonical/pebble/internal/strutil/shlex"

	"gopkg.in/tomb.v2"
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

// Start starts the named service after also starting all of its dependencies.
func (m *ServiceManager) doStart(task *state.Task, tomb *tomb.Tomb) error {
	releasePlan, err := m.acquirePlan()
	if err != nil {
		return err
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

	_, previous := m.services[req.Name]
	if previous {
		return fmt.Errorf("service %q was previously started", req.Name)
	}

	args, err := shlex.Split(service.Command)
	if err != nil {
		// Shouldn't happen as it should have failed on parsing, but
		// it does not hurt to double check and report.
		return fmt.Errorf("cannot parse service command: %s", err)
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Pass service description's environment variables to child process
	cmd.Env = os.Environ()
	for k, v := range service.Environment {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	buffer := strutil.NewLimitedBuffer(160, 10*1024)
	cmd.Stdout = buffer
	cmd.Stderr = buffer
	err = cmd.Start()
	if err != nil {
		return fmt.Errorf("cannot start service: %v", err)
	}

	active := &activeService{
		cmd:  cmd,
		done: make(chan struct{}),
	}
	m.services[req.Name] = active
	go func() {
		active.err = cmd.Wait()
		close(active.done)
	}()

	releasePlan()

	okay := time.After(okayWait)
	select {
	case <-okay:
		return nil
	case <-active.done:
		releasePlan, err := m.acquirePlan()
		if err == nil {
			if m.services[req.Name].cmd == cmd {
				delete(m.services, req.Name)
			}
			releasePlan()
		}
		return fmt.Errorf("cannot start service: exited quickly with code %d", cmd.ProcessState.ExitCode())
	}
	panic("unreachable")
}

func (m *ServiceManager) doStop(task *state.Task, tomb *tomb.Tomb) error {
	releasePlan, err := m.acquirePlan()
	if err != nil {
		return err
	}
	defer releasePlan()

	m.state.Lock()
	req, err := TaskServiceRequest(task)
	m.state.Unlock()
	if err != nil {
		return err
	}

	active, ok := m.services[req.Name]
	if !ok {
		return fmt.Errorf("service %q is not active", req.Name)
	}
	cmd := active.cmd

	releasePlan()

	syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	//cmd.Process.Signal(syscall.SIGTERM)

	// TODO Make these timings configurable in the layer itself.
	kill := time.After(killWait)
	fail := time.After(failWait)
	for {
		time.Sleep(250 * time.Millisecond)
		select {
		case <-kill:
			syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			//cmd.Process.Signal(syscall.SIGKILL)
		case <-fail:
			return fmt.Errorf("process still runs after SIGTERM and SIGKILL")
		case <-active.done:
			releasePlan, err := m.acquirePlan()
			if err == nil {
				if m.services[req.Name].cmd == cmd {
					delete(m.services, req.Name)
				}
				releasePlan()
			}
			return nil
		}
	}
	panic("unreachable")
}
