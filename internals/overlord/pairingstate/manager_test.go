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

package pairingstate_test

import (
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/pairingstate"
	"github.com/canonical/pebble/internals/overlord/state"
)

// TestEnablePairingDisabledMode tests trying to open a pairing window while
// the configuration says it is disabled (before the plan update was received).
func (ps *pairingSuite) TestEnablePairingDisabledMode(c *C) {

	err := ps.manager.EnablePairing(5 * time.Second)
	c.Assert(err, ErrorMatches, "*. pairing disabled")
	c.Assert(ps.manager.PairingWindowOpen(), Equals, false)
	c.Assert(ps.fakeTimers.TimerCount(), Equals, 0)
}

// TestEnablePairingSingleModeNotPaired checks that we can pair when using single
// pairing mode, if we have never paired before.
func (ps *pairingSuite) TestEnablePairingSingleModeNotPaired(c *C) {

	ps.setupPlan(pairingstate.ModeSingle)

	timeout := 10 * time.Second
	err := ps.manager.EnablePairing(timeout)
	c.Assert(err, IsNil)
	c.Assert(ps.manager.PairingWindowOpen(), Equals, true)
	c.Assert(ps.fakeTimers.TimerCount(), Equals, 1)
	c.Assert(ps.fakeTimers.GetDuration(0), Equals, timeout)

	// Trigger the timer expiry.
	ps.fakeTimers.TriggerTimer(0)
	c.Assert(ps.manager.PairingWindowOpen(), Equals, false)
}

// TestEnablePairingSingleModeAlreadyPaired verifies that we wil fail to pair in
// single pairing mode if already paired.
func (ps *pairingSuite) TestEnablePairingSingleModeAlreadyPaired(c *C) {
	ps.state.Lock()
	ps.state.SetIsPaired()
	ps.state.Unlock()

	ps.setupPlan(pairingstate.ModeSingle)

	err := ps.manager.EnablePairing(5 * time.Second)
	c.Assert(err, ErrorMatches, ".* already paired")
	c.Assert(ps.manager.PairingWindowOpen(), Equals, false)
	c.Assert(ps.fakeTimers.TimerCount(), Equals, 0)
}

// TestEnablePairingMultipleMode verifies we can pair when pairing mode
// is set to multiple.
func (ps *pairingSuite) TestEnablePairingMultipleMode(c *C) {

	ps.setupPlan(pairingstate.ModeMultiple)

	timeout := 15 * time.Second
	err := ps.manager.EnablePairing(timeout)
	c.Assert(err, IsNil)
	c.Assert(ps.manager.PairingWindowOpen(), Equals, true)
	c.Assert(ps.fakeTimers.TimerCount(), Equals, 1)
	c.Assert(ps.fakeTimers.GetDuration(0), Equals, timeout)

	// Trigger the timer expiry.
	ps.fakeTimers.TriggerTimer(0)
	c.Assert(ps.manager.PairingWindowOpen(), Equals, false)
}

// TestEnablePairingMultipleModeAlreadyPaired verifies we can pair again when
// pairing mode is set to multiple.
func (ps *pairingSuite) TestEnablePairingMultipleModeAlreadyPaired(c *C) {
	ps.state.Lock()
	ps.state.SetIsPaired()
	ps.state.Unlock()

	ps.setupPlan(pairingstate.ModeMultiple)

	timeout := 20 * time.Second
	err := ps.manager.EnablePairing(timeout)
	c.Assert(err, IsNil)
	c.Assert(ps.manager.PairingWindowOpen(), Equals, true)
	c.Assert(ps.fakeTimers.TimerCount(), Equals, 1)
	c.Assert(ps.fakeTimers.GetDuration(0), Equals, timeout)

	// Trigger the timer expiry.
	ps.fakeTimers.TriggerTimer(0)
	c.Assert(ps.manager.PairingWindowOpen(), Equals, false)
}

