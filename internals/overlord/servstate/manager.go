package servstate

import (
	"fmt"
	"io"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/overlord/restart"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/reaper"
	"github.com/canonical/pebble/internals/servicelog"
)

type ServiceManager struct {
	state     *state.State
	runner    *state.TaskRunner
	pebbleDir string

	planLock     sync.Mutex
	plan         *plan.Plan
	planHandlers []PlanFunc

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

// PlanFunc is the type of function used by NotifyPlanChanged.
type PlanFunc func(p *plan.Plan)

type Restarter interface {
	HandleRestart(t restart.RestartType)
}

// LabelExists is the error returned by AppendLayer when a layer with that
// label already exists.
type LabelExists struct {
	Label string
}

func (e *LabelExists) Error() string {
	return fmt.Sprintf("layer %q already exists", e.Label)
}

func NewManager(s *state.State, runner *state.TaskRunner, pebbleDir string, serviceOutput io.Writer, restarter Restarter, logMgr LogManager) (*ServiceManager, error) {
	manager := &ServiceManager{
		state:         s,
		runner:        runner,
		pebbleDir:     pebbleDir,
		services:      make(map[string]*serviceData),
		serviceOutput: serviceOutput,
		restarter:     restarter,
		rand:          rand.New(rand.NewSource(time.Now().UnixNano())),
		logMgr:        logMgr,
	}

	err := reaper.Start()
	if err != nil {
		return nil, err
	}

	runner.AddHandler("start", manager.doStart, nil)
	runner.AddHandler("stop", manager.doStop, nil)

	return manager, nil
}

// Stop implements overlord.StateStopper and stops background functions.
func (m *ServiceManager) Stop() {
	err := reaper.Stop()
	if err != nil {
		logger.Noticef("Cannot stop child process reaper: %v", err)
	}

	// Close all the service ringbuffers
	m.servicesLock.Lock()
	defer m.servicesLock.Unlock()
	for name := range m.services {
		m.removeServiceInternal(name)
	}
}

// NotifyPlanChanged adds f to the list of functions that are called whenever
// the plan is updated.
func (m *ServiceManager) NotifyPlanChanged(f PlanFunc) {
	m.planHandlers = append(m.planHandlers, f)
}

func (m *ServiceManager) updatePlan(p *plan.Plan) {
	m.plan = p
	for _, f := range m.planHandlers {
		f(p)
	}
}

func (m *ServiceManager) reloadPlan() error {
	p, err := plan.ReadDir(m.pebbleDir)
	if err != nil {
		return err
	}
	m.updatePlan(p)
	return nil
}

// Plan returns the configuration plan.
func (m *ServiceManager) Plan() (*plan.Plan, error) {
	releasePlan, err := m.acquirePlan()
	if err != nil {
		return nil, err
	}
	defer releasePlan()
	return m.plan, nil
}

// AppendLayer appends the given layer to the plan's layers and updates the
// layer.Order field to the new order. If a layer with layer.Label already
// exists, return an error of type *LabelExists.
func (m *ServiceManager) AppendLayer(layer *plan.Layer) error {
	releasePlan, err := m.acquirePlan()
	if err != nil {
		return err
	}
	defer releasePlan()

	index, _ := findLayer(m.plan.Layers, layer.Label)
	if index >= 0 {
		return &LabelExists{Label: layer.Label}
	}

	return m.appendLayer(layer)
}

func (m *ServiceManager) appendLayer(layer *plan.Layer) error {
	newOrder := 1
	if len(m.plan.Layers) > 0 {
		last := m.plan.Layers[len(m.plan.Layers)-1]
		newOrder = last.Order + 1
	}

	newLayers := append(m.plan.Layers, layer)
	err := m.updatePlanLayers(newLayers)
	if err != nil {
		return err
	}
	layer.Order = newOrder
	return nil
}

func (m *ServiceManager) updatePlanLayers(layers []*plan.Layer) error {
	combined, err := plan.CombineLayers(layers...)
	if err != nil {
		return err
	}
	p := &plan.Plan{
		Layers:     layers,
		Services:   combined.Services,
		Checks:     combined.Checks,
		LogTargets: combined.LogTargets,
	}
	m.updatePlan(p)
	return nil
}

// findLayer returns the index (in layers) of the layer with the given label,
// or returns -1, nil if there's no layer with that label.
func findLayer(layers []*plan.Layer, label string) (int, *plan.Layer) {
	for i, layer := range layers {
		if layer.Label == label {
			return i, layer
		}
	}
	return -1, nil
}

// CombineLayer combines the given layer with an existing layer that has the
// same label. If no existing layer has the label, append a new one. In either
// case, update the layer.Order field to the new order.
func (m *ServiceManager) CombineLayer(layer *plan.Layer) error {
	releasePlan, err := m.acquirePlan()
	if err != nil {
		return err
	}
	defer releasePlan()

	index, found := findLayer(m.plan.Layers, layer.Label)
	if index < 0 {
		// No layer found with this label, append new one.
		return m.appendLayer(layer)
	}

	// Layer found with this label, combine into that one.
	combined, err := plan.CombineLayers(found, layer)
	if err != nil {
		return err
	}
	combined.Order = found.Order
	combined.Label = found.Label

	// Insert combined layer back into plan's layers list.
	newLayers := make([]*plan.Layer, len(m.plan.Layers))
	copy(newLayers, m.plan.Layers)
	newLayers[index] = combined
	err = m.updatePlanLayers(newLayers)
	if err != nil {
		return err
	}
	layer.Order = found.Order
	return nil
}

func (m *ServiceManager) acquirePlan() (release func(), err error) {
	m.planLock.Lock()
	if m.plan == nil {
		err := m.reloadPlan()
		if err != nil {
			m.planLock.Unlock()
			return nil, err
		}
	}
	released := false
	release = func() {
		if !released {
			released = true
			m.planLock.Unlock()
		}
	}
	return release, nil
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
	mgrPlan, err := m.Plan()
	if err != nil {
		return nil, err
	}

	m.servicesLock.Lock()
	defer m.servicesLock.Unlock()

	requested := make(map[string]bool, len(names))
	for _, name := range names {
		requested[name] = true
	}

	var services []*ServiceInfo
	matchNames := len(names) > 0
	for name, config := range mgrPlan.Services {
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
	mgrPlan, err := m.Plan()
	if err != nil {
		return nil, err
	}

	var names []string
	for name, service := range mgrPlan.Services {
		if service.Startup == plan.StartupEnabled {
			names = append(names, name)
		}
	}

	return mgrPlan.StartOrder(names)
}

// StartOrder returns the provided services, together with any required
// dependencies, in the proper order for starting them all up.
func (m *ServiceManager) StartOrder(services []string) ([]string, error) {
	mgrPlan, err := m.Plan()
	if err != nil {
		return nil, err
	}

	return mgrPlan.StartOrder(services)
}

// StopOrder returns the provided services, together with any dependants,
// in the proper order for stopping them all.
func (m *ServiceManager) StopOrder(services []string) ([]string, error) {
	mgrPlan, err := m.Plan()
	if err != nil {
		return nil, err
	}

	return mgrPlan.StopOrder(services)
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

// Replan returns a list of services to stop and services to start because
// their plans had changed between when they started and this call.
func (m *ServiceManager) Replan() ([]string, []string, error) {
	mgrPlan, err := m.Plan()
	if err != nil {
		return nil, nil, err
	}

	m.servicesLock.Lock()
	defer m.servicesLock.Unlock()

	needsRestart := make(map[string]bool)
	var stop []string
	for name, s := range m.services {
		if config, ok := mgrPlan.Services[name]; ok {
			if config.Equal(s.config) {
				continue
			}
			s.config = config.Copy() // update service config from plan
		}
		needsRestart[name] = true
		stop = append(stop, name)
	}

	var start []string
	for name, config := range mgrPlan.Services {
		if needsRestart[name] || config.Startup == plan.StartupEnabled {
			start = append(start, name)
		}
	}

	stop, err = mgrPlan.StopOrder(stop)
	if err != nil {
		return nil, nil, err
	}
	for i, name := range stop {
		if !needsRestart[name] {
			stop = append(stop[:i], stop[i+1:]...)
		}
	}

	start, err = mgrPlan.StartOrder(start)
	if err != nil {
		return nil, nil, err
	}

	return stop, start, nil
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

// SetServiceArgs sets the service arguments provided by "pebble run --args"
// to their respective services. It adds a new layer in the plan, the layer
// consisting of services with commands having their arguments changed.
func (m *ServiceManager) SetServiceArgs(serviceArgs map[string][]string) error {
	releasePlan, err := m.acquirePlan()
	if err != nil {
		return err
	}
	defer releasePlan()

	newLayer := &plan.Layer{
		// Labels with "pebble-*" prefix are (will be) reserved, see:
		// https://github.com/canonical/pebble/issues/220
		Label:    "pebble-service-args",
		Services: make(map[string]*plan.Service),
	}

	for name, args := range serviceArgs {
		service, ok := m.plan.Services[name]
		if !ok {
			return fmt.Errorf("service %q not found in plan", name)
		}
		base, _, err := service.ParseCommand()
		if err != nil {
			return err
		}
		newLayer.Services[name] = &plan.Service{
			Override: plan.MergeOverride,
			Command:  plan.CommandString(base, args),
		}
	}

	return m.appendLayer(newLayer)
}

// servicesToStop is used during service manager shutdown to cleanly terminate
// all running services. Running services include both services in the
// stateRunning and stateBackoff, since a service in backoff state can start
// running once the timeout expires, which creates a race on service manager
// exit. If it starts just before, it would continue to run after the service
// manager is terminated. If it starts just after (before the main process
// exits), it would generate a runtime error as the reaper would already be dead.
// This function returns a slice of service names to stop, in dependency order.
func servicesToStop(m *ServiceManager) ([]string, error) {
	mgrPlan, err := m.Plan()
	if err != nil {
		return nil, err
	}

	// Get all service names in plan.
	services := make([]string, 0, len(mgrPlan.Services))
	for name := range mgrPlan.Services {
		services = append(services, name)
	}

	// Order according to dependency order.
	stop, err := mgrPlan.StopOrder(services)
	if err != nil {
		return nil, err
	}

	// Filter down to only those that are running or in backoff
	m.servicesLock.Lock()
	defer m.servicesLock.Unlock()
	var notStopped []string
	for _, name := range stop {
		s := m.services[name]
		if s != nil && (s.state == stateRunning || s.state == stateBackoff) {
			notStopped = append(notStopped, name)
		}
	}
	return notStopped, nil
}
