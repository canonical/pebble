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

// Package truststate manages the trust contexts declared in the Pebble
// plan. A trust context is a named collection of trusted x509 CA
// certificates (optionally built up from other trust contexts via
// "include") that can be consumed by services, checks and log targets to
// establish trust with an exogenous entity.
//
// The manager maintains two built-in trust contexts in addition to any
// declared in the plan:
//
//   - "system" is an immutable trust context backed by the host's default
//     x509 CA certificate pool (as loaded by the standard library).
//   - "default" is the trust context consumers use when none is explicitly
//     configured. It is mutable (via the plan), but may only "include"
//     other trust contexts; it can never define its own CA certificate
//     directly.
package truststate

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/osutil"
	"github.com/canonical/pebble/internals/plan"
)

const (
	// SystemTrustContext is the name of the built-in, immutable trust
	// context backed by the host's default CA certificate pool.
	SystemTrustContext = "system"

	// DefaultTrustContext is the name of the built-in trust context used by
	// default by trust context consumers (services, checks, log targets).
	DefaultTrustContext = "default"
)

// TrustManager loads and maintains the CA certificate data for every trust
// context declared in the plan (plus the "system" and "default" built-ins),
// and provides access to it, both as an in-memory x509.CertPool (for Go
// clients) and as a maintained PEM file on disk (for consumers that require
// a file path, such as exec or external processes).
type TrustManager struct {
	// trustDir is the directory in which CA bundle files are maintained,
	// normally "$PEBBLE/trust".
	trustDir string

	// systemPool is the CA certificate pool backing the "system" trust
	// context, loaded once via the x509 package.
	systemPool *x509.CertPool
	// systemPEM is a best-effort PEM encoded representation of the same
	// trust roots as systemPool, used only when a CA bundle *file* that
	// includes the system trust anchors is required. The x509 package does
	// not expose a way to enumerate the certificates that make up a
	// *x509.CertPool (in particular, one obtained via SystemCertPool), so
	// this is populated by reading one of a handful of well-known CA bundle
	// locations on disk. It may be nil if no such file can be found (for
	// example, in minimal container images, or on platforms where the
	// system trust store isn't backed by a single PEM file), in which case
	// file-based bundles won't include the system trust anchors even though
	// the in-memory pool always will.
	systemPEM []byte

	mu sync.Mutex
	// current holds the latest resolved version for every trust context
	// name currently known to the manager (built-ins plus anything declared
	// in the plan).
	current map[string]*trustContextVersion
}

// NewManager creates a new TrustManager which maintains CA bundle files
// under trustDir (which will be created on demand). The built-in "system"
// and "default" trust contexts are available immediately, even before the
// first call to PlanChanged.
func NewManager(trustDir string) *TrustManager {
	m := &TrustManager{
		trustDir: trustDir,
		current:  make(map[string]*trustContextVersion),
	}
	m.systemPool, m.systemPEM = loadSystemCAs()

	// Bootstrap the built-in trust contexts right away, so callers don't
	// have to wait for a real plan to be loaded to use "system" or
	// "default".
	m.PlanChanged(plan.NewPlan())
	return m
}

// Ensure implements overlord.StateManager. All of the TrustManager's work is
// performed synchronously (from PlanChanged, and lazily when a trust
// context's CABundleFile is first requested), so there's nothing to do here.
func (m *TrustManager) Ensure() error {
	return nil
}

// PlanChanged is called (normally registered as a plan change listener)
// whenever the plan changes. It re-resolves every trust context declared in
// the new plan (as well as the "system" and "default" built-ins), updating
// the CA pool and PEM bundle available to consumers.
//
// If a trust context can't be resolved (for example, because it includes an
// unknown trust context), an error is logged and the trust context's
// previous (last known good) resolved state, if any, is retained.
func (m *TrustManager) PlanChanged(pl *plan.Plan) {
	keep := make(map[string]bool)
	keep[SystemTrustContext] = true
	keep[DefaultTrustContext] = true
	for name := range pl.TrustContexts {
		keep[name] = true
	}

	type resolution struct {
		name string
		data *resolvedTrust
		err  error
	}
	resolutions := make([]resolution, 0, len(keep))
	for name := range keep {
		data, err := m.resolve(name, pl)
		resolutions = append(resolutions, resolution{name: name, data: data, err: err})
	}

	var cleanup []*trustContextVersion

	m.mu.Lock()
	newCurrent := make(map[string]*trustContextVersion, len(keep))
	for _, r := range resolutions {
		old := m.current[r.name]
		if r.err != nil {
			logger.Noticef("Cannot resolve trust context %q: %v", r.name, r.err)
			if old != nil {
				// Keep serving the previous good state.
				newCurrent[r.name] = old
			}
			continue
		}
		if old != nil && old.shortSha == r.data.shortSha {
			// Nothing of substance changed, keep the existing version
			// (and its references, and its file on disk) as-is.
			newCurrent[r.name] = old
			continue
		}
		v := &trustContextVersion{
			mgr:          m,
			name:         r.name,
			shortSha:     r.data.shortSha,
			pemBundle:    r.data.pemBundle,
			pool:         r.data.pool,
			isSystemPool: r.data.isSystemPool,
			filePath:     bundleFilePath(m.trustDir, r.name, r.data.shortSha),
		}
		newCurrent[r.name] = v
		if old != nil {
			old.superseded = true
			if old.maybeCleanupLocked() {
				cleanup = append(cleanup, old)
			}
		}
	}
	// Trust contexts that used to exist but are no longer declared anywhere
	// (removed from the plan).
	for name, old := range m.current {
		if keep[name] {
			continue
		}
		old.superseded = true
		if old.maybeCleanupLocked() {
			cleanup = append(cleanup, old)
		}
	}
	m.current = newCurrent
	m.mu.Unlock()

	for _, v := range cleanup {
		v.cleanup()
	}
}

