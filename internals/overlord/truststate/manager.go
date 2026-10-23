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
	"encoding/pem"
	"fmt"
	"os"
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
	// CA bundle files can't be created for trust contexts that include
	// "system" (though their in-memory pools are still available).
	systemPEM []byte

	// fileMu serialises writing and removing CA bundle files, and guards
	// written. When both are needed, fileMu must be acquired before mu.
	fileMu sync.Mutex
	// written maps a trust context name to the shortSha of the version whose
	// CA bundle file is currently on disk.
	written map[string]string

	mu sync.Mutex
	// current holds the latest resolved version for every trust context
	// name currently known to the manager (built-ins plus anything declared
	// in the plan).
	current map[string]*trustContextVersion
	// refs counts the outstanding TrustContext references for each trust
	// context name, across all versions.
	refs map[string]int
}

// NewManager creates a new TrustManager which maintains CA bundle files
// under trustDir (which will be created on demand). The built-in "system"
// and "default" trust contexts are available immediately, even before the
// first call to PlanChanged.
func NewManager(trustDir string) *TrustManager {
	m := &TrustManager{
		trustDir: trustDir,
		written:  make(map[string]string),
		current:  make(map[string]*trustContextVersion),
		refs:     make(map[string]int),
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
// the CA pool and PEM bundle available to consumers. CA bundle files that
// have already been written are rewritten with the new content, and those
// belonging to trust contexts removed from the plan are removed once they
// are no longer referenced.
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

	m.fileMu.Lock()
	defer m.fileMu.Unlock()

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
			// Nothing of substance changed, keep the existing version.
			newCurrent[r.name] = old
			continue
		}
		newCurrent[r.name] = &trustContextVersion{
			mgr:            m,
			name:           r.name,
			shortSha:       r.data.shortSha,
			pemBundle:      r.data.pemBundle,
			pool:           r.data.pool,
			isSystemPool:   r.data.isSystemPool,
			includesSystem: r.data.includesSystem,
		}
	}
	m.current = newCurrent
	var rewrite []*trustContextVersion
	var remove []string
	for name, sha := range m.written {
		v, ok := m.current[name]
		switch {
		case !ok && m.refs[name] == 0:
			remove = append(remove, name)
		case ok && v.shortSha != sha:
			rewrite = append(rewrite, v)
		}
	}
	m.mu.Unlock()

	for _, v := range rewrite {
		if err := m.writeFileLocked(v); err != nil {
			logger.Noticef("Cannot update CA bundle file: %v", err)
		}
	}
	for _, name := range remove {
		m.removeFileLocked(name)
	}
}

// TrustContext returns a reference to the current resolved state of the
// named trust context. The caller must call Close on the returned
// TrustContext once it is no longer needed. If passed a trust context name that
// is an empty string, the default trust context is returned.
func (m *TrustManager) TrustContext(name string) (*TrustContext, error) {
	if name == "" {
		name = DefaultTrustContext
	}
	m.mu.Lock()
	v, ok := m.current[name]
	if ok {
		m.refs[name]++
	}
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("trust context %q not found", name)
	}
	return &TrustContext{version: v}, nil
}

// release drops a reference previously obtained via TrustContext, removing
// the trust context's CA bundle file if it has been removed from the plan and
// this was the last outstanding reference.
func (m *TrustManager) release(name string) {
	m.mu.Lock()
	m.refs[name]--
	unused := m.refs[name] <= 0
	if unused {
		delete(m.refs, name)
	}
	_, current := m.current[name]
	m.mu.Unlock()
	if unused && !current {
		m.fileMu.Lock()
		m.removeFileLocked(name)
		m.fileMu.Unlock()
	}
}

// ensureFile makes sure the CA bundle file for the trust context that v
// belongs to exists and holds the latest resolved version of that trust
// context, and returns its path.
func (m *TrustManager) ensureFile(v *trustContextVersion) (string, error) {
	path, err := bundleFilePath(m.trustDir, v.name)
	if err != nil {
		return "", err
	}
	m.fileMu.Lock()
	defer m.fileMu.Unlock()
	m.mu.Lock()
	latest, ok := m.current[v.name]
	m.mu.Unlock()
	if !ok {
		// The trust context has been removed from the plan, but v is still
		// referenced. Serve the file as it was last written (if it was).
		latest = v
		if _, written := m.written[v.name]; written {
			return path, nil
		}
	}
	if m.written[latest.name] == latest.shortSha {
		return path, nil
	}
	if err := m.writeFileLocked(latest); err != nil {
		return "", err
	}
	return path, nil
}

// writeFileLocked writes the CA bundle file for v. It must be called while
// holding fileMu.
func (m *TrustManager) writeFileLocked(v *trustContextVersion) error {
	path, err := bundleFilePath(m.trustDir, v.name)
	if err != nil {
		return err
	}
	if v.includesSystem && len(m.systemPEM) == 0 {
		// The system CA pool is available in memory, but the certificates
		// can't be written to a file, which would silently drop them.
		return fmt.Errorf("cannot write CA bundle file for trust context %q: "+
			"it includes %q, but no system CA bundle was found on disk", v.name, SystemTrustContext)
	}
	err = osutil.Mkdir(filepath.Dir(path), 0o755, &osutil.MkdirOptions{
		MakeParents: true,
		ExistOK:     true,
		Chmod:       true,
	})
	if err != nil {
		return fmt.Errorf("cannot create trust directory: %w", err)
	}
	if err := osutil.AtomicWriteFile(path, v.pemBundle, 0o644, 0); err != nil {
		return fmt.Errorf("cannot write CA bundle file for trust context %q: %w", v.name, err)
	}
	m.written[v.name] = v.shortSha
	return nil
}

