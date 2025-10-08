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
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"strings"
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
	state   *state.State
	manager *pairingstate.PairingManager
}

var _ = Suite(&pairingSuite{})

func (ps *pairingSuite) SetUpTest(c *C) {
	plan.RegisterSectionExtension(pairingstate.PairingField, &pairingstate.SectionExtension{})
	ps.state = state.New(nil)
}

func (ps *pairingSuite) TearDownTest(c *C) {
	plan.UnregisterSectionExtension(pairingstate.PairingField)
}

// newManager creates a new pairing manager only after it
// persists the paired state.
func (ps *pairingSuite) newManager(c *C, s *pairingstate.PairingState) {

	if s != nil {
		ps.state.Lock()
		ps.state.Set(pairingstate.PairingStateKey, *s)
		ps.state.Unlock()
	}

	var err error
	ps.manager, err = pairingstate.NewManager(ps.state)
	c.Assert(err, IsNil)
}

// PairedState returns the persisted paired state.
func (ps *pairingSuite) PairingState() *pairingstate.PairingState {
	ps.state.Lock()
	defer ps.state.Unlock()

	var s *pairingstate.PairingState
	ps.state.Get(pairingstate.PairingStateKey, &s)
	return s
}

// updatePlan simulates a plan update with the supplied option set.
func (ps *pairingSuite) updatePlan(mode pairingstate.Mode) {
	config := &pairingstate.PairingConfig{Mode: mode}
	testPlan := plan.NewPlan()
	testPlan.Sections[pairingstate.PairingField] = config
	ps.manager.PlanChanged(testPlan)
}

// expectWindowEnableDisable makes sure that the pairing window enable phase,
// and the following transition to disable happens within reasonable bounds.
func expectWindowEnableDisable(c *C, timeout time.Duration, f func() bool) {
	// Window just opened, so should be enabled.
	c.Assert(f(), Equals, true)
	time.Sleep(timeout - time.Millisecond)
	// Window should still be open just before timeout.
	if !f() {
		c.Fatalf("pairing window disable happened before %v timeout", timeout)
	}
	// Should reset to disable within 4 milliseconds (give enough time for
	// the unit test to settle).
	deadline := time.Now().Add(4 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !f() {
			// Window should be disabled soon after timeout.
			return
		}
		time.Sleep(time.Millisecond)
	}
	c.Fatalf("pairing window did not disable within expected %v", timeout)
}

// generateTestClientCert creates a self-signed client certificate for testing.
func generateTestClientCert(c *C) *x509.Certificate {
	// Generate ed25519 key pair
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	c.Assert(err, IsNil)

	// Generate serial number
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	c.Assert(err, IsNil)

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName: "test-client",
		},
		NotBefore:             now,
		NotAfter:              now.Add(24 * time.Hour), // Valid for 24 hours
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	// Create self-signed certificate
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, privateKey.Public(), privateKey)
	c.Assert(err, IsNil)

	// Parse certificate from DER
	cert, err := x509.ParseCertificate(certDER)
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
