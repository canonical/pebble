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

// pairingstate manages client server pairing.
package pairingstate

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/plan"
)

// timeAfterFunc allows faking time.AfterFunc.
var timeAfterFunc = func(d time.Duration, f func()) Timer {
	return time.AfterFunc(d, f)
}

// Timer is used so we can supply a fake timer during testing.
type Timer interface {
	Stop() bool
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

type PairingConfig struct {
	Mode Mode `yaml:"mode,omitempty"`
}

func (c *PairingConfig) Validate() error {
	switch c.Mode {
	case ModeUnset, ModeDisabled, ModeSingle, ModeMultiple:
	default:
		return fmt.Errorf("cannot support pairing mode %q: unknown mode", c.Mode)
	}
	return nil
}

// Implements the optional Zeroer interface as used by the YAML library
// for deciding when to marshal a section or not.
func (c *PairingConfig) IsZero() bool {
	return c.Mode == ModeUnset
}

func (c *PairingConfig) Combine(other *PairingConfig) {
	if other.Mode != ModeUnset {
		c.Mode = other.Mode
	}
}

// autoUsernameRangeLimit determines the maxmimum number suffix for
// users auto-allocated by this package when a new certificate is
// paired.
const autoUsernameRangeLimit uint32 = 1000

type PairingManager struct {
	state  *state.State
	mu     sync.Mutex
	config *PairingConfig
	open   bool
	// timer controls the duration of the pairing window.
	timer Timer
	// cancelTimerFunc cancels a pairing window if enabled.
	cancelTimerFunc context.CancelFunc
}

func NewManager(state *state.State) *PairingManager {
	m := &PairingManager{
		state: state,
		config: &PairingConfig{
			Mode: ModeUnset,
		},
	}
	return m
}

// PlanChanged informs the pairing manager that the plan has been updated.
func (m *PairingManager) PlanChanged(update *plan.Plan) {
	m.mu.Lock()
	defer m.mu.Unlock()

	newConfig := update.Sections[PairingField].(*PairingConfig)

	// If the mode changed, force the pairing window to be reopened
	// taking the new config into account.
	if m.config.Mode != newConfig.Mode {
		m.open = false
		m.stopTimer()
	}
	m.config = newConfig
}

// Ensure implements overlord.StateManager interface.
func (m *PairingManager) Ensure() error {
	return nil
}

// Stop implements overlord.StateStopper interface.
func (m *PairingManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.open = false
	m.stopTimer()
}

// PairMTLS adds a client identity with admin permissions to the identity
// subsystem. A pairing request always closes the pairing window.
func (m *PairingManager) PairMTLS(clientCert *x509.Certificate) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.open {
		return errors.New("cannot pair while pairing window is closed")
	}

	// Any success or failure should always close the pairing window.
	defer func() {
		m.open = false
		m.stopTimer()
	}()

	// Verify that the client certificate is self-signed (the public
	// key included must verify the signature). We do this here as a
	// sanity check since we are about to pair this certificat and use
	// it in exactly this way for future client credential checks.
	// Note that the TLS handshake already proved that the client has
	// access to the private key by verifying the handshake transcript
	// signature.
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
			// Not valid certificate identity.
			continue
		}

		if identity.Cert.X509.Equal(clientCert) {
			return errors.New("cannot pair already paired identity")
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

	m.state.SetIsPaired()

	return nil
}

// PairingWindowOpen returns whether the pairing window is currently open.
func (m *PairingManager) PairingWindowOpen() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.open
}

// stopTimer stops the current timer if it exists
func (m *PairingManager) stopTimer() {
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	if m.cancelTimerFunc != nil {
		m.cancelTimerFunc()
		m.cancelTimerFunc = nil
	}
}

// startTimer starts a new timer that will close the pairing window after the given timeout
func (m *PairingManager) startTimer(timeout time.Duration) {
	m.stopTimer()

	ctx, cancel := context.WithCancel(context.Background())
	m.cancelTimerFunc = cancel

	m.timer = timeAfterFunc(timeout, func() {
		select {
		case <-ctx.Done():
			return
		default:
			m.mu.Lock()
			m.open = false
			m.stopTimer()
			m.mu.Unlock()
		}
	})
}

// EnablePairing requests the pairing manager to enable the pairing window.
func (m *PairingManager) EnablePairing(timeout time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If Open is already true, reset the timeout duration
	if m.open {
		m.startTimer(timeout)
		return nil
	}

	// Check the pairing mode
	switch m.config.Mode {
	case ModeDisabled, ModeUnset:
		// Mode is disabled, set Open = False
		m.open = false
		m.stopTimer()
		return errors.New("cannot enable pairing with pairing mode disabled")

	case ModeSingle:
		m.state.Lock()
		isPaired := m.state.IsPaired()
		m.state.Unlock()

		// Single mode: check if already paired
		if isPaired {
			// Already paired, set Open = False
			m.open = false
			m.stopTimer()
			return errors.New("cannot enable pairing when already paired in 'single' pairing mode")

		} else {
			// Not paired yet, set Open = True
			m.open = true
			m.startTimer(timeout)
		}
	case ModeMultiple:
		// Multiple mode: always set Open = True
		m.open = true
		m.startTimer(timeout)
	default:
		// Unknown mode, set Open = False
		m.open = false
		m.stopTimer()
		return fmt.Errorf("cannot enable pairing with unknown pairing mode %q", m.config.Mode)
	}

	return nil
}

// generateUniqueUsername finds the first unique username following the pattern
// "user-x" where x starts at 1 and monotonically increments. Users names not
// following this pattern will simply not be considered.
func generateUniqueUsername(existingIdentities map[string]*state.Identity) (string, error) {
	// Find all the usernames matching our scheme.
	var matched []uint32
	for name := range existingIdentities {
		if strings.HasPrefix(name, "user-") {
			numberStr := strings.TrimPrefix(name, "user-")
			number, err := strconv.ParseUint(numberStr, 10, 32)
			if err != nil || number == 0 {
				// Skip this entry becuase the suffix is not a supported number.
				continue
			}
			matched = append(matched, uint32(number))
		}
	}
	// Sort the numbers.
	slices.Sort(matched)
	// Find the first available number.
	userNumberFree := uint32(1)
	for _, n := range matched {
		if n > userNumberFree {
			// We found an available number.
			break
		}
		userNumberFree += 1

		// Check if we reached the user allocation limit.
		if userNumberFree > autoUsernameRangeLimit {
			return "", fmt.Errorf("user allocation limit '%d' reached", autoUsernameRangeLimit)
		}
	}

	return fmt.Sprintf("user-%d", userNumberFree), nil
}
