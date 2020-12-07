package servstate

import (
	"github.com/canonical/pebble/internal/overlord/state"

	"fmt"
)

// ServiceRequest holds the details required to perform service tasks.
type ServiceRequest struct {
	Name string
}

// Start creates and returns a task set for starting the given services.
func Start(s *state.State, services []string) (*state.TaskSet, error) {
	var tasks []*state.Task
	for _, name := range services {
		task := s.NewTask("start", fmt.Sprintf("Start service %q", name))
		req := ServiceRequest{
			Name: name,
		}
		task.Set("service-request", &req)
		if len(tasks) > 0 {
			// TODO Allow non-dependent services to start in parallel.
			task.WaitFor(tasks[len(tasks)-1])
		}
		tasks = append(tasks, task)
	}
	return state.NewTaskSet(tasks...), nil
}

// Stop creates and returns a task set for stopping the given services.
func Stop(s *state.State, services []string) (*state.TaskSet, error) {
	var tasks []*state.Task
	for _, name := range services {
		task := s.NewTask("stop", fmt.Sprintf("Stop service %q", name))
		req := ServiceRequest{
			Name: name,
		}
		task.Set("service-request", &req)
		if len(tasks) > 1 {
			// TODO Allow non-dependent services to stop in parallel.
			task.WaitFor(tasks[len(tasks)-1])
		}
		tasks = append(tasks, task)
	}
	return state.NewTaskSet(tasks...), nil
}
