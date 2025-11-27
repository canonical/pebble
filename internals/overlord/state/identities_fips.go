//go:build fips

// Copyright (c) 2025 Canonical Ltd
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

package state

import (
	"crypto/x509"
	"errors"
)

// validateCert checks if the cert identity is valid (FIPS builds: always blocks).
func (d *Identity) validateCert() (bool, error) {
	if d.Cert != nil {
		return false, errors.New("certificate authentication is not supported in FIPS builds")
	}
	return false, nil
}

// unmarshalCert unmarshals a certificate from PEM format (FIPS builds: always blocks).
func unmarshalCert(certData *apiCertIdentity) (*CertIdentity, error) {
	return nil, errors.New("certificate authentication is not supported in FIPS builds")
}

// identityFromCert returns an identity matching the given client certificate (FIPS builds: always returns nil).
func (s *State) identityFromCert(clientCert *x509.Certificate) *Identity {
	// Certificate authentication is not supported in FIPS builds
	return nil
}

// identityFromBasicAuth returns an identity matching the given username and password (FIPS builds: always returns nil).
func (s *State) identityFromBasicAuth(username, password string) *Identity {
	// Basic authentication login is not supported in FIPS builds (password verification
	// requires the github.com/GehirnInc/crypt library which is not FIPS-certified)
	return nil
}

// noTypeError returns the error message for when no identity type is specified (FIPS builds).
func noTypeError() error {
	return errors.New(`identity must have at least one type ("local" or "basic"; cert auth not supported in FIPS builds)`)
}
