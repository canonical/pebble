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

// CombineLayer combines the given layer YAML into the dynamic layers,
// returning the new layer's "order" (which won't increase if adding another
// dynamic layer).
func (m *ServiceManager) CombineLayer(layerYAML []byte) (int, error) {
	layer, err := plan.ParseLayer(0, "", layerYAML)
	if err != nil {
		return 0, err
	}

	releasePlan, err := m.acquirePlan()
	if err != nil {
		return 0, err
	}
	defer releasePlan()

	var last *plan.Layer
	layers := m.plan.Layers
	if len(layers) > 0 {
		last = layers[len(layers)-1]
	}

	var newOrder int
	var newCombined *plan.Layer
	if last != nil && last.IsDynamic() {
		// Last layer is dynamic, combine new layer into existing dynamic layer
		combined, err := plan.CombineLayers(last, layer)
		if err != nil {
			return 0, err
		}
		newOrder = last.Order
		newLayer := &plan.Layer{
			Order:       last.Order,
			Summary:     combined.Summary,
			Description: combined.Description,
			Services:    combined.Services,
		}
		layers = append(layers[:len(layers)-1], newLayer)
		newCombined, err = plan.CombineLayers(layers...)
		if err != nil {
			return 0, err
		}
	} else {
		// Last layer is not dynamic (or no layers), add new dynamic layer
		layer.Order = 1
		if last != nil {
			layer.Order = last.Order + 1
		}
		newOrder = layer.Order
		layers = append(layers, layer)
		newCombined, err = plan.CombineLayers(layers...)
		if err != nil {
			return 0, err
		}
	}

	m.plan = &plan.Plan{
		Layers:   layers,
		Services: newCombined.Services,
	}
	return newOrder, nil
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