// TestEnablePairingResetTimeout verifies that when pairing is re-enabled while
// the window is still open, the expiry period is reset with the new duration.
func (ps *pairingSuite) TestEnablePairingResetTimeout(c *C) {

	ps.setupPlan(pairingstate.ModeSingle)

	timeout1 := 10 * time.Second
	err := ps.manager.EnablePairing(timeout1)
	c.Assert(err, IsNil)
	c.Assert(ps.manager.PairingWindowOpen(), Equals, true)
	c.Assert(ps.fakeTimers.TimerCount(), Equals, 1)
	c.Assert(ps.fakeTimers.GetDuration(0), Equals, timeout1)

	timeout2 := 20 * time.Second
	err = ps.manager.EnablePairing(timeout2)
	c.Assert(err, IsNil)
	c.Assert(ps.manager.PairingWindowOpen(), Equals, true)
	c.Assert(ps.fakeTimers.TimerCount(), Equals, 2)
	c.Assert(ps.fakeTimers.GetDuration(1), Equals, timeout2)

	// Trigger the timer expiry.
	ps.fakeTimers.TriggerTimer(1)
	c.Assert(ps.manager.PairingWindowOpen(), Equals, false)
}

// TestEnablePairingUnknownMode verifies that an invalid mode from the plan
// is reported correctly.
func (ps *pairingSuite) TestEnablePairingUnknownMode(c *C) {

	ps.setupPlan(pairingstate.Mode("foo"))

	err := ps.manager.EnablePairing(5 * time.Second)
	c.Assert(err, ErrorMatches, ".* unknown pairing mode .*")
	c.Assert(ps.manager.PairingWindowOpen(), Equals, false)
	c.Assert(ps.fakeTimers.TimerCount(), Equals, 0)
}

// Test certificates for PairMTLS tests
const testPEMCert1 = `-----BEGIN CERTIFICATE-----
MIIBRDCB96ADAgECAhROTkdEcgeil5/5NUNTq1ZRPDLiPTAFBgMrZXAwGDEWMBQG
A1UEAwwNY2Fub25pY2FsLmNvbTAeFw0yNTA5MDgxNTI2NTJaFw0zNTA5MDYxNTI2
NTJaMBgxFjAUBgNVBAMMDWNhbm9uaWNhbC5jb20wKjAFBgMrZXADIQDtxRqb9EMe
ffcoJ0jNn9ys8uDFeHnQ6JRxgNFvomDTHqNTMFEwHQYDVR0OBBYEFI/oHjhG1A7F
3HM7McXP7w7CxtrwMB8GA1UdIwQYMBaAFI/oHjhG1A7F3HM7McXP7w7CxtrwMA8G
A1UdEwEB/wQFMAMBAf8wBQYDK2VwA0EA40v4eckaV7RBXyRb0sfcCcgCAGYtiCSD
jwXVTUH4HLpbhK0RAaEPOL4h5jm36CrWTkxzpbdCrIu4NgPLQKJ6Cw==
-----END CERTIFICATE-----`

const testInvalidPEM = `-----BEGIN INVALID-----
This is not a valid PEM certificate
-----END INVALID-----`

const testInvalidCert = `-----BEGIN CERTIFICATE-----
TWFMZm9ybWVkQkFTRTY0RW5jb2RlZENlcnRpZmljYXRlVGhhdFdpbGxGYWlsVG9Q
YXJzZUJ1dFdpbGxOb3RCZVRyZWF0ZWRBc0ludmFsaWRQRU0=
-----END CERTIFICATE-----`

// TestPairMTLSSuccess verifies that a successful pairing request closes the
// pairing window and updates identities correctly.
func (ps *pairingSuite) TestPairMTLSSuccess(c *C) {

	ps.setupPlan(pairingstate.ModeSingle)

	err := ps.manager.EnablePairing(10 * time.Second)
	c.Assert(err, IsNil)
	c.Assert(ps.manager.PairingWindowOpen(), Equals, true)

	err = ps.manager.PairMTLS(testPEMCert1)
	c.Assert(err, IsNil)

	c.Assert(ps.manager.PairingWindowOpen(), Equals, false)

	ps.state.Lock()
	isPaired := ps.state.IsPaired()
	identities := ps.state.Identities()
	ps.state.Unlock()

	c.Assert(isPaired, Equals, true)
	c.Assert(len(identities), Equals, 1)

	identity, exists := identities["user-1"]
	c.Assert(exists, Equals, true)
	c.Assert(identity.Access, Equals, state.AdminAccess)
	c.Assert(identity.Cert, NotNil)
	c.Assert(identity.Cert.X509, NotNil)

	expectedCert := parseCert(c, testPEMCert1)
	c.Assert(identity.Cert.X509.Equal(expectedCert), Equals, true)
}

