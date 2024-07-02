package servstate

import (
	"fmt"

	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
)

// ServiceRequest holds the details required to perform service tasks.
type ServiceRequest struct {
	Name string
}

func get_or_create_lane(s *state.State, service *plan.Service, service_lane_mapping map[string]int) int {
	// if the service has been mapped to a lane
	if lane, ok := service_lane_mapping[service.Name]; ok {
		return lane
	}

	// if any dependency has been mapped to a lane
	all_dependencies := append(append(service.Requires, service.Before...), service.After...)
	for _, dependency := range all_dependencies {
		if lane, ok := service_lane_mapping[dependency]; ok {
			return lane
		}
	}

	// neither the service itself nor any of its dependencies is mapped to an existing lane
	return s.NewLane()
}

func joinLane(s *state.State, task *state.Task, service *plan.Service, service_lane_mapping map[string]int, lane_tasks_mapping map[int][]*state.Task) {
	lane := get_or_create_lane(s, service, service_lane_mapping)

	task.JoinLane(lane)

	// map task to lane
	if _, ok := lane_tasks_mapping[lane]; !ok {
		lane_tasks_mapping[lane] = nil
	}
	lane_tasks_mapping[lane] = append(lane_tasks_mapping[lane], task)

	// map the service's dependencies to the same lane
	service_lane_mapping[service.Name] = lane
	all_dependencies := append(append(service.Requires, service.Before...), service.After...)
	for _, dependency := range all_dependencies {
		service_lane_mapping[dependency] = lane
	}
}

func handleWaitFor(lane_tasks_mapping map[int][]*state.Task) {
	for _, tasks := range lane_tasks_mapping {
		for i := 1; i < len(tasks); i++ {
			tasks[i].WaitFor(tasks[i-1])
		}
	}
}

// Start creates and returns a task set for starting the given services.
func Start(s *state.State, names []string, m *ServiceManager) (*state.TaskSet, error) {
	services := m.getPlan().Services
	service_lane_mapping := make(map[string]int)
	lane_tasks_mapping := make(map[int][]*state.Task)

	var tasks []*state.Task

	for _, name := range names {
		service, ok := services[name]
		if !ok {
			return nil, fmt.Errorf("service %q does not exist", name)
		}

		task := s.NewTask("start", fmt.Sprintf("Start service %q", name))
		req := ServiceRequest{
			Name: name,
		}
		task.Set("service-request", &req)
		joinLane(s, task, service, service_lane_mapping, lane_tasks_mapping)

		tasks = append(tasks, task)
	}

	handleWaitFor(lane_tasks_mapping)

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

// StopRunning creates and returns a task set for stopping all running
// services. It returns a nil *TaskSet if there are no services to stop.
func StopRunning(s *state.State, m *ServiceManager) (*state.TaskSet, error) {
	services, err := servicesToStop(m)
	if err != nil {
		return nil, err
	}
	if len(services) == 0 {
		return nil, nil
	}

	// One change to stop them all.
	s.Lock()
	defer s.Unlock()
	taskSet, err := Stop(s, services)
	if err != nil {
		return nil, err
	}
	return taskSet, nil
}
