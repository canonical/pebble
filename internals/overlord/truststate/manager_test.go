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
				X509: &plan.X509TrustContext{CACert: string(caPEM)},
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
	c.Assert(filepath.Base(path), Equals, "x509-ca.pem")
	c.Assert(filepath.Base(filepath.Dir(path)), Equals, "vendorA")
	c.Assert(filepath.Base(filepath.Dir(filepath.Dir(path))), Equals, "trust")

	st, err := os.Stat(path)
	c.Assert(err, IsNil)
	c.Assert(st.Mode().Perm(), Equals, os.FileMode(0o644))

	data, err := os.ReadFile(path)
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, string(caPEM))
}

func (s *trustSuite) TestIncludeCyclesAreBroken(c *C) {
	mgr := newTestManager(c)

	aPEM, aCert, aKey := generateCACert(c, "A")
	bPEM, bCert, bKey := generateCACert(c, "B")
	leafA := generateLeafCert(c, aCert, aKey, "leafA")
	leafB := generateLeafCert(c, bCert, bKey, "leafB")

	mgr.PlanChanged(&plan.Plan{
		TrustContexts: map[string]*plan.TrustContext{
			"a": {Name: "a", Include: []string{"b"}, X509: &plan.X509TrustContext{CACert: string(aPEM)}},
			"b": {Name: "b", Include: []string{"a"}, X509: &plan.X509TrustContext{CACert: string(bPEM)}},
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
			"vendorA": {Name: "vendorA", X509: &plan.X509TrustContext{CACert: string(caPEM)}},
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
			"vendorA": {Name: "vendorA", X509: &plan.X509TrustContext{CACert: string(caPEM)}},
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

func (s *trustSuite) TestFileUpdatedInPlace(c *C) {
	mgr := newTestManager(c)

	pem1, _, _ := generateCACert(c, "vendorA-v1")
	pem2, _, _ := generateCACert(c, "vendorA-v2")

	mgr.PlanChanged(&plan.Plan{
		TrustContexts: map[string]*plan.TrustContext{
			"vendorA": {Name: "vendorA", X509: &plan.X509TrustContext{CACert: string(pem1)}},
		},
	})

	tcOld, err := mgr.TrustContext("vendorA")
	c.Assert(err, IsNil)
	defer tcOld.Close()
	path, err := tcOld.CABundleFile()
	c.Assert(err, IsNil)
	data, err := os.ReadFile(path)
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, string(pem1))

	mgr.PlanChanged(&plan.Plan{
		TrustContexts: map[string]*plan.TrustContext{
			"vendorA": {Name: "vendorA", X509: &plan.X509TrustContext{CACert: string(pem2)}},
		},
	})

	// The file, having already been written, is updated with the new
	// content straight away.
	data, err = os.ReadFile(path)
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, string(pem2))

	tcNew, err := mgr.TrustContext("vendorA")
	c.Assert(err, IsNil)
	defer tcNew.Close()
	c.Assert(tcNew.Version(), Not(Equals), tcOld.Version())
	newPath, err := tcNew.CABundleFile()
	c.Assert(err, IsNil)
	c.Assert(newPath, Equals, path)
}

func (s *trustSuite) TestRemovedTrustContextFileKeptWhileReferenced(c *C) {
	mgr := newTestManager(c)

	caPEM, _, _ := generateCACert(c, "vendorA")
	mgr.PlanChanged(&plan.Plan{
		TrustContexts: map[string]*plan.TrustContext{
			"vendorA": {Name: "vendorA", X509: &plan.X509TrustContext{CACert: string(caPEM)}},
		},
	})

	tc, err := mgr.TrustContext("vendorA")
	c.Assert(err, IsNil)
	path, err := tc.CABundleFile()
	c.Assert(err, IsNil)

	mgr.PlanChanged(&plan.Plan{})

	// Still referenced, so the file remains.
	_, err = os.Stat(path)
	c.Assert(err, IsNil)

	tc.Close()
	_, err = os.Stat(filepath.Dir(path))
	c.Assert(os.IsNotExist(err), Equals, true)
}

func (s *trustSuite) TestRemovedTrustContextCleanup(c *C) {
	mgr := newTestManager(c)

	caPEM, _, _ := generateCACert(c, "vendorA")
	mgr.PlanChanged(&plan.Plan{
		TrustContexts: map[string]*plan.TrustContext{
			"vendorA": {Name: "vendorA", X509: &plan.X509TrustContext{CACert: string(caPEM)}},
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

	_, err = os.Stat(filepath.Dir(path))
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
			"vendorA": {Name: "vendorA", X509: &plan.X509TrustContext{CACert: string(caPEM)}},
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

func (s *trustSuite) TestIncludedCertsFirstAndDuplicatesDropped(c *C) {
	mgr := newTestManager(c)

	aPEM, _, _ := generateCACert(c, "A")
	bPEM, _, _ := generateCACert(c, "B")
	cPEM, _, _ := generateCACert(c, "C")

	mgr.PlanChanged(&plan.Plan{
		TrustContexts: map[string]*plan.TrustContext{
			"a": {Name: "a", X509: &plan.X509TrustContext{CACert: string(aPEM)}},
			"b": {Name: "b", Include: []string{"a"}, X509: &plan.X509TrustContext{CACert: string(bPEM)}},
			"c": {
				Name:    "c",
				Include: []string{"b", "a"},
				X509:    &plan.X509TrustContext{CACert: string(cPEM) + string(aPEM)},
			},
		},
	})

	tc, err := mgr.TrustContext("c")
	c.Assert(err, IsNil)
	defer tc.Close()
	path, err := tc.CABundleFile()
	c.Assert(err, IsNil)
	data, err := os.ReadFile(path)
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, string(aPEM)+string(bPEM)+string(cPEM))
}

func (s *trustSuite) TestDefaultWithoutIncludeTrustsNothing(c *C) {
	mgr := newTestManager(c)

	def, err := mgr.TrustContext("default")
	c.Assert(err, IsNil)
	c.Assert(def.IsSystemCA(), Equals, true)
	def.Close()

	mgr.PlanChanged(&plan.Plan{
		TrustContexts: map[string]*plan.TrustContext{
			"default": {Name: "default", Override: plan.ReplaceOverride},
		},
	})

	def, err = mgr.TrustContext("default")
	c.Assert(err, IsNil)
	defer def.Close()
	c.Assert(def.IsSystemCA(), Equals, false)
	pool, err := def.CAPool()
	c.Assert(err, IsNil)
	c.Assert(pool.Equal(x509.NewCertPool()), Equals, true)
	path, err := def.CABundleFile()
	c.Assert(err, IsNil)
	data, err := os.ReadFile(path)
	c.Assert(err, IsNil)
	c.Assert(data, HasLen, 0)
}

func (s *trustSuite) TestSystemEquivalence(c *C) {
	mgr := newTestManager(c)

	caPEM, _, _ := generateCACert(c, "vendorA")
	mgr.PlanChanged(&plan.Plan{
		TrustContexts: map[string]*plan.TrustContext{
			"onlysystem": {Name: "onlysystem", Include: []string{"system"}},
			"viadefault": {Name: "viadefault", Include: []string{"default"}},
			"extra": {
				Name:    "extra",
				Include: []string{"system"},
				X509:    &plan.X509TrustContext{CACert: string(caPEM)},
			},
		},
	})

	for name, expected := range map[string]bool{
		"system":     true,
		"default":    true,
		"onlysystem": true,
		"viadefault": true,
		"extra":      false,
	} {
		tc, err := mgr.TrustContext(name)
		c.Assert(err, IsNil)
		c.Check(tc.IsSystemCA(), Equals, expected, Commentf("trust context %q", name))
		tc.Close()
	}
}
