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
	"crypto/tls"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/truststate"
	"github.com/canonical/pebble/internals/plan"
)

type httpTransportSuite struct{}

var _ = Suite(&httpTransportSuite{})

func (s *httpTransportSuite) TestPreservesBaseTransport(c *C) {
	mgr := newTestManager(c)
	caPEM, _, _ := generateCACert(c, "vendorA")
	mgr.PlanChanged(&plan.Plan{
		TrustContexts: map[string]*plan.TrustContext{
			"vendorA": {Name: "vendorA", X509: &plan.X509TrustContext{CACert: string(caPEM)}},
		},
	})

	proxyURL, err := url.Parse("http://proxy.example.com:3128")
	c.Assert(err, IsNil)
	base := &http.Transport{
		Proxy:           http.ProxyURL(proxyURL),
		MaxIdleConns:    7,
		TLSClientConfig: &tls.Config{ServerName: "example.com"},
	}
	t := &truststate.HTTPTransport{TrustManager: mgr, Name: "vendorA", Base: base}

	rt, err := t.RoundTripper()
	c.Assert(err, IsNil)
	transport, ok := rt.(*http.Transport)
	c.Assert(ok, Equals, true)
	c.Assert(transport, Not(Equals), base)
	c.Assert(transport.MaxIdleConns, Equals, 7)
	req, err := http.NewRequest("GET", "https://example.com", nil)
	c.Assert(err, IsNil)
	gotProxy, err := transport.Proxy(req)
	c.Assert(err, IsNil)
	c.Assert(gotProxy, DeepEquals, proxyURL)
	c.Assert(transport.TLSClientConfig.ServerName, Equals, "example.com")
	c.Assert(transport.TLSClientConfig.RootCAs, NotNil)
	// The base transport's TLS config is not modified.
	c.Assert(base.TLSClientConfig.RootCAs, IsNil)
}

func (s *httpTransportSuite) TestDefaultBaseKeepsProxyFromEnvironment(c *C) {
	mgr := newTestManager(c)
	caPEM, _, _ := generateCACert(c, "vendorA")
	mgr.PlanChanged(&plan.Plan{
		TrustContexts: map[string]*plan.TrustContext{
			"vendorA": {Name: "vendorA", X509: &plan.X509TrustContext{CACert: string(caPEM)}},
		},
	})

	t := &truststate.HTTPTransport{TrustManager: mgr, Name: "vendorA"}
	rt, err := t.RoundTripper()
	c.Assert(err, IsNil)
	transport := rt.(*http.Transport)
	c.Assert(transport.Proxy, NotNil)
	c.Assert(transport.ForceAttemptHTTP2, Equals, true)
	c.Assert(transport.IdleConnTimeout, Equals, http.DefaultTransport.(*http.Transport).IdleConnTimeout)
}

func (s *httpTransportSuite) TestCachedUntilTrustContextChanges(c *C) {
	mgr := newTestManager(c)
	pem1, _, _ := generateCACert(c, "v1")
	pem2, _, _ := generateCACert(c, "v2")
	mgr.PlanChanged(&plan.Plan{
		TrustContexts: map[string]*plan.TrustContext{
			"vendorA": {Name: "vendorA", X509: &plan.X509TrustContext{CACert: string(pem1)}},
		},
	})

	t := &truststate.HTTPTransport{TrustManager: mgr, Name: "vendorA"}
	rt1, err := t.RoundTripper()
	c.Assert(err, IsNil)
	rt2, err := t.RoundTripper()
	c.Assert(err, IsNil)
	c.Assert(rt2, Equals, rt1)

	mgr.PlanChanged(&plan.Plan{
		TrustContexts: map[string]*plan.TrustContext{
			"vendorA": {Name: "vendorA", X509: &plan.X509TrustContext{CACert: string(pem2)}},
		},
	})
	rt3, err := t.RoundTripper()
	c.Assert(err, IsNil)
	c.Assert(rt3, Not(Equals), rt1)
}

func (s *httpTransportSuite) TestSystemUsesBase(c *C) {
	mgr := newTestManager(c)
	base := &http.Transport{}

	t := &truststate.HTTPTransport{TrustManager: mgr, Base: base}
	rt, err := t.RoundTripper()
	c.Assert(err, IsNil)
	c.Assert(rt, Equals, base)

	t = &truststate.HTTPTransport{TrustManager: mgr, Name: "system"}
	rt, err = t.RoundTripper()
	c.Assert(err, IsNil)
	c.Assert(rt, Equals, http.DefaultTransport)
}

func (s *httpTransportSuite) TestFailsClosed(c *C) {
	mgr := newTestManager(c)

	t := &truststate.HTTPTransport{TrustManager: mgr, Name: "unknown"}
	_, err := t.RoundTripper()
	c.Assert(err, ErrorMatches, `cannot resolve trust context "unknown": trust context "unknown" not found`)

	t = &truststate.HTTPTransport{Name: "vendorA"}
	_, err = t.RoundTripper()
	c.Assert(err, ErrorMatches, `cannot resolve trust context "vendorA": no trust manager`)

	// With no trust context set, the base transport is used.
	t = &truststate.HTTPTransport{}
	rt, err := t.RoundTripper()
	c.Assert(err, IsNil)
	c.Assert(rt, Equals, http.DefaultTransport)
}

func (s *httpTransportSuite) TestInvalidNameNeverWritesOutsideTrustDir(c *C) {
	dir := c.MkDir()
	mgr := truststate.NewManager(filepath.Join(dir, "trust"))
	caPEM, _, _ := generateCACert(c, "evil")
	// Bypass plan validation, which would reject this name.
	mgr.PlanChanged(&plan.Plan{
		TrustContexts: map[string]*plan.TrustContext{
			"../outside": {Name: "../outside", X509: &plan.X509TrustContext{CACert: string(caPEM)}},
		},
	})

	tc, err := mgr.TrustContext("../outside")
	c.Assert(err, IsNil)
	_, err = tc.CABundleFile()
	c.Assert(err, ErrorMatches, `cannot use invalid trust context name "../outside"`)
	_, err = os.Stat(filepath.Join(dir, "outside"))
	c.Assert(os.IsNotExist(err), Equals, true)
}