// removeFileLocked removes the CA bundle file (and its directory) for the
// named trust context, unless the trust context is (again) current or still
// referenced. It must be called while holding fileMu.
func (m *TrustManager) removeFileLocked(name string) {
	m.mu.Lock()
	_, current := m.current[name]
	inUse := m.refs[name] > 0
	m.mu.Unlock()
	if current || inUse {
		return
	}
	if _, ok := m.written[name]; !ok {
		return
	}
	delete(m.written, name)
	path, err := bundleFilePath(m.trustDir, name)
	if err != nil {
		logger.Noticef("Cannot remove stale CA bundle file: %v", err)
		return
	}
	// Remove only the file and its (then empty) directory, never recursively.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		logger.Noticef("Cannot remove stale CA bundle file for trust context %q: %v", name, err)
		return
	}
	if err := os.Remove(filepath.Dir(path)); err != nil && !os.IsNotExist(err) {
		logger.Noticef("Cannot remove stale CA bundle directory for trust context %q: %v", name, err)
	}
}

// resolvedTrust holds the trust data for a single trust context.
type resolvedTrust struct {
	pemBundle      []byte
	pool           *x509.CertPool
	isSystemPool   bool
	includesSystem bool
	shortSha       string
}

// resolve computes the fully-resolved CA pool and PEM bundle for the named
// trust context. The certificates of each included trust context (in order,
// recursively) come first, followed by the trust context's own certificates.
// Duplicate certificates are dropped, retaining the first. Cycles are broken
// by tracking which trust contexts have already been visited: since
// "include" only ever adds to the resulting set of trusted CAs, revisiting an
// already-included trust context can't change the result, so it's simply
// skipped.
func (m *TrustManager) resolve(name string, pl *plan.Plan) (*resolvedTrust, error) {
	visited := make(map[string]bool)
	includesSystem := false
	// parts holds the trust contexts' CA certificate data in order, where a
	// nil entry marks the position of the system CA certificates.
	var parts [][]byte

	var visit func(cur string) error
	visit = func(cur string) error {
		if visited[cur] {
			return nil
		}
		visited[cur] = true

		if cur == SystemTrustContext {
			includesSystem = true
			parts = append(parts, nil)
			return nil
		}

		ctx, ok := lookupTrustContext(cur, pl)
		if !ok {
			return fmt.Errorf("includes unknown trust context %q", cur)
		}
		for _, include := range ctx.Include {
			if err := visit(include); err != nil {
				return err
			}
		}
		if ctx.X509 != nil && len(ctx.X509.CACert) > 0 {
			parts = append(parts, []byte(ctx.X509.CACert))
		}
		return nil
	}
	if err := visit(name); err != nil {
		return nil, err
	}

	var pool *x509.CertPool
	if includesSystem {
		pool = m.systemPool.Clone()
	} else {
		pool = x509.NewCertPool()
	}
	var bundle bytes.Buffer
	seen := make(map[[sha256.Size]byte]bool)
	for _, part := range parts {
		if part == nil {
			// The system CA certificates are already in the pool, but
			// still need adding to the bundle (and seen).
			appendCerts(&bundle, nil, seen, m.systemPEM)
			continue
		}
		if appendCerts(&bundle, pool, seen, part) == 0 {
			return nil, fmt.Errorf("no valid CA certificate found for trust context %q", name)
		}
	}

	pemBundle := bundle.Bytes()
	h := sha256.New()
	if includesSystem {
		// The system CA pool may not be (fully) represented in the bundle.
		h.Write([]byte("system\n"))
	}
	h.Write(pemBundle)
	shortSha := hex.EncodeToString(h.Sum(nil))[:8]

	return &resolvedTrust{
		pemBundle:      pemBundle,
		pool:           pool,
		isSystemPool:   pool.Equal(m.systemPool),
		includesSystem: includesSystem,
		shortSha:       shortSha,
	}, nil
}

// appendCerts parses the PEM encoded certificates in data, and appends each
// one not yet seen to buf (and pool, if not nil). It returns the number of
// valid certificates found in data, including duplicates.
func appendCerts(buf *bytes.Buffer, pool *x509.CertPool, seen map[[sha256.Size]byte]bool, data []byte) int {
	count := 0
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			return count
		}
		if block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		count++
		sum := sha256.Sum256(block.Bytes)
		if seen[sum] {
			continue
		}
		seen[sum] = true
		if pool != nil {
			pool.AddCert(cert)
		}
		pem.Encode(buf, block)
	}
}

// lookupTrustContext returns the trust context configuration for name. When
// "default" isn't declared in the plan, it implicitly includes "system".
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
// named trust context. It returns an error if name isn't a valid trust context
// name, which guarantees the path stays inside trustDir.
func bundleFilePath(trustDir, name string) (string, error) {
	if !plan.ValidTrustContextName(name) {
		return "", fmt.Errorf("cannot use invalid trust context name %q", name)
	}
	return filepath.Join(trustDir, name, "x509-ca.pem"), nil
}
