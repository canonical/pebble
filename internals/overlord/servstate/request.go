package servstate

import (
	"fmt"

	"github.com/canonical/pebble/internals/overlord/state"
)

// ServiceRequest holds the details required to perform service tasks.
type ServiceRequest struct {
	Name string
}

// Start creates and returns a task set for starting the given services.
func Start(s *state.State, lanes [][]string) (*state.TaskSet, error) {
	var tasks []*state.Task
	for _, services := range lanes {
		lane := s.NewLane()
		for i, name := range services {
			task := s.NewTask("start", fmt.Sprintf("Start service %q", name))
			req := ServiceRequest{
				Name: name,
			}
			task.Set("service-request", &req)
			task.JoinLane(lane)
			// Wait for the previous task in the same lane.
			if i > 0 {
				task.WaitFor(tasks[len(tasks)-1])
			}
			tasks = append(tasks, task)
		}
	}
	return state.NewTaskSet(tasks...), nil
}

// Stop creates and returns a task set for stopping the given services.
func Stop(s *state.State, lanes [][]string) (*state.TaskSet, error) {
	var tasks []*state.Task
	for _, services := range lanes {
		lane := s.NewLane()
		for i, name := range services {
			task := s.NewTask("stop", fmt.Sprintf("Stop service %q", name))
			req := ServiceRequest{
				Name: name,
			}
			task.Set("service-request", &req)
			task.JoinLane(lane)
			// Wait for the previous task in the same lane.
			if i > 0 {
				task.WaitFor(tasks[len(tasks)-1])
			}
			tasks = append(tasks, task)
		}
	}
	return state.NewTaskSet(tasks...), nil
}

// StopRunning creates and returns a task set for stopping all running
// services. It returns a nil *TaskSet if there are no services to stop.
func StopRunning(s *state.State, m *ServiceManager) (*state.TaskSet, error) {
	lanes, err := servicesToStop(m)
	if err != nil {
		return nil, err
	}
	if len(lanes) == 0 {
		return nil, nil
	}

	// One change to stop them all.
	s.Lock()
	defer s.Unlock()
	taskSet, err := Stop(s, lanes)
	if err != nil {
		return nil, err
	}
	return taskSet, nil
}
