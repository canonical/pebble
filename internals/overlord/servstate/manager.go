package servstate

import (
	"fmt"
	"io"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/canonical/pebble/internals/metrics"
	"github.com/canonical/pebble/internals/overlord/restart"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/servicelog"
)

type ServiceManager struct {
	state *state.State

	planLock sync.Mutex
	plan     *plan.Plan

	servicesLock sync.Mutex
	services     map[string]*serviceData

	serviceOutput io.Writer
	restarter     Restarter

	randLock sync.Mutex
	rand     *rand.Rand

	logMgr LogManager
}

type LogManager interface {
	ServiceStarted(service *plan.Service, logs *servicelog.RingBuffer)
}

type Restarter interface {
	HandleRestart(t restart.RestartType)
}

func NewManager(s *state.State, runner *state.TaskRunner, serviceOutput io.Writer, restarter Restarter, logMgr LogManager) (*ServiceManager, error) {
	manager := &ServiceManager{
		state:         s,
		services:      make(map[string]*serviceData),
		serviceOutput: serviceOutput,
		restarter:     restarter,
		rand:          rand.New(rand.NewSource(time.Now().UnixNano())),
		logMgr:        logMgr,
	}

	runner.AddHandler("start", manager.doStart, nil)
	runner.AddHandler("stop", manager.doStop, nil)

	return manager, nil
}

// PlanChanged informs the service manager that the plan has been updated.
func (m *ServiceManager) PlanChanged(plan *plan.Plan) {
	m.planLock.Lock()
	defer m.planLock.Unlock()
	m.plan = plan
}

// getPlan returns the current plan pointer in a concurrency-safe way. The
// service manager must not mutate the result.
func (m *ServiceManager) getPlan() *plan.Plan {
	m.planLock.Lock()
	defer m.planLock.Unlock()
	// This should never be possible, but lets make the requirements clear to
	// catch misuse during development. Managers using the plan must receive
	// a PlanChanged update before the plan is used. The first update will be
	// received during stateengine StartUp, after the plan manager loads the
	// plan layers from storage.
	if m.plan == nil {
		panic("service manager with invalid plan state")
	}
	return m.plan
}

// Ensure implements StateManager.Ensure.
func (m *ServiceManager) Ensure() error {
	return nil
}

type ServiceInfo struct {
	Name         string
	Startup      ServiceStartup
	Current      ServiceStatus
	CurrentSince time.Time
}

type ServiceStartup string

const (
	StartupEnabled  = "enabled"
	StartupDisabled = "disabled"
)

type ServiceStatus string

const (
	StatusActive   ServiceStatus = "active"
	StatusBackoff  ServiceStatus = "backoff"
	StatusError    ServiceStatus = "error"
	StatusInactive ServiceStatus = "inactive"
)

// Services returns the list of configured services and their status, sorted
// by service name. Filter by the specified service names if provided.
func (m *ServiceManager) Services(names []string) ([]*ServiceInfo, error) {
	currentPlan := m.getPlan()
	m.servicesLock.Lock()
	defer m.servicesLock.Unlock()

	requested := make(map[string]bool, len(names))
	for _, name := range names {
		requested[name] = true
	}

	var services []*ServiceInfo
	matchNames := len(names) > 0
	for name, config := range currentPlan.Services {
		if matchNames && !requested[name] {
			continue
		}
		info := &ServiceInfo{
			Name:    name,
			Startup: StartupDisabled,
			Current: StatusInactive,
		}
		if config.Startup == plan.StartupEnabled {
			info.Startup = StartupEnabled
		}
		if s, ok := m.services[name]; ok {
			info.Current = stateToStatus(s.state)
			info.CurrentSince = s.currentSince
		}
		services = append(services, info)
	}
	sort.Slice(services, func(i, j int) bool {
		return services[i].Name < services[j].Name
	})
	return services, nil
}

// StopTimeout returns the worst case duration that will have to be waited for
// to have all services in this manager stopped.
func (m *ServiceManager) StopTimeout() time.Duration {
	m.servicesLock.Lock()
	defer m.servicesLock.Unlock()

	maxDuration := killDelayDefault
	for _, service := range m.services {
		if service == nil {
			continue
		}
		switch service.state {
		case stateStarting, stateRunning, stateTerminating:
			if service.killDelay() > maxDuration {
				maxDuration = service.killDelay()
			}
		}
	}

	// We add a little extra time here to allow for signals to be sent and
	// processed.
	return maxDuration + failDelay + 100*time.Millisecond
}

func stateToStatus(state serviceState) ServiceStatus {
	switch state {
	case stateStarting, stateRunning:
		return StatusActive
	case stateTerminating, stateKilling, stateStopped:
		return StatusInactive
	case stateBackoff:
		return StatusBackoff
	default: // stateInitial (should never happen) and stateExited
		return StatusError
	}
}

// DefaultServiceNames returns the name of the services set to start
// by default.
func (m *ServiceManager) DefaultServiceNames() ([]string, error) {
	currentPlan := m.getPlan()
	var names []string
	for name, service := range currentPlan.Services {
		if service.Startup == plan.StartupEnabled {
			names = append(names, name)
		}
	}

	lanes, err := currentPlan.StartOrder(names)
	if err != nil {
		return nil, err
	}

	var result []string
	for _, lane := range lanes {
		result = append(result, lane...)
	}
	return result, err
}

