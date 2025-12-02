//go:build fips

// Copyright (c) 2014-2020 Canonical Ltd
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

package client

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
)

// verifyConnection is not supported in FIPS builds (HTTPS is blocked).
func verifyConnection(state tls.ConnectionState, opts *Config) error {
	return errors.New("TLS connections are not supported in FIPS builds")
}

// getIdentityFingerprint is not supported in FIPS builds (HTTPS is blocked).
func getIdentityFingerprint(cert *x509.Certificate) (string, error) {
	return "", errors.New("certificate operations are not supported in FIPS builds")
}
