// Copyright (c) 2026 Canonical Ltd
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

package truststate_test

import (
	"crypto/x509"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/truststate"
	"github.com/canonical/pebble/internals/plan"
)

type trustSuite struct{}

var _ = Suite(&trustSuite{})

func newTestManager(c *C) *truststate.TrustManager {
	return truststate.NewManager(filepath.Join(c.MkDir(), "trust"))
}

func (s *trustSuite) TestBuiltinContexts(c *C) {
	mgr := newTestManager(c)

	sys, err := mgr.TrustContext("system")
	c.Assert(err, IsNil)
	defer sys.Close()
	sysPool, err := sys.CAPool()
	c.Assert(err, IsNil)
	c.Assert(sysPool, NotNil)

	def, err := mgr.TrustContext("default")
	c.Assert(err, IsNil)
	defer def.Close()
	defPool, err := def.CAPool()
	c.Assert(err, IsNil)
	c.Assert(defPool, NotNil)

	_, err = mgr.TrustContext("unknown")
	c.Assert(err, ErrorMatches, `trust context "unknown" not found`)
}

func (s *trustSuite) TestCustomTrustContextPoolAndFile(c *C) {
	mgr := newTestManager(c)

	caPEM, caCert, caKey := generateCACert(c, "vendorA")
	leaf := generateLeafCert(c, caCert, caKey, "leaf.vendora.example")

	mgr.PlanChanged(&plan.Plan{
		TrustContexts: map[string]*plan.TrustContext{
			"vendorA": {
				Name: "vendorA",
				TLS:  &plan.TLSTrustContext{CACert: string(caPEM)},
			},
		},
	})

	tc, err := mgr.TrustContext("vendorA")
	c.Assert(err, IsNil)
	defer tc.Close()

	pool, err := tc.CAPool()
	c.Assert(err, IsNil)
	_, err = leaf.Verify(x509.VerifyOptions{Roots: pool})
	c.Assert(err, IsNil)

	path, err := tc.CABundleFile()
	c.Assert(err, IsNil)
	c.Assert(strings.HasPrefix(filepath.Base(path), "vendorA-"), Equals, true)
	c.Assert(strings.HasSuffix(filepath.Base(path), "-tls-ca-bundle.pem"), Equals, true)

	data, err := os.ReadFile(path)
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, caPEM)
}

func (s *trustSuite) TestIncludeCyclesAreBroken(c *C) {
	mgr := newTestManager(c)

	aPEM, aCert, aKey := generateCACert(c, "A")
	bPEM, bCert, bKey := generateCACert(c, "B")
	leafA := generateLeafCert(c, aCert, aKey, "leafA")
	leafB := generateLeafCert(c, bCert, bKey, "leafB")

	mgr.PlanChanged(&plan.Plan{
		TrustContexts: map[string]*plan.TrustContext{
			"a": {Name: "a", Include: []string{"b"}, TLS: &plan.TLSTrustContext{CACert: string(aPEM)}},
			"b": {Name: "b", Include: []string{"a"}, TLS: &plan.TLSTrustContext{CACert: string(bPEM)}},
		},
	})

	tc, err := mgr.TrustContext("a")
	c.Assert(err, IsNil)
	defer tc.Close()

	pool, err := tc.CAPool()
	c.Assert(err, IsNil)

	_, err = leafA.Verify(x509.VerifyOptions{Roots: pool})
	c.Assert(err, IsNil)
	_, err = leafB.Verify(x509.VerifyOptions{Roots: pool})
	c.Assert(err, IsNil)
}

func (s *trustSuite) TestDefaultIncludesSystemAndCustom(c *C) {
	mgr := newTestManager(c)

	caPEM, caCert, caKey := generateCACert(c, "vendorA")
	leaf := generateLeafCert(c, caCert, caKey, "leaf")

	mgr.PlanChanged(&plan.Plan{
		TrustContexts: map[string]*plan.TrustContext{
			"vendorA": {Name: "vendorA", TLS: &plan.TLSTrustContext{CACert: string(caPEM)}},
			"default": {Name: "default", Include: []string{"vendorA"}},
		},
	})

	tc, err := mgr.TrustContext("default")
	c.Assert(err, IsNil)
	defer tc.Close()

	pool, err := tc.CAPool()
	c.Assert(err, IsNil)
	_, err = leaf.Verify(x509.VerifyOptions{Roots: pool})
	c.Assert(err, IsNil)
}

func (s *trustSuite) TestUnknownIncludeRetainsPreviousGoodState(c *C) {
	mgr := newTestManager(c)

	caPEM, caCert, caKey := generateCACert(c, "vendorA")
	leaf := generateLeafCert(c, caCert, caKey, "leaf")

	mgr.PlanChanged(&plan.Plan{
		TrustContexts: map[string]*plan.TrustContext{
			"vendorA": {Name: "vendorA", TLS: &plan.TLSTrustContext{CACert: string(caPEM)}},
		},
	})

	tc1, err := mgr.TrustContext("vendorA")
	c.Assert(err, IsNil)
	defer tc1.Close()
	path1, err := tc1.CABundleFile()
	c.Assert(err, IsNil)

	// Update the plan with an invalid trust context (referencing an unknown
	// included trust context). The previous good version should be kept.
	mgr.PlanChanged(&plan.Plan{
		TrustContexts: map[string]*plan.TrustContext{
			"vendorA": {Name: "vendorA", Include: []string{"doesnotexist"}},
		},
	})

	tc2, err := mgr.TrustContext("vendorA")
	c.Assert(err, IsNil)
	defer tc2.Close()
	path2, err := tc2.CABundleFile()
	c.Assert(err, IsNil)
	c.Assert(path2, Equals, path1)

	pool, err := tc2.CAPool()
	c.Assert(err, IsNil)
	_, err = leaf.Verify(x509.VerifyOptions{Roots: pool})
	c.Assert(err, IsNil)
}

