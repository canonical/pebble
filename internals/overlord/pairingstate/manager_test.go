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

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/overlord/identities"
	"github.com/canonical/pebble/internals/overlord/pairingstate"
)

// testWindowDuration is a carefully selected pairing window duration that is
// long enough to make the test robust on busy test runners.
const testWindowDuration = 100 * time.Millisecond

// TestEnablePairingDisabledMode tests trying to open a pairing window while
// the configuration says it is disabled (before the plan update was received).
func (ps *pairingSuite) TestEnablePairingDisabledMode(c *tc.C) {
	ps.newManager(c, nil)

	err := ps.manager.EnablePairing(testWindowDuration)
	c.Assert(err, tc.ErrorMatches, "*. pairing mode disabled")
	c.Assert(ps.manager.PairingEnabled(), tc.Equals, false)
}

// TestEnablePairingSingleModeNotPaired checks that we can enable the pairing
// window when using single pairing mode, if we never paired before.
func (ps *pairingSuite) TestEnablePairingSingleModeNotPaired(c *tc.C) {
	ps.newManager(c, nil)

	ps.updatePlan(pairingstate.ModeSingle)

	err := ps.manager.EnablePairing(testWindowDuration)
	c.Assert(err, tc.ErrorIsNil)

	ps.expectWindowEnableDisable(c, testWindowDuration)
}

// TestEnablePairingSingleModeAlreadyPaired verifies that we will fail to
// open the pairing window when in single pairing mode and already paired.
func (ps *pairingSuite) TestEnablePairingSingleModeAlreadyPaired(c *tc.C) {
	ps.newManager(c, &pairingstate.PairingDetails{
		Paired: true,
	})

	ps.updatePlan(pairingstate.ModeSingle)

	err := ps.manager.EnablePairing(testWindowDuration)
	c.Assert(err, tc.ErrorMatches, ".* already paired in 'single' pairing mode")
	c.Assert(ps.manager.PairingEnabled(), tc.Equals, false)
}

// TestEnablePairingMultipleMode verifies that we can enable the pairing
// window when pairing mode is set to multiple.
func (ps *pairingSuite) TestEnablePairingMultipleMode(c *tc.C) {
	ps.newManager(c, nil)

	ps.updatePlan(pairingstate.ModeMultiple)

	err := ps.manager.EnablePairing(testWindowDuration)
	c.Assert(err, tc.ErrorIsNil)

	ps.expectWindowEnableDisable(c, testWindowDuration)
}

// TestEnablePairingEarlyEnsure verifies that if we receive the ensure
// early, due to some other manager requesting it, we still honor
// the expiry.
func (ps *pairingSuite) TestEnablePairingEarlyEnsure(c *tc.C) {
	ps.newManager(c, nil)

	ps.updatePlan(pairingstate.ModeMultiple)

	err := ps.manager.EnablePairing(testWindowDuration)
	c.Assert(err, tc.ErrorIsNil)

	// This ensure will be received by the pairing manager before
	// the expected final ensure that disables the window.
	ps.state.EnsureBefore(0)

	ps.expectWindowEnableDisable(c, testWindowDuration)
}

// TestEnablePairingAfterDisable checks that we can still enable the pairing
// window after it expired.
func (ps *pairingSuite) TestEnablePairingAfterDisable(c *tc.C) {
	ps.newManager(c, nil)

	ps.updatePlan(pairingstate.ModeSingle)

	err := ps.manager.EnablePairing(testWindowDuration)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(ps.manager.PairingEnabled(), tc.Equals, true)

	// Wait for expiry and the window to get disabled automatically by the overlord's ensure loop
	time.Sleep(testWindowDuration + 50*time.Millisecond)

	c.Assert(ps.manager.PairingEnabled(), tc.Equals, false)

	// Now we should be able to enable the pairing window again
	err = ps.manager.EnablePairing(testWindowDuration)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(ps.manager.PairingEnabled(), tc.Equals, true)
}

// TestEnablePairingMultipleModeAlreadyPaired verifies that we can enable
// the pairing window again when pairing mode is set to multiple, with
// a client already paired.
func (ps *pairingSuite) TestEnablePairingMultipleModeAlreadyPaired(c *tc.C) {
	ps.newManager(c, &pairingstate.PairingDetails{
		Paired: true,
	})

	ps.updatePlan(pairingstate.ModeMultiple)

	err := ps.manager.EnablePairing(testWindowDuration)
	c.Assert(err, tc.ErrorIsNil)

	ps.expectWindowEnableDisable(c, testWindowDuration)
}

// TestEnablePairingResetTimeout verifies that when pairing is re-enabled while
// the window is still open, the expiry period is reset with the new duration.
func (ps *pairingSuite) TestEnablePairingResetTimeout(c *tc.C) {
	ps.newManager(c, nil)

	ps.updatePlan(pairingstate.ModeSingle)

	err := ps.manager.EnablePairing(testWindowDuration)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(ps.manager.PairingEnabled(), tc.Equals, true)

	// Sleep only half the previous period, so we can issue another
	// enable pairing window request in the middle.
	time.Sleep(50 * time.Millisecond)

	err = ps.manager.EnablePairing(testWindowDuration)
	c.Assert(err, tc.ErrorIsNil)

	ps.expectWindowEnableDisable(c, testWindowDuration)
}

