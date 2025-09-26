// Copyright (C) 2025 Canonical Ltd
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

package pairingstate_test

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v3"

	"github.com/canonical/pebble/internals/overlord/pairingstate"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
)

// Hook up check.v1 into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type pairingSuite struct {
	fakeTimers *FakeTimers
	restore    func()
	state      *state.State
	manager    *pairingstate.PairingManager
}

var _ = Suite(&pairingSuite{})

func (ps *pairingSuite) SetUpTest(c *C) {
	plan.RegisterSectionExtension(pairingstate.PairingField, &pairingstate.SectionExtension{})
	ps.fakeTimers = NewFakeTimers()
	ps.restore = pairingstate.FakeAfterFunc(ps.fakeTimers.AfterFunc)
	ps.state = state.New(nil)
	ps.manager = pairingstate.NewManager(ps.state)
}

func (ps *pairingSuite) TearDownTest(c *C) {
	if ps.restore != nil {
		ps.restore()
	}
	plan.UnregisterSectionExtension(pairingstate.PairingField)
}

// updatePlan simulates a plan update with the supplied option set.
func (ps *pairingSuite) updatePlan(mode pairingstate.Mode) {
	config := &pairingstate.PairingConfig{Mode: mode}
	testPlan := plan.NewPlan()
	testPlan.Sections[pairingstate.PairingField] = config
	ps.manager.PlanChanged(testPlan)
}

// parseCert converts a PEM certificate to X509.
func parseCert(c *C, pemData string) *x509.Certificate {
	block, _ := pem.Decode([]byte(pemData))
	c.Assert(block, NotNil)
	cert, err := x509.ParseCertificate(block.Bytes)
	c.Assert(err, IsNil)
	return cert
}

// parseCombineLayers combines layers into a final plan, allowing us to confirm
// the section extension works.
func parseCombineLayers(yamls []string) (*plan.Layer, error) {
	var layers []*plan.Layer
	for i, yaml := range yamls {
		layer, err := plan.ParseLayer(i, fmt.Sprintf("test-plan-layer-%v", i), []byte(yaml))
		if err != nil {
			return nil, err
		}
		layers = append(layers, layer)
	}
	return plan.CombineLayers(layers...)
}

// layerYAML presents a plan as a marshalled YAML string.
func layerYAML(c *C, layer *plan.Layer) string {
	yml, err := yaml.Marshal(layer)
	c.Assert(err, IsNil)
	return strings.TrimSpace(string(yml))
}

type fakeTimer struct {
	stopped bool
}

func (ft *fakeTimer) Stop() bool {
	if ft.stopped {
		return false
	}
	ft.stopped = true
	return true
}

// FakeTimers allows us to test code that uses time.AfterFunc. Instead of
// writing unit test code with delays, this object allows is to manually
// trigger the events of interest without delay.
type FakeTimers struct {
	mu        sync.Mutex
	callbacks []func()
	durations []time.Duration
	timers    []*fakeTimer
}

func NewFakeTimers() *FakeTimers {
	return &FakeTimers{}
}

func (f *FakeTimers) AfterFunc(d time.Duration, callback func()) pairingstate.Timer {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.callbacks = append(f.callbacks, callback)
	f.durations = append(f.durations, d)

	// Create a fake timer that implements the Timer interface
	fakeTimer := &fakeTimer{}
	f.timers = append(f.timers, fakeTimer)

	return fakeTimer
}

// TriggerTimer expires a selected timer, resulting in the AfterFunc callback
// getting called.
func (f *FakeTimers) TriggerTimer(index int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if index < len(f.callbacks) && index < len(f.timers) {
		// Only trigger if the timer hasn't been stopped
		if !f.timers[index].stopped {
			f.callbacks[index]()
		}
	}
}

func (f *FakeTimers) GetDuration(index int) time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()

	if index < len(f.durations) {
		return f.durations[index]
	}
	return 0
}

// TimerCount returns the number of timer instances that was created during the
// test.
func (f *FakeTimers) TimerCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.callbacks)
}
