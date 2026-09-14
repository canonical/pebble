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

	"github.com/canonical/pebble/internals/logger"
)

// loadSystemCAs loads the pool backing the "system" trust context via the
// x509 package, and (on a best-effort, platform-specific basis) the same
// trust anchors as raw PEM data, for use when a file-based CA bundle needs
// to include the system trust anchors. The returned pool is never nil; the
// returned PEM data may be nil if it can't be located (or reconstructed) on
// this platform.
//
// The x509 package doesn't provide a way to enumerate the certificates that
// make up a *x509.CertPool (which is required to build a CA bundle *file*,
// as opposed to just an in-memory pool), so loadReplicaSystemCABundle
// (platform-specific) independently locates and reads the same underlying
// CA certificate data that x509.SystemCertPool uses. Since this duplicates
// logic from the standard library, and could conceivably drift or disagree
// with it (for example, if this file needs updating to track a Go release,
// or the underlying platform's trust store changed shape), the resulting
// pool is compared against the real system pool via CertPool.Equal, and a
// warning is logged if they don't match.
func loadSystemCAs() (*x509.CertPool, []byte) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		logger.Noticef("Cannot load system CA certificate pool: %v", err)
		pool = x509.NewCertPool()
	}

	replicaPool, pemBytes := loadSystemCABundle()
	if replicaPool == nil {
		logger.Debugf(`Cannot locate a system CA certificate bundle on disk; ` +
			`file-based CA bundles that include the "system" trust context ` +
			`will not contain the system trust anchors`)
		return pool, nil
	}

	if !pool.Equal(replicaPool) {
		logger.Noticef(`The system CA certificates read from disk do not match ` +
			`the standard library's system certificate pool; file-based CA ` +
			`bundles that include the "system" trust context may not exactly ` +
			`match what Go HTTP clients trust`)
	}

	return pool, pemBytes
}
