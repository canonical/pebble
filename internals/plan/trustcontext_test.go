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

package plan_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/plan"
)

func generateTestCACert(c *C, commonName string) string {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	c.Assert(err, IsNil)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	c.Assert(err, IsNil)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

// indentPEM indents each line of a PEM bundle so it can be embedded as a YAML
// block scalar.
func indentPEM(data string, indent int) string {
	prefix := strings.Repeat(" ", indent)
	return prefix + strings.ReplaceAll(strings.TrimSpace(data), "\n", "\n"+prefix)
}

func combineTrustLayers(c *C, layers ...string) (*plan.Plan, error) {
	p := &plan.Plan{}
	for i, yml := range layers {
		layer, err := plan.ParseLayer(i, fmt.Sprintf("layer-%d", i), []byte(yml))
		if err != nil {
			return nil, err
		}
		p.Layers = append(p.Layers, layer)
	}
	combined, err := plan.CombineLayers(p.Layers...)
	if err != nil {
		return nil, err
	}
	p.Services = combined.Services
	p.Checks = combined.Checks
	p.LogTargets = combined.LogTargets
	p.TrustContexts = combined.TrustContexts
	p.Sections = combined.Sections
	return p, p.Validate()
}

func (s *S) TestTrustContextIncludeForms(c *C) {
	p, err := combineTrustLayers(c, `
trust-contexts:
    single:
        override: replace
        include: system
    list:
        override: replace
        include: [system, single]
    empty:
        override: replace
        include: []
    unset:
        override: replace
`)
	c.Assert(err, IsNil)
	c.Check(p.TrustContexts["single"].Name, Equals, "single")
	c.Check(p.TrustContexts["single"].Include, DeepEquals, plan.TrustContextIncludes{"system"})
	c.Check(p.TrustContexts["list"].Include, DeepEquals, plan.TrustContextIncludes{"system", "single"})
	c.Check(p.TrustContexts["empty"].Include, NotNil)
	c.Check(p.TrustContexts["empty"].Include, HasLen, 0)
	c.Check(p.TrustContexts["unset"].Include, IsNil)

	_, err = combineTrustLayers(c, `
trust-contexts:
    bad:
        override: replace
        include: {a: b}
`)
	c.Assert(err, ErrorMatches, `.*include must be a string or a list of strings.*`)
}

func (s *S) TestTrustContextMerge(c *C) {
	certA := generateTestCACert(c, "A")
	certB := generateTestCACert(c, "B")
	p, err := combineTrustLayers(c, fmt.Sprintf(`
trust-contexts:
    other:
        override: replace
    ctx:
        override: replace
        include: [system]
        x509:
            ca-cert: |
%s
`, indentPEM(certA, 16)), fmt.Sprintf(`
trust-contexts:
    ctx:
        override: merge
        include: other
        x509:
            ca-cert: |
%s
%s
`, indentPEM(certA, 16), indentPEM(certB, 16)), `
trust-contexts:
    ctx:
        override: merge
`)
	c.Assert(err, IsNil)
	ctx := p.TrustContexts["ctx"]
	// The include is replaced, not appended to, and a later layer without
	// include leaves it alone.
	c.Check(ctx.Include, DeepEquals, plan.TrustContextIncludes{"other"})
	// Certificates are appended, with duplicates dropped.
	c.Check(ctx.X509.CACert, Equals, certA+certB)

	p, err = combineTrustLayers(c, `
trust-contexts:
    ctx:
        override: replace
        include: [system]
`, `
trust-contexts:
    ctx:
        override: merge
        include: []
`)
	c.Assert(err, IsNil)
	c.Check(p.TrustContexts["ctx"].Include, HasLen, 0)
}

func (s *S) TestTrustContextValidation(c *C) {
	cert := generateTestCACert(c, "A")
	tests := []struct {
		summary string
		layer   string
		error   string
	}{{
		summary: "system is reserved",
		layer: `
trust-contexts:
    system:
        override: replace
`,
		error: `trust context name "system" is reserved`,
	}, {
		summary: "default must use override replace",
		layer: `
trust-contexts:
    default:
        override: merge
        include: system
`,
		error: `trust context "default" must use "override: replace"`,
	}, {
		summary: "default cannot define x509",
		layer: fmt.Sprintf(`
trust-contexts:
    default:
        override: replace
        x509:
            ca-cert: |
%s
`, indentPEM(cert, 16)),
		error: `trust context "default" cannot define "x509" directly, only "include"`,
	}, {
		summary: "invalid ca-cert",
		layer: `
trust-contexts:
    ctx:
        override: replace
        x509:
            ca-cert: not a certificate
`,
		error: `trust context "ctx" has invalid "ca-cert": .*`,
	}, {
		summary: "missing override",
		layer: `
trust-contexts:
    ctx:
        include: system
`,
		error: `layer "layer-0" must define "override" for trust context "ctx"`,
	}, {
		summary: "unknown include",
		layer: `
trust-contexts:
    ctx:
        override: replace
        include: missing
`,
		error: `layer "layer-0" trust context "ctx" includes unknown trust context "missing"`,
	}, {
		summary: "unknown service trust context",
		layer: `
services:
    srv:
        override: replace
        command: cmd
        trust-context: missing
`,
		error: `layer "layer-0" service "srv" specifies unknown trust context "missing"`,
	}, {
		summary: "unknown check trust context",
		layer: `
checks:
    chk:
        override: replace
        http:
            url: https://example.com
            trust-context: missing
`,
		error: `layer "layer-0" check "chk" specifies unknown trust context "missing"`,
	}, {
		summary: "unknown log target trust context",
		layer: `
log-targets:
    tgt:
        override: replace
        type: loki
        location: https://example.com
        services: []
        trust-context: missing
`,
		error: `layer "layer-0" log target "tgt" specifies unknown trust context "missing"`,
	}, {
		summary: "trust-context is accepted (and ignored) for syslog log targets",
		layer: `
log-targets:
    tgt:
        override: replace
        type: syslog
        location: tcp://example.com:514
        services: []
        trust-context: default
`,
	}, {
		summary: "default without include is valid",
		layer: `
trust-contexts:
    default:
        override: replace
services:
    srv:
        override: replace
        command: cmd
        trust-context: default
`,
	}}
	for _, test := range tests {
		c.Logf(test.summary)
		_, err := combineTrustLayers(c, test.layer)
		if test.error == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, test.error)
		}
	}
}

