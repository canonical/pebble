package servstate

import (
	"os/exec"
	"sync"

	"github.com/canonical/pebble/internal/overlord/state"
	"github.com/canonical/pebble/internal/setup"
)

type ServiceManager struct {
	state     *state.State
	runner    *state.TaskRunner
	pebbleDir string

	setupLock sync.Mutex
	setup     *setup.Setup
	flattened *setup.Layer
	services  map[string]*activeService
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

func (m *ServiceManager) reloadSetup() error {
	setup, err := setup.ReadDir(m.pebbleDir)
	if err != nil {
		return err
	}
	flattened, err := setup.Flatten()
	if err != nil {
		return err
	}
	m.setup = setup
	m.flattened = flattened
	return nil
}

func (m *ServiceManager) FlattenedSetup() ([]byte, error) {
	releaseSetup, err := m.acquireSetup()
	if err != nil {
		return nil, err
	}
	defer releaseSetup()

	return m.flattened.AsYAML()
}

func (m *ServiceManager) AddSetupLayer(layerYAML []byte) (int, error) {
	releaseSetup, err := m.acquireSetup()
	if err != nil {
		return 0, err
	}
	defer releaseSetup()

	maxOrder := 0
	for _, layer := range m.setup.Layers {
		if layer.Order > maxOrder {
			maxOrder = layer.Order
		}
	}

	layer, err := setup.ParseLayer(maxOrder+1, "layer", layerYAML)
	if err != nil {
		return 0, err
	}

	newSetup := &setup.Setup{Layers: append(m.setup.Layers, layer)}
	flattened, err := newSetup.Flatten()
	if err != nil {
		return 0, err
	}
	m.setup = newSetup
	m.flattened = flattened
	return layer.Order, nil
}

func (m *ServiceManager) acquireSetup() (release func(), err error) {
	m.setupLock.Lock()
	if m.setup == nil {
		err := m.reloadSetup()
		if err != nil {
			m.setupLock.Unlock()
			return nil, err
		}
	}
	released := false
	release = func() {
		if !released {
			released = true
			m.setupLock.Unlock()
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
	for name, service := range m.flattened.Services {
		if service.Default == setup.StartAction {
			names = append(names, name)
		}
	}
	return names
}

// DefaultServices returns the name of the services set to start
// by default.
func (m *ServiceManager) DefaultServices() ([]string, error) {
	releaseSetup, err := m.acquireSetup()
	if err != nil {
		return nil, err
	}
	defer releaseSetup()

	var names []string
	for name, service := range m.flattened.Services {
		if service.Default == setup.StartAction {
			names = append(names, name)
		}
	}

	return m.flattened.StartOrder(names)
}

// StartOrder returns the provided services, together with any required
// dependencies, in the proper order for starting them all up.
func (m *ServiceManager) StartOrder(services []string) ([]string, error) {
	releaseSetup, err := m.acquireSetup()
	if err != nil {
		return nil, err
	}
	defer releaseSetup()

	return m.flattened.StartOrder(services)
}

// StartOrder returns the provided services, together with any required
// dependencies, in the proper order for starting them all up.
func (m *ServiceManager) StopOrder(services []string) ([]string, error) {
	releaseSetup, err := m.acquireSetup()
	if err != nil {
		return nil, err
	}
	defer releaseSetup()

	return m.flattened.StopOrder(services)
}

// Override changes the current override service layer which sits atop the
// layers loaded from storage. No services will be started by default (see AutoStart),
// but any services present in the previous override layer and not present in the
// new layer will be stopped for consistency.
func (m *ServiceManager) Override(layer *setup.Layer) error {
	panic("unsupported")
}