// TestEnablePairingUnknownMode verifies that an invalid mode from the plan
// is reported correctly.
func (ps *pairingSuite) TestEnablePairingUnknownMode(c *tc.C) {
	ps.newManager(c, nil)

	ps.updatePlan(pairingstate.Mode("foo"))

	err := ps.manager.EnablePairing(testWindowDuration)
	c.Assert(err, tc.ErrorMatches, ".* unknown pairing mode .*")
	c.Assert(ps.manager.PairingEnabled(), tc.Equals, false)
}

// TestPairMTLSSuccess verifies that a successful pairing request closes the
// pairing window and updates identities correctly.
func (ps *pairingSuite) TestPairMTLSSuccess(c *tc.C) {
	clientCert := generateTestClientCert(c)
	ps.newManager(c, nil)

	ps.updatePlan(pairingstate.ModeSingle)

	err := ps.manager.EnablePairing(testWindowDuration)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(ps.manager.PairingEnabled(), tc.Equals, true)

	err = ps.manager.PairMTLS(clientCert)
	c.Assert(err, tc.ErrorIsNil)

	c.Assert(ps.manager.PairingEnabled(), tc.Equals, false)

	pairingDetails := ps.PairingDetails()
	ps.state.Lock()
	idents := ps.identitiesMgr.Identities()
	ps.state.Unlock()

	c.Assert(pairingDetails.Paired, tc.Equals, true)
	c.Assert(len(idents), tc.Equals, 1)

	identity, exists := idents["user-1"]
	c.Assert(exists, tc.Equals, true)
	c.Assert(identity.Access, tc.Equals, identities.AdminAccess)
	c.Assert(identity.Cert, tc.NotNil)
	c.Assert(identity.Cert.X509, tc.NotNil)

	c.Assert(identity.Cert.X509.Equal(clientCert), tc.Equals, true)
}

// TestPairMTLSNotOpen verifies pairing is rejected if the pairing window
// is not open.
func (ps *pairingSuite) TestPairMTLSNotOpen(c *tc.C) {
	clientCert := generateTestClientCert(c)
	ps.newManager(c, nil)

	c.Assert(ps.manager.PairingEnabled(), tc.Equals, false)

	err := ps.manager.PairMTLS(clientCert)
	c.Assert(err, tc.ErrorMatches, ".* pairing window is disabled")

	pairingDetails := ps.PairingDetails()
	ps.state.Lock()
	idents := ps.identitiesMgr.Identities()
	ps.state.Unlock()

	c.Assert(pairingDetails.Paired, tc.Equals, false)
	c.Assert(len(idents), tc.Equals, 0)
}

// TestPairMTLSDuplicateCertificate verifies that identities already added
// by a different means (e.g. using the identities add CLI) will result in
// the pairing request succeeding.
func (ps *pairingSuite) TestPairMTLSDuplicateCertificate(c *tc.C) {
	clientCert := generateTestClientCert(c)
	ps.newManager(c, nil)

	ps.updatePlan(pairingstate.ModeMultiple)

	ps.state.Lock()
	ps.identitiesMgr.AddIdentities(map[string]*identities.Identity{
		"existing-user": {
			Access: identities.AdminAccess,
			Cert:   &identities.CertIdentity{X509: clientCert},
		},
	})
	ps.state.Unlock()

	err := ps.manager.EnablePairing(testWindowDuration)
	c.Assert(err, tc.ErrorIsNil)

	err = ps.manager.PairMTLS(clientCert)
	c.Assert(err, tc.ErrorIsNil)

	c.Assert(ps.manager.PairingEnabled(), tc.Equals, false)

	pairingDetails := ps.PairingDetails()
	ps.state.Lock()
	idents := ps.identitiesMgr.Identities()
	ps.state.Unlock()

	c.Assert(len(idents), tc.Equals, 1)
	c.Assert(pairingDetails.Paired, tc.Equals, true)
}

// TestPairMTLSUsernameIncrementing verifies name allocation.
func (ps *pairingSuite) TestPairMTLSUsernameIncrementing(c *tc.C) {
	clientCert := generateTestClientCert(c)
	ps.newManager(c, nil)

	ps.state.Lock()
	ps.identitiesMgr.AddIdentities(map[string]*identities.Identity{
		"user-3": {
			Access: identities.AdminAccess,
			Local:  &identities.LocalIdentity{UserID: 1000},
		},
		"user-1": {
			Access: identities.ReadAccess,
			Local:  &identities.LocalIdentity{UserID: 1001},
		},
		"other-user": {
			Access: identities.AdminAccess,
			Local:  &identities.LocalIdentity{UserID: 1002},
		},
	})
	ps.state.Unlock()

	ps.updatePlan(pairingstate.ModeMultiple)

	err := ps.manager.EnablePairing(10 * time.Millisecond)
	c.Assert(err, tc.ErrorIsNil)

	err = ps.manager.PairMTLS(clientCert)
	c.Assert(err, tc.ErrorIsNil)

	c.Assert(ps.manager.PairingEnabled(), tc.Equals, false)

	ps.state.Lock()
	idents := ps.identitiesMgr.Identities()
	ps.state.Unlock()

	_, exists := idents["user-2"]
	c.Assert(exists, tc.Equals, true)
}