func (s *trustSuite) TestFileLifecycleAcrossUpdatesAndRelease(c *C) {
	mgr := newTestManager(c)

	pem1, _, _ := generateCACert(c, "vendorA-v1")
	pem2, _, _ := generateCACert(c, "vendorA-v2")

	mgr.PlanChanged(&plan.Plan{
		TrustContexts: map[string]*plan.TrustContext{
			"vendorA": {Name: "vendorA", TLS: &plan.TLSTrustContext{CACert: string(pem1)}},
		},
	})

	tcOld, err := mgr.TrustContext("vendorA")
	c.Assert(err, IsNil)
	oldPath, err := tcOld.CABundleFile()
	c.Assert(err, IsNil)
	_, err = os.Stat(oldPath)
	c.Assert(err, IsNil)

	mgr.PlanChanged(&plan.Plan{
		TrustContexts: map[string]*plan.TrustContext{
			"vendorA": {Name: "vendorA", TLS: &plan.TLSTrustContext{CACert: string(pem2)}},
		},
	})

	// The old version's file must still be present: tcOld hasn't been
	// released yet, even though it's now superseded.
	_, err = os.Stat(oldPath)
	c.Assert(err, IsNil)

	tcNew, err := mgr.TrustContext("vendorA")
	c.Assert(err, IsNil)
	newPath, err := tcNew.CABundleFile()
	c.Assert(err, IsNil)
	c.Assert(newPath, Not(Equals), oldPath)

	// Releasing the superseded reference should now clean up its file.
	tcOld.Close()
	_, err = os.Stat(oldPath)
	c.Assert(os.IsNotExist(err), Equals, true)

	// The new (current) version's file must remain until it's released.
	_, err = os.Stat(newPath)
	c.Assert(err, IsNil)
	tcNew.Close()
}

func (s *trustSuite) TestRemovedTrustContextCleanup(c *C) {
	mgr := newTestManager(c)

	caPEM, _, _ := generateCACert(c, "vendorA")
	mgr.PlanChanged(&plan.Plan{
		TrustContexts: map[string]*plan.TrustContext{
			"vendorA": {Name: "vendorA", TLS: &plan.TLSTrustContext{CACert: string(caPEM)}},
		},
	})

	tc, err := mgr.TrustContext("vendorA")
	c.Assert(err, IsNil)
	path, err := tc.CABundleFile()
	c.Assert(err, IsNil)

	// Release the only outstanding reference before removing the trust
	// context. Since nothing has superseded it yet, the file must remain.
	tc.Close()
	_, err = os.Stat(path)
	c.Assert(err, IsNil)

	// Now remove vendorA from the plan entirely.
	mgr.PlanChanged(&plan.Plan{})

	_, err = mgr.TrustContext("vendorA")
	c.Assert(err, ErrorMatches, `trust context "vendorA" not found`)

	_, err = os.Stat(path)
	c.Assert(os.IsNotExist(err), Equals, true)
}

func (s *trustSuite) TestReleaseIdempotentAndUseAfterRelease(c *C) {
	mgr := newTestManager(c)

	tc, err := mgr.TrustContext("system")
	c.Assert(err, IsNil)
	tc.Close()
	tc.Close() // must not panic

	_, err = tc.CAPool()
	c.Assert(err, Equals, truststate.ErrClosed)
	_, err = tc.CABundleFile()
	c.Assert(err, Equals, truststate.ErrClosed)
}

func (s *trustSuite) TestSameContentKeepsSameVersion(c *C) {
	mgr := newTestManager(c)

	caPEM, _, _ := generateCACert(c, "vendorA")
	pl := &plan.Plan{
		TrustContexts: map[string]*plan.TrustContext{
			"vendorA": {Name: "vendorA", TLS: &plan.TLSTrustContext{CACert: string(caPEM)}},
		},
	}
	mgr.PlanChanged(pl)

	tc1, err := mgr.TrustContext("vendorA")
	c.Assert(err, IsNil)
	defer tc1.Close()
	path1, err := tc1.CABundleFile()
	c.Assert(err, IsNil)

	// Re-announce an equivalent plan; the resolved content is unchanged so
	// the same version (and file) should still be in use.
	mgr.PlanChanged(pl)

	tc2, err := mgr.TrustContext("vendorA")
	c.Assert(err, IsNil)
	defer tc2.Close()
	path2, err := tc2.CABundleFile()
	c.Assert(err, IsNil)
	c.Assert(path2, Equals, path1)

	_, err = os.Stat(path1)
	c.Assert(err, IsNil)
}
