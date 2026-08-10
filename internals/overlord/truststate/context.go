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
	"fmt"
	"os"
	"sync"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/osutil"
)

// ErrClosed is returned by TrustContext methods once the TrustContext has
// been closed.
var ErrClosed = errors.New("trust context reference already closed")

// TrustContext is a reference to a specific, immutable, resolved snapshot of
// a named trust context, as maintained by a TrustManager. Once the caller no
// longer needs it, Release must be called so the manager can reclaim
// resources (such as an on-disk CA bundle file) once a newer version of the
// trust context has superseded this one (or it has been removed from the
// plan).
//
// TrustContext is safe for concurrent use, but is not safe for use after
// Release has been called.
type TrustContext struct {
	mu      sync.Mutex
	version *trustContextVersion
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

// CABundleFile returns the path of a PEM file on disk containing all of the
// CA certificates trusted by this trust context. The file is created lazily
// on first use, and is guaranteed to exist and remain unchanged for as long
// as this (or any other reference to the same resolved version) has not
// been released.
func (t *TrustContext) CABundleFile() (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.version == nil {
		return "", ErrClosed
	}
	return t.version.ensureFile()
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
		v.release()
	}
	return nil
}

// trustContextVersion holds one immutable, fully-resolved snapshot of a
// named trust context, as computed by TrustManager.resolve. A new version is
// only created when the resolved CA data actually changes (tracked via
// shortSha); as long as the content is unchanged across plan changes, the
// same version (and any outstanding references and on-disk file) continues
// to be used.
type trustContextVersion struct {
	mgr  *TrustManager
	name string

	shortSha  string
	pemBundle []byte
	pool      *x509.CertPool
	filePath  string

	// refCount, superseded and cleaned are all only ever accessed while
	// holding mgr.mu.
	refCount   int
	superseded bool
	cleaned    bool

	// fileMu guards lazy creation of the CA bundle file.
	fileMu      sync.Mutex
	fileWritten bool
}

// addRef must be called while holding mgr.mu.
func (v *trustContextVersion) addRef() {
	v.refCount++
}

// release drops a reference previously obtained via addRef, cleaning up the
// version's on-disk state if it has been superseded and this was the last
// outstanding reference.
func (v *trustContextVersion) release() {
	v.mgr.mu.Lock()
	v.refCount--
	doCleanup := v.maybeCleanupLocked()
	v.mgr.mu.Unlock()
	if doCleanup {
		v.cleanup()
	}
}

// maybeCleanupLocked must be called while holding mgr.mu. It returns true
// (at most once, ever, for a given version) when the caller has become
// responsible for removing this version's on-disk state.
func (v *trustContextVersion) maybeCleanupLocked() bool {
	if v.cleaned || !v.superseded || v.refCount > 0 {
		return false
	}
	v.cleaned = true
	return true
}

// ensureFile lazily writes the CA bundle file for this version to disk, if
// it hasn't been already, and returns its path.
func (v *trustContextVersion) ensureFile() (string, error) {
	v.fileMu.Lock()
	defer v.fileMu.Unlock()
	if v.fileWritten {
		return v.filePath, nil
	}
	if err := v.mgr.ensureTrustDir(); err != nil {
		return "", fmt.Errorf("cannot create trust directory: %w", err)
	}
	if err := osutil.AtomicWriteFile(v.filePath, v.pemBundle, 0o644, 0); err != nil {
		return "", fmt.Errorf("cannot write CA bundle file for trust context %q: %w", v.name, err)
	}
	v.fileWritten = true
	return v.filePath, nil
}

// cleanup removes this version's CA bundle file from disk, if it was ever
// written.
func (v *trustContextVersion) cleanup() {
	v.fileMu.Lock()
	written := v.fileWritten
	v.fileMu.Unlock()
	if !written {
		return
	}
	if err := os.Remove(v.filePath); err != nil && !os.IsNotExist(err) {
		logger.Noticef("Cannot remove stale CA bundle file %q: %v", v.filePath, err)
	}
}