// TrustContext returns a reference to the current resolved state of the
// named trust context. The caller must call Close on the returned
// TrustContext once it is no longer needed.
func (m *TrustManager) TrustContext(name string) (*TrustContext, error) {
	m.mu.Lock()
	v, ok := m.current[name]
	if ok {
		v.addRef()
	}
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("trust context %q not found", name)
	}
	return &TrustContext{version: v}, nil
}

func (m *TrustManager) ensureTrustDir() error {
	return osutil.Mkdir(m.trustDir, 0o755, &osutil.MkdirOptions{
		MakeParents: true,
		ExistOK:     true,
		Chmod:       true,
	})
}

// resolvedTrust holds the trust data for a single trust context.
type resolvedTrust struct {
	pemBundle    []byte
	pool         *x509.CertPool
	isSystemPool bool
	shortSha     string
}

// resolve computes the fully-resolved CA pool and PEM bundle for the named
// trust context, following "include" chains (including through the
// synthesized "default" trust context). Cycles are broken by tracking which
// trust contexts have already been visited: since "include" only ever adds
// to the resulting set of trusted CAs, revisiting an already-included trust
// context can't change the result, so it's simply skipped.
func (m *TrustManager) resolve(name string, pl *plan.Plan) (*resolvedTrust, error) {
	visited := make(map[string]bool)
	queue := []string{name}
	includesSystem := false
	var pemParts [][]byte

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if visited[cur] {
			continue
		}
		visited[cur] = true

		if cur == SystemTrustContext {
			includesSystem = true
			continue
		}

		ctx, ok := lookupTrustContext(cur, pl)
		if !ok {
			return nil, fmt.Errorf("includes unknown trust context %q", cur)
		}
		if ctx.TLS != nil && len(ctx.TLS.CACert) > 0 {
			pemParts = append(pemParts, []byte(ctx.TLS.CACert))
		}
		queue = append(queue, ctx.Include...)
	}

	var pool *x509.CertPool
	var bundle bytes.Buffer
	if includesSystem {
		pool = m.systemPool.Clone()
		writePEMPart(&bundle, m.systemPEM)
	} else {
		pool = x509.NewCertPool()
	}

	for _, part := range pemParts {
		if !pool.AppendCertsFromPEM(part) {
			return nil, fmt.Errorf("no valid CA certificate found for trust context %q", name)
		}
		writePEMPart(&bundle, part)
	}

	pemBundle := bundle.Bytes()
	sum := sha256.Sum256(pemBundle)
	shortSha := hex.EncodeToString(sum[:])[:8]

	return &resolvedTrust{
		pemBundle:    pemBundle,
		pool:         pool,
		isSystemPool: includesSystem && len(pemParts) == 0,
		shortSha:     shortSha,
	}, nil
}

// writePEMPart appends data to buf, ensuring it's separated from any
// subsequent content by a newline.
func writePEMPart(buf *bytes.Buffer, data []byte) {
	if len(data) == 0 {
		return
	}
	buf.Write(data)
	if data[len(data)-1] != '\n' {
		buf.WriteByte('\n')
	}
}

// lookupTrustContext returns the trust context configuration for name.
func lookupTrustContext(name string, pl *plan.Plan) (*plan.TrustContext, bool) {
	ctx, ok := pl.TrustContexts[name]
	if !ok && name == DefaultTrustContext {
		return &plan.TrustContext{
			Name:     DefaultTrustContext,
			Override: plan.ReplaceOverride,
			Include:  []string{SystemTrustContext},
		}, true
	}
	return ctx, ok
}

// bundleFilePath returns the path of the maintained CA bundle file for the
// named trust context's given content hash.
func bundleFilePath(trustDir, name, shortSha string) string {
	return filepath.Join(trustDir, fmt.Sprintf("%s-%s-tls-ca-bundle.pem", name, shortSha))
}