// TestPlanChangedDisablesPairingWindow verifies that when a PlanChanged event
// modifies the Mode while the pairing window is enabled, the window is closed.
func (ps *pairingSuite) TestPlanChangedDisablesPairingWindow(c *tc.C) {
	ps.newManager(c, nil)

	ps.updatePlan(pairingstate.ModeSingle)

	err := ps.manager.EnablePairing(10 * time.Millisecond)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(ps.manager.PairingEnabled(), tc.Equals, true)

	ps.updatePlan(pairingstate.ModeMultiple)

	c.Assert(ps.manager.PairingEnabled(), tc.Equals, false)
}

// TestGenerateUniqueUsername checks that we can generate the name of the
// next free username.
func (ps *pairingSuite) TestGenerateUniqueUsername(c *tc.C) {
	testCases := []struct {
		name               string
		existingIdentities map[string]*identities.Identity
		expectedUsername   string
		expectedError      string
	}{{
		name:               "empty identities should return user-1",
		existingIdentities: map[string]*identities.Identity{},
		expectedUsername:   "user-1",
	}, {
		name: "single user-1 should return user-2",
		existingIdentities: map[string]*identities.Identity{
			"user-1": {Access: identities.AdminAccess},
		},
		expectedUsername: "user-2",
	}, {
		name: "non-sequential users should fill gaps",
		existingIdentities: map[string]*identities.Identity{
			"user-1": {Access: identities.AdminAccess},
			"user-3": {Access: identities.AdminAccess},
			"user-5": {Access: identities.AdminAccess},
		},
		expectedUsername: "user-2",
	}, {
		name: "non-user prefixed usernames should be ignored",
		existingIdentities: map[string]*identities.Identity{
			"admin-1":    {Access: identities.AdminAccess},
			"other-user": {Access: identities.ReadAccess},
			"user1":      {Access: identities.AdminAccess},
			"usertest":   {Access: identities.AdminAccess},
		},
		expectedUsername: "user-1",
	}, {
		name: "invalid user suffixes should be ignored",
		existingIdentities: map[string]*identities.Identity{
			"user-":    {Access: identities.AdminAccess},
			"user-abc": {Access: identities.AdminAccess},
			"user-1.5": {Access: identities.AdminAccess},
			"user-0":   {Access: identities.AdminAccess},
			"user--1":  {Access: identities.AdminAccess},
			"user-1-2": {Access: identities.AdminAccess},
		},
		expectedUsername: "user-1",
	}, {
		name: "sequential users from 1 to 10",
		existingIdentities: map[string]*identities.Identity{
			"user-1":  {Access: identities.AdminAccess},
			"user-2":  {Access: identities.AdminAccess},
			"user-3":  {Access: identities.AdminAccess},
			"user-4":  {Access: identities.AdminAccess},
			"user-5":  {Access: identities.AdminAccess},
			"user-6":  {Access: identities.AdminAccess},
			"user-7":  {Access: identities.AdminAccess},
			"user-8":  {Access: identities.AdminAccess},
			"user-9":  {Access: identities.AdminAccess},
			"user-10": {Access: identities.AdminAccess},
		},
		expectedUsername: "user-11",
	}, {
		name: "mixed valid and invalid usernames",
		existingIdentities: map[string]*identities.Identity{
			"user-1":     {Access: identities.AdminAccess},
			"user-abc":   {Access: identities.AdminAccess},
			"user-3":     {Access: identities.AdminAccess},
			"admin-user": {Access: identities.AdminAccess},
			"user-":      {Access: identities.AdminAccess},
			"user-5":     {Access: identities.AdminAccess},
		},
		expectedUsername: "user-2",
	}, {
		name: "limit exceeded should return error",
		existingIdentities: func() map[string]*identities.Identity {
			idents := make(map[string]*identities.Identity)
			for i := 1; i <= 1000; i++ {
				idents[fmt.Sprintf("user-%d", i)] = &identities.Identity{Access: identities.AdminAccess}
			}
			return idents
		}(),
		expectedError: "user allocation limit 1000 reached",
	}}

	for _, test := range testCases {
		c.Logf("Running test case: %s", test.name)

		result, err := pairingstate.GenerateUniqueUsername(test.existingIdentities)

		if test.expectedError != "" {
			c.Assert(err, tc.ErrorMatches, test.expectedError)
		} else {
			c.Assert(err, tc.ErrorIsNil)
			c.Assert(result, tc.Equals, test.expectedUsername)
		}
	}
}
