package servstate

import (
	"fmt"
	"io"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/canonical/pebble/internal/overlord/state"
	"github.com/canonical/pebble/internal/plan"
	"github.com/canonical/pebble/internal/servicelog"
)

type ServiceManager struct {
	state     *state.State
	runner    *state.TaskRunner
	pebbleDir string

	planLock sync.Mutex
	plan     *plan.Plan

	servicesLock sync.Mutex
	services     map[string]*serviceData

	serviceOutput io.Writer
	restarter     Restarter

	randLock sync.Mutex
	rand     *rand.Rand
}

type Restarter interface {
	HandleRestart(t state.RestartType)
}

// LabelExists is the error returned by AppendLayer when a layer with that
// label already exists.
type LabelExists struct {
	Label string
}

func (e *LabelExists) Error() string {
	return fmt.Sprintf("layer %q already exists", e.Label)
}

func NewManager(s *state.State, runner *state.TaskRunner, pebbleDir string, serviceOutput io.Writer, restarter Restarter) (*ServiceManager, error) {
	manager := &ServiceManager{
		state:         s,
		runner:        runner,
		pebbleDir:     pebbleDir,
		services:      make(map[string]*serviceData),
		serviceOutput: serviceOutput,
		restarter:     restarter,
		rand:          rand.New(rand.NewSource(time.Now().UnixNano())),
	}

	runner.AddHandler("start", manager.doStart, nil)
	runner.AddHandler("stop", manager.doStop, nil)

	return manager, nil
}

func (m *ServiceManager) reloadPlan() error {
	p, err := plan.ReadDir(m.pebbleDir)
	if err != nil {
		return err
	}
	m.plan = p
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
	err := m.updatePlan(newLayers)
	if err != nil {
		return err
	}
	layer.Order = newOrder
	return nil
}

func (m *ServiceManager) updatePlan(layers []*plan.Layer) error {
	combined, err := plan.CombineLayers(layers...)
	if err != nil {
		return err
	}
	m.plan = &plan.Plan{
		Layers:   layers,
		Services: combined.Services,
	}
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
	err = m.updatePlan(newLayers)
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
	Name    string
	Startup ServiceStartup
	Current ServiceStatus
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
	releasePlan, err := m.acquirePlan()
	if err != nil {
		return nil, err
	}
	defer releasePlan()

	m.servicesLock.Lock()
	defer m.servicesLock.Unlock()

	requested := make(map[string]bool, len(names))
	for _, name := range names {
		requested[name] = true
	}

	var services []*ServiceInfo
	matchNames := len(names) > 0
	for name, config := range m.plan.Services {
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
			switch s.state {
			case stateInitial, stateStarting, stateRunning:
				info.Current = StatusActive
			case stateTerminating, stateKilling, stateStopped:
				// Already set to inactive above, but it's nice to be explicit for each state
				info.Current = StatusInactive
			case stateBackoff:
				info.Current = StatusBackoff
			default:
				info.Current = StatusError
			}
		}
		services = append(services, info)
	}
	sort.Slice(services, func(i, j int) bool {
		return services[i].Name < services[j].Name
	})
	return services, nil
}

// DefaultServiceNames returns the name of the services set to start
// by default.
func (m *ServiceManager) DefaultServiceNames() ([]string, error) {
	releasePlan, err := m.acquirePlan()
	if err != nil {
		return nil, err
	}
	defer releasePlan()

	var names []string
	for name, service := range m.plan.Services {
		if service.Startup == plan.StartupEnabled {
			names = append(names, name)
		}
	}

	return m.plan.StartOrder(names)
}

// StartOrder returns the provided services, together with any required
// dependencies, in the proper order for starting them all up.
func (m *ServiceManager) StartOrder(services []string) ([]string, error) {
	releasePlan, err := m.acquirePlan()
	if err != nil {
		return nil, err
	}
	defer releasePlan()

	return m.plan.StartOrder(services)
}

// StopOrder returns the provided services, together with any dependants,
// in the proper order for starting them all up.
func (m *ServiceManager) StopOrder(services []string) ([]string, error) {
	releasePlan, err := m.acquirePlan()
	if err != nil {
		return nil, err
	}
	defer releasePlan()

	return m.plan.StopOrder(services)
}

// ServiceLogs returns iterators to the provided services. If last is negative,
// return tail iterators; if last is zero or positive, return head iterators
// going back last elements. Each iterator must be closed via the Close method.
func (m *ServiceManager) ServiceLogs(services []string, last int) (map[string]servicelog.Iterator, error) {
	releasePlan, err := m.acquirePlan()
	if err != nil {
		return nil, err
	}
	defer releasePlan()

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
	releasePlan, err := m.acquirePlan()
	if err != nil {
		return nil, nil, err
	}
	defer releasePlan()

	m.servicesLock.Lock()
	defer m.servicesLock.Unlock()

	needsRestart := make(map[string]bool)
	var stop []string
	for name, s := range m.services {
		if config, ok := m.plan.Services[name]; ok && config.Equal(s.config) {
			if config.Equal(s.config) {
				continue
			}
			s.config = config.Copy() // update service config from plan
		}
		needsRestart[name] = true
		stop = append(stop, name)
	}

	var start []string
	for name, config := range m.plan.Services {
		if needsRestart[name] || config.Startup == plan.StartupEnabled {
			start = append(start, name)
		}
	}

	stop, err = m.plan.StopOrder(stop)
	if err != nil {
		return nil, nil, err
	}
	for i, name := range stop {
		if !needsRestart[name] {
			stop = append(stop[:i], stop[i+1:]...)
		}
	}

	start, err = m.plan.StartOrder(start)
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
