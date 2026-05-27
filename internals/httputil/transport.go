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

//go:build linux

package httputil

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/canonical/pebble/internals/logger"
)

// Transport is a lazy-loading, refreshable http.RoundTripper. The
// system root cert pool is loaded on first use via x509.SystemCertPool.
// Call Refresh to reload cert pool entries from disk, logging any
// additions or removals.
type Transport struct {
	mu           sync.RWMutex
	transport    *http.Transport
	trackedCerts map[[32]byte]string // fingerprint -> subject string
	fileStates   []certFileState
}

type certFileState struct {
	path    string
	modTime time.Time
	size    int64
}

// NewTransport creates a Transport. No cert loading is done until the
// transport is first used via RoundTrip.
func NewTransport() *Transport {
	return &Transport{}
}

// RoundTrip implements http.RoundTripper. It lazily loads the system
// cert pool on the first call.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	err := t.lazyInit()
	if err != nil {
		return nil, fmt.Errorf("cannot initialise transport: %w", err)
	}
	t.mu.RLock()
	transport := t.transport
	t.mu.RUnlock()
	return transport.RoundTrip(req)
}

// Refresh reloads cert files from disk and compares against the
// currently tracked cert set. Added and removed certificates are
// logged via logger.Noticef. If the transport has not yet been used,
// Refresh is a no-op. If no cert files have changed on disk since the
// last Refresh, Refresh is also a no-op.
func (t *Transport) Refresh() error {
	t.mu.RLock()
	initialized := t.transport != nil
	t.mu.RUnlock()
	if !initialized {
		return nil
	}
	if !t.certFilesChanged() {
		return nil
	}
	certs, err := loadSystemCerts()
	if err != nil {
		return fmt.Errorf("cannot load system certs: %w", err)
	}
	newTracked := make(map[[32]byte]string, len(certs))
	for _, c := range certs {
		fp := sha256.Sum256(c.Raw)
		newTracked[fp] = c.Subject.String()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	changed := false
	for fp, subject := range newTracked {
		if _, ok := t.trackedCerts[fp]; !ok {
			logger.Noticef(
				"Certificate %q (%s) added to root cert pool.",
				subject, formatFingerprint(fp),
			)
			changed = true
		}
	}
	for fp, subject := range t.trackedCerts {
		if _, ok := newTracked[fp]; !ok {
			logger.Noticef(
				"Certificate %q (%s) removed from root cert pool.",
				subject, formatFingerprint(fp),
			)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	pool := x509.NewCertPool()
	for _, c := range certs {
		pool.AddCert(c)
	}
	old := t.transport
	t.transport = buildTransport(pool)
	t.trackedCerts = newTracked
	t.fileStates = currentFileStates()
	old.CloseIdleConnections()
	return nil
}

// lazyInit loads the system cert pool on first use.
func (t *Transport) lazyInit() error {
	t.mu.RLock()
	initialized := t.transport != nil
	t.mu.RUnlock()
	if initialized {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.transport != nil {
		return nil
	}
	// Use the stdlib's memoized pool for the initial load.
	pool, err := x509.SystemCertPool()
	if err != nil {
		return fmt.Errorf("cannot load system cert pool: %w", err)
	}
	// Load individual certs from disk to establish the initial tracked set.
	certs, certsErr := loadSystemCerts()
	if certsErr != nil {
		logger.Noticef("Cannot track system cert pool: %v", certsErr)
	}
	tracked := make(map[[32]byte]string, len(certs))
	for _, c := range certs {
		fp := sha256.Sum256(c.Raw)
		tracked[fp] = c.Subject.String()
	}
	t.transport = buildTransport(pool)
	t.trackedCerts = tracked
	t.fileStates = currentFileStates()
	return nil
}

// certFilesChanged returns true if any cert file's mtime or size has
// changed since the last check.
func (t *Transport) certFilesChanged() bool {
	t.mu.RLock()
	prev := t.fileStates
	t.mu.RUnlock()
	curr := currentFileStates()
	if len(prev) != len(curr) {
		return true
	}
	for i, p := range prev {
		c := curr[i]
		if p.path != c.path || p.modTime != c.modTime || p.size != c.size {
			return true
		}
	}
	return false
}

// currentFileStates returns the current mtime and size for each cert
// file and directory that exists, following the same discovery order
// as loadSystemRoots.
func currentFileStates() []certFileState {
	var states []certFileState
	seen := make(map[string]bool)
	files := certFiles
	if f := os.Getenv(certFileEnv); f != "" {
		files = []string{f}
	}
	for _, path := range files {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if !seen[path] {
			seen[path] = true
			states = append(states, certFileState{
				path:    path,
				modTime: info.ModTime(),
				size:    info.Size(),
			})
		}
		break // certFiles: stop after finding first
	}
	dirs := certDirectories
	if d := os.Getenv(certDirEnv); d != "" {
		dirs = strings.Split(d, ":")
	}
	for _, dir := range dirs {
		dinfo, err := os.Stat(dir)
		if err != nil {
			continue
		}
		if !seen[dir] {
			seen[dir] = true
			states = append(states, certFileState{
				path:    dir,
				modTime: dinfo.ModTime(),
				size:    dinfo.Size(),
			})
		}
		fis, err := readUniqueDirectoryEntries(dir)
		if err != nil {
			continue
		}
		for _, fi := range fis {
			path := dir + "/" + fi.Name()
			info, err := fi.Info()
			if err != nil {
				continue
			}
			if !seen[path] {
				seen[path] = true
				states = append(states, certFileState{
					path:    path,
					modTime: info.ModTime(),
					size:    info.Size(),
				})
			}
		}
	}
	return states
}

// loadSystemCerts reads individual x509 certificates from the system
// cert files, following the same discovery logic as loadSystemRoots.
func loadSystemCerts() ([]*x509.Certificate, error) {
	var certs []*x509.Certificate
	files := certFiles
	if f := os.Getenv(certFileEnv); f != "" {
		files = []string{f}
	}
	var firstErr error
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) && firstErr == nil {
				firstErr = err
			}
			continue
		}
		certs = append(certs, parsePEMCerts(data)...)
		break
	}
	dirs := certDirectories
	if d := os.Getenv(certDirEnv); d != "" {
		dirs = strings.Split(d, ":")
	}
	for _, dir := range dirs {
		fis, err := readUniqueDirectoryEntries(dir)
		if err != nil {
			if !os.IsNotExist(err) && firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, fi := range fis {
			data, err := os.ReadFile(dir + "/" + fi.Name())
			if err == nil {
				certs = append(certs, parsePEMCerts(data)...)
			}
		}
	}
	if len(certs) > 0 || firstErr == nil {
		return certs, nil
	}
	return nil, firstErr
}

// parsePEMCerts parses all PEM-encoded certificates from data.
func parsePEMCerts(data []byte) []*x509.Certificate {
	var certs []*x509.Certificate
	for len(data) > 0 {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			continue
		}
		certs = append(certs, cert)
	}
	return certs
}

// buildTransport creates a new *http.Transport with the given cert
// pool, cloning DefaultTransport's connection settings.
func buildTransport(pool *x509.CertPool) *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig = &tls.Config{RootCAs: pool}
	return t
}

// formatFingerprint formats a SHA-256 fingerprint as a
// colon-separated hex string.
func formatFingerprint(fp [32]byte) string {
	var sb strings.Builder
	for i, b := range fp {
		if i > 0 {
			sb.WriteByte(':')
		}
		fmt.Fprintf(&sb, "%02x", b)
	}
	return sb.String()
}
