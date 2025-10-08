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

// autoUsernameRangeLimit determines the maximum number suffix for
// users auto-allocated by this package when a new certificate is
// paired.
const autoUsernameRangeLimit uint32 = 1000

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
	// ModeDisabled means no pairing is possible
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
		return fmt.Errorf("cannot support pairing mode %q: unknown mode", c.Mode)
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
	pairingConfig *pairingConfig
	// Persisted state of the pairing manager.
	pairingDetails *pairingDetails
	// enabled is true if the pairing window is enabled.
	enabled bool
	// timer controls the duration of the pairing window.
	timer *time.Timer
	// skipHandlerOnce is used to skip the next AfterFunc handler
	// execution. This is used for cases where we want to extend
	// the pairing window, but the handler was already kicked off,
	// blocked on entry on the Mutex.
	skipHandlerOnce bool
}

func NewManager(st *state.State) (*PairingManager, error) {
	m := &PairingManager{
		state: st,
		pairingConfig: &pairingConfig{
			Mode: ModeUnset,
		},
		pairingDetails: &pairingDetails{
			Paired: false,
		},
	}

	// Load the paired state at startup.
	m.state.Lock()
	defer m.state.Unlock()
	err := m.state.Get(pairingDetailsAttr, &m.pairingDetails)
	if errors.Is(err, state.ErrNoState) {
		// Let's make sure the state always reflects the pairing state
		// explicitly.
		m.state.Set(pairingDetailsAttr, m.pairingDetails)
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
	if m.pairingConfig.Mode != newConfig.Mode {
		m.enabled = false
		m.stopTimer()
	}
	m.pairingConfig = newConfig
}

// Ensure implements overlord.StateManager interface.
func (m *PairingManager) Ensure() error {
	return nil
}

// Stop implements overlord.StateStopper interface.
func (m *PairingManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.enabled = false
	m.stopTimer()
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
	defer func() {
		m.enabled = false
		m.stopTimer()
	}()

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
			m.pairingDetails.Paired = true
			m.state.Set(pairingDetailsAttr, m.pairingDetails)

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

	m.pairingDetails.Paired = true
	m.state.Set(pairingDetailsAttr, m.pairingDetails)

	return nil
}

// PairingWindowEnabled returns whether the pairing window is currently enabled.
func (m *PairingManager) PairingWindowEnabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabled
}

// stopTimer stops the current timer if it exists
func (m *PairingManager) stopTimer() {
	if m.timer != nil {
		m.timer.Stop()
	}
}

// startTimer starts a new timer that will disable the pairing window after
// the given timeout. If the timer exists, we will reuse the timer by
// resetting it with a new duration.
func (m *PairingManager) startTimer(timeout time.Duration) {
	// If timer already exists, just reset it with the new timeout
	if m.timer != nil {
		alreadyStopped := !m.timer.Reset(timeout)
		if alreadyStopped && m.enabled {
			// We get here if we are trying to extend the pairing
			// window duration while it is still enabled. However,
			// the handler to disable it already fired, and is now
			// blocked on the mutex.
			m.skipHandlerOnce = true
		}
		return
	}

	m.timer = time.AfterFunc(timeout, m.timeoutHandler)
}

func (m *PairingManager) timeoutHandler() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// This is used in cases where we want to extend the pairing
	// window duration but this handler was already started, but
	// blocked on the mutex above.
	if m.skipHandlerOnce {
		m.skipHandlerOnce = false
		return
	}
	m.enabled = false
}

// EnablePairing requests the pairing manager to enable the pairing window.
func (m *PairingManager) EnablePairing(timeout time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If the pairing window is already enabled, reset the timeout duration
	if m.enabled {
		m.startTimer(timeout)
		return nil
	}

	// Check the pairing mode
	switch m.pairingConfig.Mode {
	case ModeDisabled, ModeUnset:
		return errors.New("cannot enable pairing with pairing mode disabled")

	case ModeSingle:
		// Single mode: check if already paired
		if m.pairingDetails.Paired {
			return errors.New("cannot enable pairing when already paired in 'single' pairing mode")

		}
	case ModeMultiple:
	default:
		return fmt.Errorf("cannot enable pairing with unknown pairing mode %q", m.pairingConfig.Mode)
	}

	// If we get here we passed all checks and we can enable the
	// pairing window for the given duration.
	m.enabled = true
	m.startTimer(timeout)
	return nil
}

// generateUniqueUsername finds the first unique username following the pattern
// "user-x" where x starts at 1 and monotonically increments. Usernames not
// following this pattern will simply not be considered.
func generateUniqueUsername(existingIdentities map[string]*state.Identity) (string, error) {
	for i := uint32(1); i <= autoUsernameRangeLimit; i++ {
		username := fmt.Sprintf("user-%d", i)

		// If the generated username doesn't already exist, we can use it.
		if _, exists := existingIdentities[username]; !exists {
			return username, nil
		}
	}
	// No free username found.
	return "", fmt.Errorf("user allocation limit '%d' reached", autoUsernameRangeLimit)
}
