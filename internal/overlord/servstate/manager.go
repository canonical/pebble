package servstate

import (
	"os/exec"
	"sync"

	"github.com/canonical/pebble/internal/overlord/state"
	"github.com/canonical/pebble/internal/plan"
)

type ServiceManager struct {
	state     *state.State
	runner    *state.TaskRunner
	pebbleDir string

	planLock sync.Mutex
	plan     *plan.Plan
	services map[string]*activeService
}

type activeService struct {
	cmd  *exec.Cmd
	err  error
	done chan struct{}
}

func NewManager(s *state.State, runner *state.TaskRunner, pebbleDir string) (*ServiceManager, error) {
	manager := &ServiceManager{
		state:     s,
		runner:    runner,
		pebbleDir: pebbleDir,
		services:  make(map[string]*activeService),
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

// AppendLayer appends the given layer to the plan's layers and updates
// the layer.Order field to the new order.
func (m *ServiceManager) AppendLayer(layer *plan.Layer) error {
	releasePlan, err := m.acquirePlan()
	if err != nil {
		return err
	}
	defer releasePlan()

	return m.appendLayer(layer)
}

func (m *ServiceManager) appendLayer(layer *plan.Layer) error {
	newOrder := 1
	if len(m.plan.Layers) > 0 {
		last := m.plan.Layers[len(m.plan.Layers)-1]
		newOrder = last.Order + 1
	}

	newLayers := append(m.plan.Layers, layer)
	err := m.updateLayers(newLayers)
	if err != nil {
		return err
	}
	layer.Order = newOrder
	return nil
}

func (m *ServiceManager) updateLayers(layers []*plan.Layer) error {
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

// CombineLayer combines the given layer with an existing layer that has the
// same label. If no existing layer has the label, append a new one. In either
// case, update the layer.Order field to the new order.
func (m *ServiceManager) CombineLayer(layer *plan.Layer) error {
	releasePlan, err := m.acquirePlan()
	if err != nil {
		return err
	}
	defer releasePlan()

	index := -1
	for i, found := range m.plan.Layers {
		if found.Label == layer.Label {
			index = i
			break
		}
	}
	if index < 0 {
		// No layer found with this label, append new one.
		return m.appendLayer(layer)
	}
	found := m.plan.Layers[index]

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
	err = m.updateLayers(newLayers)
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

// ActiveServices returns the name of the services which are currently
// set to run. They may be running or not depending on the state of their
// process lifecycle.
func (m *ServiceManager) ActiveServices() []string {
	var names []string
	for name, service := range m.plan.Services {
		if service.Default == plan.StartAction {
			names = append(names, name)
		}
	}
	return names
}

// DefaultServices returns the name of the services set to start
// by default.
func (m *ServiceManager) DefaultServices() ([]string, error) {
	releasePlan, err := m.acquirePlan()
	if err != nil {
		return nil, err
	}
	defer releasePlan()

	var names []string
	for name, service := range m.plan.Services {
		if service.Default == plan.StartAction {
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

// StartOrder returns the provided services, together with any required
// dependencies, in the proper order for starting them all up.
func (m *ServiceManager) StopOrder(services []string) ([]string, error) {
	releasePlan, err := m.acquirePlan()
	if err != nil {
		return nil, err
	}
	defer releasePlan()

	return m.plan.StopOrder(services)
}
