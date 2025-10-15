// Copyright (c) 2025 Canonical Ltd
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

// pairingstate manages client-server pairing.
package pairingstate

import (
	"crypto/x509"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
)

// maxUsernameSuffix determines the maximum number suffix for users
// auto-allocated by this package when a new certificate is paired.
const maxUsernameSuffix = 1000

// pairingDetailsAttr is the key to the pairing state.
const pairingDetailsAttr = "pairing-details"

type pairingDetails struct {
	// If the paired state is true, at least one client successfully paired
	// with the server. The paired state is significant for "single"
	// pairing mode because once the first client paired with the server
	// (and paired state is set to true), no further pairing is allowed
	// from that point in time (until the state is cleared).
	Paired bool `json:"paired"`
}

// Mode controls the pairing policy of the pairing manager.
type Mode string

const (
	// ModeUnset is the same as ModeDisabled, but this value prevents the
	// plan from marshalling the Mode explicitly.
	ModeUnset Mode = ""
	// ModeDisabled means no pairing is possible.
	ModeDisabled Mode = "disabled"
	// ModeSingle means only a single client can pair.
	ModeSingle Mode = "single"
	// ModeMultiple means multiple clients can pair.
	ModeMultiple Mode = "multiple"
)

var _ plan.Section = (*pairingConfig)(nil)

// pairingConfig contains the options exposed in the plan extension.
type pairingConfig struct {
	Mode Mode `yaml:"mode,omitempty"`
}

func (c *pairingConfig) Validate() error {
	switch c.Mode {
	case ModeUnset, ModeDisabled, ModeSingle, ModeMultiple:
	default:
		return fmt.Errorf("invalid pairing mode %q: should be %q, %q or %q",
			c.Mode, ModeDisabled, ModeSingle, ModeMultiple)
	}
	return nil
}

// Implements the optional Zeroer interface as used by the YAML library
// for deciding when to marshal a section or not.
func (c *pairingConfig) IsZero() bool {
	return c.Mode == ModeUnset
}

func (c *pairingConfig) Combine(other *pairingConfig) {
	if other.Mode != ModeUnset {
		c.Mode = other.Mode
	}
}

type PairingManager struct {
	state *state.State
	mu    sync.Mutex
	// Plan config of the pairing manager.
	config *pairingConfig
	// Persisted state of the pairing manager.
	details *pairingDetails
	// enabled indicates whether the pairing window is currently enabled.
	enabled bool
	// expiry is the time when the pairing window expires, while enabled
	// is set to true. While enabled is false, the expiry time is ignored.
	expiry time.Time
}

func NewManager(st *state.State) (*PairingManager, error) {
	m := &PairingManager{
		state: st,
		config: &pairingConfig{
			Mode: ModeUnset,
		},
		details: &pairingDetails{
			Paired: false,
		},
		enabled: false,
	}

	// Load the paired state at startup.
	m.state.Lock()
	defer m.state.Unlock()
	err := m.state.Get(pairingDetailsAttr, &m.details)
	if errors.Is(err, state.ErrNoState) {
		// Let's make sure the state always reflects the pairing state
		// explicitly.
		m.state.Set(pairingDetailsAttr, m.details)
		err = nil
	}
	if err != nil {
		return nil, err
	}

	return m, nil
}

// PlanChanged informs the pairing manager that the plan has been updated.
func (m *PairingManager) PlanChanged(update *plan.Plan) {
	m.mu.Lock()
	defer m.mu.Unlock()

	newConfig := update.Sections[PairingField].(*pairingConfig)

	// If the mode changed, make sure the pairing window is disabled.
	if m.config.Mode != newConfig.Mode {
		m.enabled = false
	}
	m.config = newConfig
}

