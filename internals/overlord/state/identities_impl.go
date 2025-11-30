//go:build !fips

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
	"encoding/pem"
	"errors"
	"fmt"

	"github.com/GehirnInc/crypt/sha512_crypt"
)

// validateCert checks if the cert identity is valid.
func (d *Identity) validateCert() (bool, error) {
	if d.Cert != nil {
		if d.Cert.X509 == nil {
			return false, errors.New("cert identity must include an X.509 certificate")
		}
		return true, nil
	}
	return false, nil
}

// unmarshalCert unmarshals a certificate from PEM format.
func unmarshalCert(certData *apiCertIdentity) (*CertIdentity, error) {
	block, rest := pem.Decode([]byte(certData.PEM))
	if block == nil {
		return nil, errors.New("cert identity must include a PEM-encoded certificate")
	}
	if len(rest) > 0 {
		return nil, errors.New("cert identity cannot have extra data after the PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("cannot parse certificate from cert identity: %w", err)
	}
	return &CertIdentity{X509: cert}, nil
}

// identityFromCert returns an identity matching the given client certificate.
func (s *State) identityFromCert(clientCert *x509.Certificate) *Identity {
	for _, identity := range s.identities {
		if identity.Cert != nil && identity.Cert.X509.Equal(clientCert) {
			// Certificate identities can be added manually, so we still need to verify
			// this was a self-signed client identity certificate without intermediaries.
			roots := x509.NewCertPool()
			roots.AddCert(identity.Cert.X509)
			opts := x509.VerifyOptions{
				Roots: roots,
				// We only support verifying client TLS certificates.
				KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
			}
			_, err := clientCert.Verify(opts)
			if err == nil {
				return identity
			}
		}
	}
	// If a client certificate is provided, but did not match, we bail.
	return nil
}

// identityFromBasicAuth returns an identity matching the given username and password.
func (s *State) identityFromBasicAuth(username, password string) *Identity {
	passwordBytes := []byte(password)
	for _, identity := range s.identities {
		if identity.Basic == nil || identity.Name != username {
			continue
		}
		crypt := sha512_crypt.New()
		err := crypt.Verify(identity.Basic.Password, passwordBytes)
		if err == nil {
			return identity
		}
		// No further username match possible.
		break
	}
	// If basic auth credentials were provided, but did not match, we bail.
	return nil
}

// noTypeError returns the error message for when no identity type is specified.
func noTypeError() error {
	return errors.New(`identity must have at least one type ("local", "basic", or "cert")`)
}