// StartOrder returns the provided services, together with any required
// dependencies, in the proper order, put in lanes, for starting them all up.
func (m *ServiceManager) StartOrder(services []string) ([][]string, error) {
	currentPlan := m.getPlan()
	return currentPlan.StartOrder(services)
}

// StopOrder returns the provided services, together with any dependants,
// in the proper order, put in lanes, for stopping them all.
func (m *ServiceManager) StopOrder(services []string) ([][]string, error) {
	currentPlan := m.getPlan()
	return currentPlan.StopOrder(services)
}

// ServiceLogs returns iterators to the provided services. If last is negative,
// return tail iterators; if last is zero or positive, return head iterators
// going back last elements. Each iterator must be closed via the Close method.
func (m *ServiceManager) ServiceLogs(services []string, last int) (map[string]servicelog.Iterator, error) {
	requested := make(map[string]bool, len(services))
	for _, name := range services {
		requested[name] = true
	}

	m.servicesLock.Lock()
	defer m.servicesLock.Unlock()

	iterators := make(map[string]servicelog.Iterator)
	for name, service := range m.services {
		if !requested[name] {
			continue
		}
		if service == nil || service.logs == nil {
			continue
		}
		if last >= 0 {
			iterators[name] = service.logs.HeadIterator(last)
		} else {
			iterators[name] = service.logs.TailIterator()
		}
	}

	return iterators, nil
}

// Replan returns a list of services in lanes to stop and services to start
// because their plans had changed between when they started and this call.
func (m *ServiceManager) Replan() ([][]string, [][]string, error) {
	currentPlan := m.getPlan()
	m.servicesLock.Lock()
	defer m.servicesLock.Unlock()

	needsRestart := make(map[string]bool)
	var stop []string
	for name, s := range m.services {
		if config, ok := currentPlan.Services[name]; ok {
			if config.Equal(s.config) {
				continue
			}
			s.config = config.Copy() // update service config from plan
		}
		needsRestart[name] = true
		stop = append(stop, name)
	}

	var start []string
	for name, config := range currentPlan.Services {
		if needsRestart[name] || config.Startup == plan.StartupEnabled {
			start = append(start, name)
		}
	}

	stopLanes, err := currentPlan.StopOrder(stop)
	if err != nil {
		return nil, nil, err
	}
	for i, name := range stop {
		if !needsRestart[name] {
			stop = append(stop[:i], stop[i+1:]...)
		}
	}

	startLanes, err := currentPlan.StartOrder(start)
	if err != nil {
		return nil, nil, err
	}

	return stopLanes, startLanes, nil
}

func (m *ServiceManager) SendSignal(services []string, signal string) error {
	m.servicesLock.Lock()
	defer m.servicesLock.Unlock()

	var errors []string
	for _, name := range services {
		s := m.services[name]
		if s == nil {
			errors = append(errors, fmt.Sprintf("cannot send signal to %q: service is not running", name))
			continue
		}
		err := s.sendSignal(signal)
		if err != nil {
			errors = append(errors, fmt.Sprintf("cannot send signal to %q: %v", name, err))
			continue
		}
	}
	if len(errors) > 0 {
		return fmt.Errorf("%s", strings.Join(errors, "; "))
	}
	return nil
}

// CheckFailed response to a health check failure. If the given check name is
// in the on-check-failure map for a service, tell the service to perform the
// configured action (for example, "restart").
func (m *ServiceManager) CheckFailed(name string) {
	m.servicesLock.Lock()
	defer m.servicesLock.Unlock()

	for _, service := range m.services {
		for checkName, action := range service.config.OnCheckFailure {
			if checkName == name {
				service.checkFailed(action)
			}
		}
	}
}

// WriteMetrics collects and writes metrics for all services to the provided writer.
func (m *ServiceManager) WriteMetrics(writer metrics.Writer) error {
	m.servicesLock.Lock()
	defer m.servicesLock.Unlock()

	names := make([]string, 0, len(m.services))
	for name := range m.services {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		service := m.services[name]
		err := service.writeMetrics(writer)
		if err != nil {
			return err
		}
	}
	return nil
}

// servicesToStop is used during service manager shutdown to cleanly terminate
// all running services. Running services include both services in the
// stateRunning and stateBackoff, since a service in backoff state can start
// running once the timeout expires, which creates a race on service manager
// exit. If it starts just before, it would continue to run after the service
// manager is terminated. If it starts just after (before the main process
// exits), it would generate a runtime error as the reaper would already be dead.
// This function returns a slice of service names to stop, in dependency order,
// put in lanes.
func servicesToStop(m *ServiceManager) ([][]string, error) {
	currentPlan := m.getPlan()
	// Get all service names in plan.
	services := make([]string, 0, len(currentPlan.Services))
	for name := range currentPlan.Services {
		services = append(services, name)
	}

	// Order according to dependency order.
	stop, err := currentPlan.StopOrder(services)
	if err != nil {
		return nil, err
	}

	// Filter down to only those that are starting, running or in backoff
	m.servicesLock.Lock()
	defer m.servicesLock.Unlock()
	var result [][]string
	for _, services := range stop {
		var notStopped []string
		for _, name := range services {
			s := m.services[name]
			if s != nil && (s.state == stateStarting || s.state == stateRunning || s.state == stateBackoff) {
				notStopped = append(notStopped, name)
			}
		}
		if len(notStopped) > 0 {
			result = append(result, notStopped)
		}
	}
	return result, nil
}
