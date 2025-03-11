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
	"reflect"
	"slices"
	"strings"
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
	layersDir string

	planLock sync.Mutex
	plan     *plan.Plan

	changeListeners []PlanChangedFunc
}

func NewManager(layersDir string) (*PlanManager, error) {
	manager := &PlanManager{
		layersDir: layersDir,
		plan:      &plan.Plan{},
	}
	return manager, nil
}

// Load reads plan layers from the pebble directory, combines and validates the
// final plan, and finally notifies registered managers of the plan update. In
// the case of a non-existent layers directory, or no layers in the layers
// directory, an empty plan is announced to change subscribers.
func (m *PlanManager) Load() error {
	if !reflect.DeepEqual(m.plan, &plan.Plan{}) {
		// Plan already loaded
		return nil
	}

	plan, err := plan.ReadDir(m.layersDir)
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

func (m *PlanManager) SetPlan(p *plan.Plan) {
	m.planLock.Lock()
	m.plan = p
	m.planLock.Unlock()
}

// AppendLayer takes a Layer, appends it to the plan's layers and updates the
// layer.Order field to the new order. If a layer with layer.Label already
// exists, return an error of type *LabelExists. Inner must be set to true
// if the append operation may be demoted to an insert due to the layer
// configuration being located in a sub-directory.
func (m *PlanManager) AppendLayer(layer *plan.Layer, inner bool) error {
	var newPlan *plan.Plan
	defer func() { m.callChangeListeners(newPlan) }()

	m.planLock.Lock()
	defer m.planLock.Unlock()

	index, _ := findLayer(m.plan.Layers, layer.Label)
	if index >= 0 {
		return &LabelExists{Label: layer.Label}
	}

	newPlan, err := m.appendLayer(layer, inner)
	return err
}

// CombineLayer takes a Layer, combines it to an existing layer that has the
// same label. If no existing layer has the label, append a new one. In either
// case, update the layer.Order field to the new order. Inner must be set to
// true if a combine operation gets demoted to an append operation (due to the
// layer not yet existing), and if the configuration layer is located in a
// sub-directory (see AppendLayer).
func (m *PlanManager) CombineLayer(layer *plan.Layer, inner bool) error {
	var newPlan *plan.Plan
	defer func() { m.callChangeListeners(newPlan) }()

	m.planLock.Lock()
	defer m.planLock.Unlock()

	index, found := findLayer(m.plan.Layers, layer.Label)
	if index < 0 {
		// No layer found with this label, append new one.
		var err error
		newPlan, err = m.appendLayer(layer, inner)
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

// appendLayer appends (or inserts) a new layer configuration
// into the layers slice of the current plan. One important
// task of this method is to determine the new order of the layer.
//
//	| File (inside layersDir)    | Order           | Label   |
//	| -------------------------- | --------------- | ------- |
//	| 001-foo.yaml               | 001-000 => 1000 | foo     |
//	| 002-bar.d/001-aaa.yaml     | 002-001 => 2001 | bar/aaa |
//	| 002-bar.d/002-bbb.yaml     | 002-002 => 2002 | bar/bbb |
//	| 003-baz.yaml               | 003-000 => 3000 | baz     |
//
// The new incoming layer only supplies the label, which may include
// a sub-directory prefix. Normally without a sub-directory prefix,
// the new layer will always be appended, which means incrementing
// the root level order (+ 1000). If a sub-directory already exists
// in the layers slice, its order was already allocated, which means
// we can at most insert the layer as the last entry in the
// sub-directory. However, this insert is only allowed if explicitly
// requested by the user (inner=true).
func (m *PlanManager) appendLayer(newLayer *plan.Layer, inner bool) (*plan.Plan, error) {
	// The starting index and order assumes no existing layers.
	newIndex := 0
	newOrder := 1000

	// If we have existing layers, things get a little bit more complex.
	layersCount := len(m.plan.Layers)
	if layersCount > 0 {
		// We know at this point the complete label does not yet exist.
		// However, let's see if the first part of the label is a
		// sub-directory that exists and for which we already allocated
		// an order?
		newSubLabel, _, hasSub := strings.Cut(newLayer.Label, "/")
		for i := layersCount - 1; i >= 0; i-- {
			layer := m.plan.Layers[i]
			layerSubLabel, _, _ := strings.Cut(layer.Label, "/")
			// If we have a sub-directory match we know it already exists.
			// Since we searched backwards, we know the order should be the
			// next integer value.
			if layerSubLabel == newSubLabel {
				newOrder = layer.Order + 1
				newIndex = i + 1
				break
			}
		}

		// If we did not match a sub-directory, this is simply an append.
		// However, we need to know if this a inside a sub-directory or not
		// as it has an impact on how we allocate the order.
		if newIndex == 0 {
			newIndex = layersCount
			newOrder = ((m.plan.Layers[layersCount-1].Order / 1000) + 1) * 1000
			if hasSub {
				// The first file in the sub-directory gets an order of "001".
				newOrder += 1
			}
		}
	}

	// If the append operation requires an insert because the layer is added
	// inside an already existing sub-directory, with higher order items already
	// allocated beyond it, we allow it only if the request specifically
	// authorised it (inner=true).
	if newIndex != layersCount && !inner {
		return nil, fmt.Errorf("cannot insert sub-directory layer without 'inner' attribute set")
	}

	newLayers := slices.Insert(m.plan.Layers, newIndex, newLayer)
	newPlan, err := m.updatePlanLayers(newLayers)
	if err != nil {
		return nil, err
	}
	newLayer.Order = newOrder
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
		Sections:   combined.Sections,
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

	newPlan, err := m.appendLayer(newLayer, false)
	return err
}
