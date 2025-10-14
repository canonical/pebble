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
	"fmt"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/pairingstate"
	"github.com/canonical/pebble/internals/overlord/state"
)

// testWindowDuration is a carefully selected pairing window duration that is
// long enough to make the test robust on busy test runners.
const testWindowDuration = 100 * time.Millisecond

// TestEnablePairingAfterStop checks that a late enable will fail as we expect.
func (ps *pairingSuite) TestEnablePairingAfterStop(c *C) {
	ps.newManager(c, nil)

	ps.updatePlan(pairingstate.ModeSingle)
	ps.manager.Stop()

	err := ps.manager.EnablePairing(testWindowDuration)
	c.Assert(err, ErrorMatches, "*: manager is shutting down")
	c.Assert(ps.manager.PairingEnabled(), Equals, false)
}

// TestPairingEnabledAfterStop checks that Stop() will result in the pairing
// window getting disabled.
func (ps *pairingSuite) TestPairingEnabledAfterStop(c *C) {
	ps.newManager(c, nil)

	ps.updatePlan(pairingstate.ModeSingle)
	err := ps.manager.EnablePairing(testWindowDuration)
	c.Assert(err, IsNil)

	ps.manager.Stop()

	c.Assert(ps.manager.PairingEnabled(), Equals, false)
}

// TestEnablePairingDisabledMode tests trying to open a pairing window while
// the configuration says it is disabled (before the plan update was received).
func (ps *pairingSuite) TestEnablePairingDisabledMode(c *C) {
	ps.newManager(c, nil)
	defer ps.manager.Stop()

	err := ps.manager.EnablePairing(testWindowDuration)
	c.Assert(err, ErrorMatches, "*. pairing mode disabled")
	c.Assert(ps.manager.PairingEnabled(), Equals, false)
}

// TestEnablePairingSingleModeNotPaired checks that we can pair when using single
// pairing mode, if we never paired before.
func (ps *pairingSuite) TestEnablePairingSingleModeNotPaired(c *C) {
	ps.newManager(c, nil)
	defer ps.manager.Stop()

	ps.updatePlan(pairingstate.ModeSingle)

	err := ps.manager.EnablePairing(testWindowDuration)
	c.Assert(err, IsNil)

	expectWindowEnableDisable(c, testWindowDuration, func() bool {
		return ps.manager.PairingEnabled()

	})
}

// TestEnablePairingSingleModeAlreadyPaired verifies that we will fail to pair in
// single pairing mode if already paired.
func (ps *pairingSuite) TestEnablePairingSingleModeAlreadyPaired(c *C) {
	ps.newManager(c, &pairingstate.PairingDetails{
		Paired: true,
	})
	defer ps.manager.Stop()

	ps.updatePlan(pairingstate.ModeSingle)

	err := ps.manager.EnablePairing(testWindowDuration)
	c.Assert(err, ErrorMatches, ".* already paired in 'single' pairing mode")
	c.Assert(ps.manager.PairingEnabled(), Equals, false)
}

// TestEnablePairingMultipleMode verifies that we can pair when pairing mode
// is set to multiple.
func (ps *pairingSuite) TestEnablePairingMultipleMode(c *C) {
	ps.newManager(c, nil)
	defer ps.manager.Stop()

	ps.updatePlan(pairingstate.ModeMultiple)

	err := ps.manager.EnablePairing(testWindowDuration)
	c.Assert(err, IsNil)

	expectWindowEnableDisable(c, testWindowDuration, func() bool {
		return ps.manager.PairingEnabled()

	})
}

// TestEnablePairingMultipleModeAlreadyPaired verifies that we can pair again when
// pairing mode is set to multiple.
func (ps *pairingSuite) TestEnablePairingMultipleModeAlreadyPaired(c *C) {
	ps.newManager(c, &pairingstate.PairingDetails{
		Paired: true,
	})
	defer ps.manager.Stop()

	ps.updatePlan(pairingstate.ModeMultiple)

	err := ps.manager.EnablePairing(testWindowDuration)
	c.Assert(err, IsNil)

	expectWindowEnableDisable(c, testWindowDuration, func() bool {
		return ps.manager.PairingEnabled()

	})
}

// TestEnablePairingResetTimeout verifies that when pairing is re-enabled while
// the window is still open, the expiry period is reset with the new duration.
func (ps *pairingSuite) TestEnablePairingResetTimeout(c *C) {
	ps.newManager(c, nil)
	defer ps.manager.Stop()

	ps.updatePlan(pairingstate.ModeSingle)

	err := ps.manager.EnablePairing(testWindowDuration)
	c.Assert(err, IsNil)
	c.Assert(ps.manager.PairingEnabled(), Equals, true)

	// Sleep only half the previous period, so we can issue another
	// pairing request in the middle.
	time.Sleep(50 * time.Millisecond)

	err = ps.manager.EnablePairing(testWindowDuration)
	c.Assert(err, IsNil)

	expectWindowEnableDisable(c, testWindowDuration, func() bool {
		return ps.manager.PairingEnabled()

	})
}