// Ensure implements overlord.StateManager interface.
func (m *PairingManager) Ensure() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.enabled {
		return nil
	}

	now := time.Now()
	if now.Before(m.expiry) {
		// The ensure call happened sooner, before the expiry time. We
		// have to schedule another ensure call to try reach the
		// expiry, so we can disable the pairing window.
		m.state.EnsureBefore(m.expiry.Sub(now))
	} else {
		// Expiry time has passed, disable the pairing window
		m.enabled = false
	}

	return nil
}

// PairingEnabled returns whether pairing is currently enabled.
func (m *PairingManager) PairingEnabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.enabled
}

// EnablePairing requests the pairing manager to enable the pairing window.
func (m *PairingManager) EnablePairing(timeout time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check the pairing mode
	switch m.config.Mode {
	case ModeDisabled, ModeUnset:
		return errors.New("cannot enable pairing with pairing mode disabled")

	case ModeSingle:
		// Single mode: check if already paired
		if m.details.Paired {
			return errors.New("cannot enable pairing when already paired in 'single' pairing mode")
		}
	case ModeMultiple:
	default:
		return fmt.Errorf("cannot enable pairing with unknown pairing mode %q", m.config.Mode)
	}

	// If we get here we passed all checks and we can enable the
	// pairing window for the given duration.
	m.expiry = time.Now().Add(timeout)
	m.enabled = true
	m.state.EnsureBefore(timeout)

	return nil
}

// PairMTLS adds a client identity with admin permissions to the identity
// subsystem. A pairing request always leaves the pairing window disabled.
func (m *PairingManager) PairMTLS(clientCert *x509.Certificate) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.enabled {
		return errors.New("cannot pair while pairing window is disabled")
	}

	// Any success or failure should always disable the pairing window.
	defer func() { m.enabled = false }()

	// Verify that the client certificate is self-signed (the public
	// key included must verify the signature). We do this here as a
	// sanity check since we are about to persist this certificate and use
	// it in exactly this way for future client credential checks. Note
	// that the TLS handshake already proved that the client has access
	// to the private key by verifying the handshake transcript signature
	// using the public key in this certificate.
	roots := x509.NewCertPool()
	roots.AddCert(clientCert)
	opts := x509.VerifyOptions{
		Roots: roots,
		// We only support verifying client TLS certificates.
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	_, err := clientCert.Verify(opts)
	if err != nil {
		return fmt.Errorf("cannot verify client certificate signature: %w", err)
	}

	m.state.Lock()
	defer m.state.Unlock()

	existingIdentities := m.state.Identities()

	for _, identity := range existingIdentities {
		if identity.Cert == nil || identity.Cert.X509 == nil {
			// Not a valid certificate identity.
			continue
		}

		if identity.Cert.X509.Equal(clientCert) {
			// This identity is already added so in this special
			// case we complete the pairing request without adding
			// it again with a new username.
			m.details.Paired = true
			m.state.Set(pairingDetailsAttr, m.details)

			return nil
		}
	}

	username, err := generateUniqueUsername(existingIdentities)
	if err != nil {
		return fmt.Errorf("cannot create new identity username: %w", err)
	}

	newIdentity := &state.Identity{
		Access: state.AdminAccess,
		Cert:   &state.CertIdentity{X509: clientCert},
	}

	err = m.state.AddIdentities(map[string]*state.Identity{
		username: newIdentity,
	})
	if err != nil {
		return fmt.Errorf("cannot add identity: %w", err)
	}

	m.details.Paired = true
	m.state.Set(pairingDetailsAttr, m.details)

	return nil
}

// generateUniqueUsername finds the first unique username following the pattern
// "user-x" where x starts at 1 and monotonically increments. Usernames not
// following this pattern will simply not be considered.
func generateUniqueUsername(existingIdentities map[string]*state.Identity) (string, error) {
	for i := 1; i <= maxUsernameSuffix; i++ {
		username := fmt.Sprintf("user-%d", i)

		if _, exists := existingIdentities[username]; !exists {
			return username, nil
		}
	}
	return "", fmt.Errorf("user allocation limit %d reached", maxUsernameSuffix)
}
