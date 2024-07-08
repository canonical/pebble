// Copyright (c) 2024 Canonical Ltd
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License version 3 as
// published by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package planstate

import (
	"fmt"
	"sync"

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
	pebbleDir string

	planLock sync.Mutex
	plan     *plan.Plan

	changeListeners []PlanChangedFunc
}

func NewManager(pebbleDir string) (*PlanManager, error) {
	manager := &PlanManager{
		pebbleDir: pebbleDir,
		plan:      &plan.Plan{},
	}
	return manager, nil
}

// Load reads plan layers from the pebble directory, combines and validates the
// final plan, and finally notifies registered managers of the plan update. In
// the case of a non-existent layers directory, or no layers in the layers
// directory, an empty plan is announced to change subscribers.
func (m *PlanManager) Load() error {
	plan, err := plan.ReadDir(m.pebbleDir)
	if err != nil {
		return err
	}

	m.planLock.Lock()
	m.plan = plan
	m.planLock.Unlock()

	m.callChangeListeners(plan)
	return nil
}

// PlanChangedFunc is the function type used by AddChangeListener.
type PlanChangedFunc func(p *plan.Plan)

// AddChangeListener adds f to the list of functions that are called whenever
// a plan change event took place (Load, AppendLayer, CombineLayer). A plan
// change event does not guarantee that combined plan content has changed.
// Notification registration must be completed before the plan is loaded.
func (m *PlanManager) AddChangeListener(f PlanChangedFunc) {
	m.changeListeners = append(m.changeListeners, f)
}

func (m *PlanManager) callChangeListeners(plan *plan.Plan) {
	if plan == nil {
		// Avoids if statement on every deferred call to this method (we
		// shouldn't call listeners when the operation fails).
		return
	}
	for _, f := range m.changeListeners {
		f(plan)
	}
}

// Plan returns the combined configuration plan. Any change made to the plan
// will result in a new Plan instance, so the current design assumes a returned
// plan is never mutated by planstate (and may never be mutated by any
// consumer).
func (m *PlanManager) Plan() *plan.Plan {
	m.planLock.Lock()
	defer m.planLock.Unlock()
	return m.plan
}

// AppendLayer takes a Layer, appends it to the plan's layers and updates the
// layer.Order field to the new order. If a layer with layer.Label already
// exists, return an error of type *LabelExists.
func (m *PlanManager) AppendLayer(layer *plan.Layer) error {
	var newPlan *plan.Plan
	defer func() { m.callChangeListeners(newPlan) }()

	m.planLock.Lock()
	defer m.planLock.Unlock()

	index, _ := findLayer(m.plan.Layers, layer.Label)
	if index >= 0 {
		return &LabelExists{Label: layer.Label}
	}

	newPlan, err := m.appendLayer(layer)
	return err
}

// CombineLayer takes a Layer, combines it to an existing layer that has the
// same label. If no existing layer has the label, append a new one. In either
// case, update the layer.Order field to the new order.
func (m *PlanManager) CombineLayer(layer *plan.Layer) error {
	var newPlan *plan.Plan
	defer func() { m.callChangeListeners(newPlan) }()

	m.planLock.Lock()
	defer m.planLock.Unlock()

	index, found := findLayer(m.plan.Layers, layer.Label)
	if index < 0 {
		// No layer found with this label, append new one.
		var err error
		newPlan, err = m.appendLayer(layer)
		return err
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
	newPlan, err = m.updatePlanLayers(newLayers)
	if err != nil {
		return err
	}
	layer.Order = found.Order
	return nil
}

func (m *PlanManager) appendLayer(layer *plan.Layer) (*plan.Plan, error) {
	newOrder := 1
	if len(m.plan.Layers) > 0 {
		last := m.plan.Layers[len(m.plan.Layers)-1]
		newOrder = last.Order + 1
	}

	newLayers := append(m.plan.Layers, layer)
	newPlan, err := m.updatePlanLayers(newLayers)
	if err != nil {
		return nil, err
	}
	layer.Order = newOrder
	return newPlan, nil
}

func (m *PlanManager) updatePlanLayers(layers []*plan.Layer) (*plan.Plan, error) {
	combined, err := plan.CombineLayers(layers...)
	if err != nil {
		return nil, err
	}
	p := &plan.Plan{
		Layers:     layers,
		Services:   combined.Services,
		Checks:     combined.Checks,
		LogTargets: combined.LogTargets,
	}
	err = p.Validate()
	if err != nil {
		return nil, err
	}
	m.plan = p
	return p, nil
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

// SetServiceArgs sets the service arguments provided by "pebble run --args"
// to their respective services. It adds a new layer in the plan, the layer
// consisting of services with commands having their arguments changed.
//
// NOTE: This functionality should be redesigned (moved out of the plan manager)
// as the plan manager should not be concerned with schema section details.
func (m *PlanManager) SetServiceArgs(serviceArgs map[string][]string) error {
	var newPlan *plan.Plan
	defer func() { m.callChangeListeners(newPlan) }()

	m.planLock.Lock()
	defer m.planLock.Unlock()

	newLayer := &plan.Layer{
		// Labels with "pebble-*" prefix are reserved for use by Pebble.
		// Layer.Validate() ensures this, so skip calling that because we're creating the
		// Layer directly, not parsing from user input.
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

	newPlan, err := m.appendLayer(newLayer)
	return err
}