// TestEnablePairingUnknownMode verifies that an invalid mode from the plan
// is reported correctly.
func (ps *pairingSuite) TestEnablePairingUnknownMode(c *C) {
	ps.newManager(c, nil)
	defer ps.manager.Stop()

	ps.updatePlan(pairingstate.Mode("foo"))

	err := ps.manager.EnablePairing(testWindowDuration)
	c.Assert(err, ErrorMatches, ".* unknown pairing mode .*")
	c.Assert(ps.manager.PairingEnabled(), Equals, false)
}

// TestPairMTLSSuccess verifies that a successful pairing request closes the
// pairing window and updates identities correctly.
func (ps *pairingSuite) TestPairMTLSSuccess(c *C) {
	clientCert := generateTestClientCert(c)
	ps.newManager(c, nil)
	defer ps.manager.Stop()

	ps.updatePlan(pairingstate.ModeSingle)

	err := ps.manager.EnablePairing(testWindowDuration)
	c.Assert(err, IsNil)
	c.Assert(ps.manager.PairingEnabled(), Equals, true)

	err = ps.manager.PairMTLS(clientCert)
	c.Assert(err, IsNil)

	c.Assert(ps.manager.PairingEnabled(), Equals, false)

	pairingState := ps.PairingState()
	ps.state.Lock()
	identities := ps.state.Identities()
	ps.state.Unlock()

	c.Assert(pairingState.Paired, Equals, true)
	c.Assert(len(identities), Equals, 1)

	identity, exists := identities["user-1"]
	c.Assert(exists, Equals, true)
	c.Assert(identity.Access, Equals, state.AdminAccess)
	c.Assert(identity.Cert, NotNil)
	c.Assert(identity.Cert.X509, NotNil)

	c.Assert(identity.Cert.X509.Equal(clientCert), Equals, true)
}

// TestPairMTLSNotOpen verifies pairing is rejected if the pairing window
// is not open.
func (ps *pairingSuite) TestPairMTLSNotOpen(c *C) {
	clientCert := generateTestClientCert(c)
	ps.newManager(c, nil)
	defer ps.manager.Stop()

	c.Assert(ps.manager.PairingEnabled(), Equals, false)

	err := ps.manager.PairMTLS(clientCert)
	c.Assert(err, ErrorMatches, ".* pairing window is disabled")

	pairingState := ps.PairingState()
	ps.state.Lock()
	identities := ps.state.Identities()
	ps.state.Unlock()

	c.Assert(pairingState.Paired, Equals, false)
	c.Assert(len(identities), Equals, 0)
}

// TestPairMTLSDuplicateCertificate verifies that identities already added
// by a different means (e.g. using the identities add CLI) will result in
// the pairing request succeeding.
func (ps *pairingSuite) TestPairMTLSDuplicateCertificate(c *C) {
	clientCert := generateTestClientCert(c)
	ps.newManager(c, nil)
	defer ps.manager.Stop()

	ps.updatePlan(pairingstate.ModeMultiple)

	ps.state.Lock()
	ps.state.AddIdentities(map[string]*state.Identity{
		"existing-user": {
			Access: state.AdminAccess,
			Cert:   &state.CertIdentity{X509: clientCert},
		},
	})
	ps.state.Unlock()

	err := ps.manager.EnablePairing(testWindowDuration)
	c.Assert(err, IsNil)

	err = ps.manager.PairMTLS(clientCert)
	c.Assert(err, IsNil)

	c.Assert(ps.manager.PairingEnabled(), Equals, false)

	pairingState := ps.PairingState()
	ps.state.Lock()
	identities := ps.state.Identities()
	ps.state.Unlock()

	c.Assert(len(identities), Equals, 1)
	c.Assert(pairingState.Paired, Equals, true)
}

// TestPairMTLSUsernameIncrementing verifies name allocation.
func (ps *pairingSuite) TestPairMTLSUsernameIncrementing(c *C) {
	clientCert := generateTestClientCert(c)
	ps.newManager(c, nil)
	defer ps.manager.Stop()

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

	ps.updatePlan(pairingstate.ModeMultiple)

	err := ps.manager.EnablePairing(10 * time.Millisecond)
	c.Assert(err, IsNil)

	err = ps.manager.PairMTLS(clientCert)
	c.Assert(err, IsNil)

	c.Assert(ps.manager.PairingEnabled(), Equals, false)

	ps.state.Lock()
	identities := ps.state.Identities()
	ps.state.Unlock()

	_, exists := identities["user-2"]
	c.Assert(exists, Equals, true)
}