// TestPairMTLSNotOpen verifies pairing is rejected if the pairing window
// is not open.
func (ps *pairingSuite) TestPairMTLSNotOpen(c *C) {

	c.Assert(ps.manager.PairingWindowOpen(), Equals, false)

	err := ps.manager.PairMTLS(testPEMCert1)
	c.Assert(err, ErrorMatches, ".* pairing is not open")

	ps.state.Lock()
	isPaired := ps.state.IsPaired()
	identities := ps.state.Identities()
	ps.state.Unlock()

	c.Assert(isPaired, Equals, false)
	c.Assert(len(identities), Equals, 0)
}

// TestPairMTLSInvalidPEM check that the pairing request fails if the supplied
// PEM certificate is invalid.
func (ps *pairingSuite) TestPairMTLSInvalidPEM(c *C) {

	ps.setupPlan(pairingstate.ModeSingle)

	err := ps.manager.EnablePairing(10 * time.Second)
	c.Assert(err, IsNil)
	c.Assert(ps.manager.PairingWindowOpen(), Equals, true)

	err = ps.manager.PairMTLS(testInvalidPEM)
	c.Assert(err, ErrorMatches, ".* invalid PEM certificate")

	c.Assert(ps.manager.PairingWindowOpen(), Equals, true)

	ps.state.Lock()
	isPaired := ps.state.IsPaired()
	identities := ps.state.Identities()
	ps.state.Unlock()

	c.Assert(isPaired, Equals, false)
	c.Assert(len(identities), Equals, 0)
}

// TestPairMTLSInvalidCertificate checks that pairing fails if the supplied
// X509 certificate embedded in PEM is invalid.
func (ps *pairingSuite) TestPairMTLSInvalidCertificate(c *C) {

	ps.setupPlan(pairingstate.ModeSingle)

	err := ps.manager.EnablePairing(10 * time.Second)
	c.Assert(err, IsNil)
	c.Assert(ps.manager.PairingWindowOpen(), Equals, true)

	err = ps.manager.PairMTLS(testInvalidCert)
	c.Assert(err, ErrorMatches, "cannot parse certificate: .*")

	c.Assert(ps.manager.PairingWindowOpen(), Equals, true)

	ps.state.Lock()
	isPaired := ps.state.IsPaired()
	identities := ps.state.Identities()
	ps.state.Unlock()

	c.Assert(isPaired, Equals, false)
	c.Assert(len(identities), Equals, 0)
}

// TestPairMTLSDuplicateCertificate verifies that dupliciate identities will result
// in the pairing request failing.
func (ps *pairingSuite) TestPairMTLSDuplicateCertificate(c *C) {

	ps.setupPlan(pairingstate.ModeMultiple)

	err := ps.manager.EnablePairing(10 * time.Second)
	c.Assert(err, IsNil)

	err = ps.manager.PairMTLS(testPEMCert1)
	c.Assert(err, IsNil)

	err = ps.manager.EnablePairing(10 * time.Second)
	c.Assert(err, IsNil)

	err = ps.manager.PairMTLS(testPEMCert1)
	c.Assert(err, ErrorMatches, ".* identity already paired")

	ps.state.Lock()
	identities := ps.state.Identities()
	ps.state.Unlock()

	c.Assert(len(identities), Equals, 1)
}

// TestPairMTLSUsernameIncrementing verifies name allocation.
func (ps *pairingSuite) TestPairMTLSUsernameIncrementing(c *C) {
	ps.state.Lock()
	ps.state.AddIdentities(map[string]*state.Identity{
		"user-3": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1000},
		},
		"user-1": {
			Access: state.ReadAccess,
			Local:  &state.LocalIdentity{UserID: 1001},
		},
		"other-user": {
			Access: state.AdminAccess,
			Local:  &state.LocalIdentity{UserID: 1002},
		},
	})
	ps.state.Unlock()

	ps.setupPlan(pairingstate.ModeMultiple)

	err := ps.manager.EnablePairing(10 * time.Second)
	c.Assert(err, IsNil)

	err = ps.manager.PairMTLS(testPEMCert1)
	c.Assert(err, IsNil)

	ps.state.Lock()
	identities := ps.state.Identities()
	ps.state.Unlock()

	_, exists := identities["user-4"]
	c.Assert(exists, Equals, true)
}
