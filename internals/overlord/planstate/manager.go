package planstate

import (
	"fmt"
	"sync"

	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
)

// LabelExists is the error returned by AppendLayer when a layer with that
// label already exists.
type LabelExists struct {
	Label string
}

func (e *LabelExists) Error() string {
	return fmt.Sprintf("layer %q already exists", e.Label)
}

type PlanManager struct {
	state     *state.State
	runner    *state.TaskRunner
	pebbleDir string

	planLock     sync.Mutex
	plan         *plan.Plan
	planHandlers []PlanChangeFunc
}

func NewManager(s *state.State, runner *state.TaskRunner, pebbleDir string) (*PlanManager, error) {
	manager := &PlanManager{
		state:     s,
		runner:    runner,
		pebbleDir: pebbleDir,
	}

	return manager, nil
}

// Load reads plan layers from the pebble directory, combine and validate the
// final plan, and finally notifies registered managers up the plan update.
func (m *PlanManager) Load() error {
	m.planLock.Lock()
	defer m.planLock.Unlock()
	plan, err := plan.ReadDir(m.pebbleDir)
	if err != nil {
		return err
	}
	m.planChanged(plan)
	return nil
}

// PlanChangeFunc is the type of function used by AddChangeListeners.
type PlanChangeFunc func(p *plan.Plan)

// AddChangeListeners adds f to the list of functions that are called whenever
// the plan has changed. This method may not be called once the overlord state
// engine has started.
func (m *PlanManager) AddChangeListeners(f PlanChangeFunc) {
	m.planHandlers = append(m.planHandlers, f)
}

func (m *PlanManager) planChanged(plan *plan.Plan) {
	m.plan = plan
	for _, f := range m.planHandlers {
		f(plan)
	}
}

// Plan returns the configuration plan. Any change made to the plan will
// result in a new Plan instance, so the current design assumes a returned
// plan is never mutated by planstate.
func (m *PlanManager) Plan() (*plan.Plan, error) {
	m.planLock.Lock()
	defer m.planLock.Unlock()
	return m.plan, nil
}

// LayerCreator allows an Append or Combine operation on the plan to include
// layer creation derived from the plan itself, which has to happen while
// the plan is locked.
type LayerCreator interface {
	Layer(planIn *plan.Plan) (*plan.Layer, error)
}

// AppendLayer takes a LayerCreator interface, which may be an existing plan
// Layer, and appends it to the plan's layers and updates the layer.Order
// field to the new order. If a layer with layer.Label already exists, return
// an error of type *LabelExists.
func (m *PlanManager) AppendLayer(creator LayerCreator) error {
	m.planLock.Lock()
	defer m.planLock.Unlock()

	layer, err := creator.Layer(m.plan)
	if err != nil {
		return err
	}

	index, _ := findLayer(m.plan.Layers, layer.Label)
	if index >= 0 {
		return &LabelExists{Label: layer.Label}
	}

	return m.appendLayer(layer)
}

// CombineLayer takes a LayerCreator interface, which may be an existing plan
// Layer, and appends it to an existing layer that has the same label. If no
// existing layer has the label, append a new one. In either case, update the
// layer.Order field to the new order.
func (m *PlanManager) CombineLayer(creator LayerCreator) error {
	m.planLock.Lock()
	defer m.planLock.Unlock()

	layer, err := creator.Layer(m.plan)
	if err != nil {
		return err
	}

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

func (m *PlanManager) appendLayer(layer *plan.Layer) error {
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

func (m *PlanManager) updatePlanLayers(layers []*plan.Layer) error {
	combined, err := plan.CombineLayers(layers...)
	if err != nil {
		return err
	}
	plan := &plan.Plan{
		Layers:     layers,
		Services:   combined.Services,
		Checks:     combined.Checks,
		LogTargets: combined.LogTargets,
	}
	m.planChanged(plan)
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

// Ensure implements StateManager.Ensure.
func (m *PlanManager) Ensure() error {
	return nil
}