func (s *S) TestTrustContextInvalidNames(c *C) {
	for _, name := range []string{"..", ".", "../outside", "foo/bar", `foo\bar`, "/abs", ".hidden", "-dash", "dash-", "a b", "a.b", "a_b", "a..b"} {
		c.Logf("name: %q", name)
		_, err := combineTrustLayers(c, fmt.Sprintf(`
trust-contexts:
    %q:
        override: replace
`, name))
		c.Assert(err, NotNil)
		c.Check(err.Error(), Equals, fmt.Sprintf(`cannot use invalid trust context name %q`, name))
	}
	for _, name := range []string{"vendorA", "vendor-a", "vendor--a", "1ca", "a", "9"} {
		c.Logf("name: %q", name)
		_, err := combineTrustLayers(c, fmt.Sprintf(`
trust-contexts:
    %q:
        override: replace
`, name))
		c.Check(err, IsNil)
	}
}

func (s *S) TestPlanValidateNilTrustContext(c *C) {
	p := &plan.Plan{
		TrustContexts: map[string]*plan.TrustContext{"ctx": nil},
	}
	err := p.Validate()
	c.Assert(err, ErrorMatches, `trust context object cannot be null for trust context "ctx"`)
}

func (s *S) TestTrustContextReferencesMustBeDefinedInOrder(c *C) {
	// Defined in the same layer as the reference.
	_, err := combineTrustLayers(c, `
trust-contexts:
    base:
        override: replace
    ctx:
        override: replace
        include: base
services:
    srv:
        override: replace
        command: cmd
        trust-context: ctx
`)
	c.Assert(err, IsNil)

	// Defined in an earlier layer.
	_, err = combineTrustLayers(c, `
trust-contexts:
    ctx:
        override: replace
`, `
trust-contexts:
    other:
        override: replace
        include: ctx
services:
    srv:
        override: replace
        command: cmd
        trust-context: ctx
checks:
    chk:
        override: replace
        exec:
            command: cmd
            trust-context: ctx
log-targets:
    tgt:
        override: replace
        type: loki
        location: https://example.com
        services: []
        trust-context: ctx
`)
	c.Assert(err, IsNil)

	// A later merge layer that doesn't set trust-context is fine.
	_, err = combineTrustLayers(c, `
trust-contexts:
    ctx:
        override: replace
services:
    srv:
        override: replace
        command: cmd
        trust-context: ctx
`, `
services:
    srv:
        override: merge
        environment:
            FOO: bar
`)
	c.Assert(err, IsNil)

	// Defined only in a later layer.
	tests := []struct {
		summary string
		layer   string
		error   string
	}{{
		summary: "include",
		layer: `
trust-contexts:
    other:
        override: replace
        include: ctx
`,
		error: `layer "layer-0" trust context "other" includes unknown trust context "ctx"`,
	}, {
		summary: "service",
		layer: `
services:
    srv:
        override: replace
        command: cmd
        trust-context: ctx
`,
		error: `layer "layer-0" service "srv" specifies unknown trust context "ctx"`,
	}, {
		summary: "http check",
		layer: `
checks:
    chk:
        override: replace
        http:
            url: https://example.com
            trust-context: ctx
`,
		error: `layer "layer-0" check "chk" specifies unknown trust context "ctx"`,
	}, {
		summary: "exec check",
		layer: `
checks:
    chk:
        override: replace
        exec:
            command: cmd
            trust-context: ctx
`,
		error: `layer "layer-0" check "chk" specifies unknown trust context "ctx"`,
	}, {
		summary: "log target",
		layer: `
log-targets:
    tgt:
        override: replace
        type: loki
        location: https://example.com
        services: []
        trust-context: ctx
`,
		error: `layer "layer-0" log target "tgt" specifies unknown trust context "ctx"`,
	}}
	for _, test := range tests {
		c.Logf(test.summary)
		_, err := combineTrustLayers(c, test.layer, `
trust-contexts:
    ctx:
        override: replace
`)
		c.Check(err, ErrorMatches, test.error)
	}
}