// TestPlanChangedDisablesPairingWindow verifies that when a PlanChanged event
// modifies the Mode while the pairing window is enabled, the window is closed.
func (ps *pairingSuite) TestPlanChangedDisablesPairingWindow(c *C) {
	ps.newManager(c, nil)
	defer ps.manager.Stop()

	ps.updatePlan(pairingstate.ModeSingle)

	err := ps.manager.EnablePairing(10 * time.Millisecond)
	c.Assert(err, IsNil)
	c.Assert(ps.manager.PairingEnabled(), Equals, true)

	ps.updatePlan(pairingstate.ModeMultiple)

	c.Assert(ps.manager.PairingEnabled(), Equals, false)
}

// TestGenerateUniqueUsername checks that we can generate the name of the
// next free username.
func (ps *pairingSuite) TestGenerateUniqueUsername(c *C) {
	testCases := []struct {
		name               string
		existingIdentities map[string]*state.Identity
		expectedUsername   string
		expectedError      string
	}{{
		name:               "empty identities should return user-1",
		existingIdentities: map[string]*state.Identity{},
		expectedUsername:   "user-1",
	}, {
		name: "single user-1 should return user-2",
		existingIdentities: map[string]*state.Identity{
			"user-1": {Access: state.AdminAccess},
		},
		expectedUsername: "user-2",
	}, {
		name: "non-sequential users should fill gaps",
		existingIdentities: map[string]*state.Identity{
			"user-1": {Access: state.AdminAccess},
			"user-3": {Access: state.AdminAccess},
			"user-5": {Access: state.AdminAccess},
		},
		expectedUsername: "user-2",
	}, {
		name: "non-user prefixed usernames should be ignored",
		existingIdentities: map[string]*state.Identity{
			"admin-1":    {Access: state.AdminAccess},
			"other-user": {Access: state.ReadAccess},
			"user1":      {Access: state.AdminAccess},
			"usertest":   {Access: state.AdminAccess},
		},
		expectedUsername: "user-1",
	}, {
		name: "invalid user suffixes should be ignored",
		existingIdentities: map[string]*state.Identity{
			"user-":    {Access: state.AdminAccess},
			"user-abc": {Access: state.AdminAccess},
			"user-1.5": {Access: state.AdminAccess},
			"user-0":   {Access: state.AdminAccess},
			"user--1":  {Access: state.AdminAccess},
			"user-1-2": {Access: state.AdminAccess},
		},
		expectedUsername: "user-1",
	}, {
		name: "sequential users from 1 to 10",
		existingIdentities: map[string]*state.Identity{
			"user-1":  {Access: state.AdminAccess},
			"user-2":  {Access: state.AdminAccess},
			"user-3":  {Access: state.AdminAccess},
			"user-4":  {Access: state.AdminAccess},
			"user-5":  {Access: state.AdminAccess},
			"user-6":  {Access: state.AdminAccess},
			"user-7":  {Access: state.AdminAccess},
			"user-8":  {Access: state.AdminAccess},
			"user-9":  {Access: state.AdminAccess},
			"user-10": {Access: state.AdminAccess},
		},
		expectedUsername: "user-11",
	}, {
		name: "mixed valid and invalid usernames",
		existingIdentities: map[string]*state.Identity{
			"user-1":     {Access: state.AdminAccess},
			"user-abc":   {Access: state.AdminAccess},
			"user-3":     {Access: state.AdminAccess},
			"admin-user": {Access: state.AdminAccess},
			"user-":      {Access: state.AdminAccess},
			"user-5":     {Access: state.AdminAccess},
		},
		expectedUsername: "user-2",
	}, {
		name: "limit exceeded should return error",
		existingIdentities: func() map[string]*state.Identity {
			identities := make(map[string]*state.Identity)
			for i := 1; i <= 1000; i++ {
				identities[fmt.Sprintf("user-%d", i)] = &state.Identity{Access: state.AdminAccess}
			}
			return identities
		}(),
		expectedError: "user allocation limit 1000 reached",
	}}

	for _, tc := range testCases {
		c.Logf("Running test case: %s", tc.name)

		result, err := pairingstate.GenerateUniqueUsername(tc.existingIdentities)

		if tc.expectedError != "" {
			c.Assert(err, ErrorMatches, tc.expectedError)
		} else {
			c.Assert(err, IsNil)
			c.Assert(result, Equals, tc.expectedUsername)
		}
	}
}
