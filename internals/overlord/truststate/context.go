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
)

// TrustContext is a specific, immutable, resolved snapshot of a named trust
// context, as maintained by a TrustManager. It is safe for concurrent use.
type TrustContext struct {
	mgr  *TrustManager
	name string

	shortSha       string
	pemBundle      []byte
	pool           *x509.CertPool
	isSystemPool   bool
	includesSystem bool
}

// IsSystemCA returns true if the trust context's effective CA certificates
// are equivalent to the system CA pool.
func (t *TrustContext) IsSystemCA() bool {
	return t.isSystemPool
}

// Version returns the version for the trust context. Between two versions of a
// trust context of the same name, this value will be different.
func (t *TrustContext) Version() string {
	return t.shortSha
}

// CAPool returns a certificate pool containing all of the CA certificates
// trusted by this trust context, including those pulled in transitively via
// "include". The returned pool is a fresh copy on each call, so it's safe
// for the caller to hold on to and use (for example, as tls.Config.RootCAs)
// without it changing underneath them or being affected by later plan
// changes.
func (t *TrustContext) CAPool() *x509.CertPool {
	return t.pool.Clone()
}

// CABundleFile returns the path of a PEM file on disk containing the CA
// certificates trusted by the named trust context. The file is created lazily
// on first use at "<trust dir>/<name>/x509-ca.pem". Once created, it is kept
// up to date with the latest resolved version of the trust context.
func (t *TrustContext) CABundleFile() (string, error) {
	return t.mgr.ensureFile(t)
}
