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

package truststate

import (
	"crypto/x509"
	"errors"
	"sync"
)

// ErrClosed is returned by TrustContext methods once the TrustContext has
// been closed.
var ErrClosed = errors.New("trust context reference already closed")

// TrustContext is a reference to a specific, immutable, resolved snapshot of
// a named trust context, as maintained by a TrustManager. Once the caller no
// longer needs it, Close must be called so the manager can reclaim resources
// (such as the on-disk CA bundle file) once the trust context has been
// removed from the plan.
//
// TrustContext is safe for concurrent use, but is not safe for use after
// Close has been called.
type TrustContext struct {
	mu      sync.Mutex
	version *trustContextVersion
}

// IsSystemCA returns true if the trust context's effective CA certificates
// are equivalent to the system CA pool.
func (t *TrustContext) IsSystemCA() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.version == nil {
		return false
	}
	return t.version.isSystemPool
}

// Version returns the version for the trust context. Between two versions of a
// trust context of the same name, this value will be different.
func (t *TrustContext) Version() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.version == nil {
		return ""
	}
	return t.version.shortSha
}

// CAPool returns a certificate pool containing all of the CA certificates
// trusted by this trust context, including those pulled in transitively via
// "include". The returned pool is a fresh copy on each call, so it's safe
// for the caller to hold on to and use (for example, as tls.Config.RootCAs)
// without it changing underneath them or being affected by later plan
// changes.
func (t *TrustContext) CAPool() (*x509.CertPool, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.version == nil {
		return nil, ErrClosed
	}
	return t.version.pool.Clone(), nil
}

// CABundleFile returns the path of a PEM file on disk containing the CA
// certificates trusted by the named trust context. The file is created lazily
// on first use at "<trust dir>/<name>/x509-ca.pem". Once created, it is kept
// up to date with the latest resolved version of the trust context, and it
// remains on disk until the trust context has been removed from the plan and
// all references to it have been closed.
func (t *TrustContext) CABundleFile() (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.version == nil {
		return "", ErrClosed
	}
	return t.version.mgr.ensureFile(t.version)
}

// Close tells the TrustManager that this reference is no longer needed.
// It is safe to call Close more than once; calls after the first are a
// no-op.
func (t *TrustContext) Close() error {
	t.mu.Lock()
	v := t.version
	t.version = nil
	t.mu.Unlock()
	if v != nil {
		v.mgr.release(v.name)
	}
	return nil
}

// trustContextVersion holds one immutable, fully-resolved snapshot of a
// named trust context, as computed by TrustManager.resolve. A new version is
// only created when the resolved CA data actually changes (tracked via
// shortSha).
type trustContextVersion struct {
	mgr  *TrustManager
	name string

	shortSha       string
	pemBundle      []byte
	pool           *x509.CertPool
	isSystemPool   bool
	includesSystem bool
}
