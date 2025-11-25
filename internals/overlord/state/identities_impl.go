//go:build !fips

// Copyright (c) 2024 Canonical Ltd
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
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"

	"github.com/GehirnInc/crypt/sha512_crypt"
)

// validateAccess checks that the identity's access and type are valid, returning an error if not.
func (d *Identity) validateAccess() error {
	if d == nil {
		return errors.New("identity must not be nil")
	}

	switch d.Access {
	case AdminAccess, ReadAccess, MetricsAccess, UntrustedAccess:
	case "":
		return fmt.Errorf("access value must be specified (%q, %q, %q, or %q)",
			AdminAccess, ReadAccess, MetricsAccess, UntrustedAccess)
	default:
		return fmt.Errorf("invalid access value %q, must be %q, %q, %q, or %q",
			d.Access, AdminAccess, ReadAccess, MetricsAccess, UntrustedAccess)
	}

	gotType := false
	if d.Local != nil {
		gotType = true
	}
	if d.Basic != nil {
		if d.Basic.Password == "" {
			return errors.New("basic identity must specify password (hashed)")
		}
		gotType = true
	}
	if d.Cert != nil {
		if d.Cert.X509 == nil {
			return errors.New("cert identity must include an X.509 certificate")
		}
		gotType = true
	}
	if !gotType {
		return errors.New(`identity must have at least one type ("local", "basic", or "cert")`)
	}

	return nil
}

func (d *Identity) UnmarshalJSON(data []byte) error {
	var ai apiIdentity
	err := json.Unmarshal(data, &ai)
	if err != nil {
		return err
	}

	identity := Identity{
		Access: IdentityAccess(ai.Access),
	}

	if ai.Local != nil {
		if ai.Local.UserID == nil {
			return errors.New("local identity must specify user-id")
		}
		identity.Local = &LocalIdentity{UserID: *ai.Local.UserID}
	}
	if ai.Basic != nil {
		identity.Basic = &BasicIdentity{Password: ai.Basic.Password}
	}
	if ai.Cert != nil {
		block, rest := pem.Decode([]byte(ai.Cert.PEM))
		if block == nil {
			return errors.New("cert identity must include a PEM-encoded certificate")
		}
		if len(rest) > 0 {
			return errors.New("cert identity cannot have extra data after the PEM block")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return fmt.Errorf("cannot parse certificate from cert identity: %w", err)
		}
		identity.Cert = &CertIdentity{X509: cert}
	}

	// Perform additional validation using the local Identity type.
	err = identity.validateAccess()
	if err != nil {
		return err
	}

	*d = identity
	return nil
}

// identityFromInputs returns an identity matching the given inputs.
func (s *State) identityFromInputs(userID *uint32, username, password string, clientCert *x509.Certificate) *Identity {
	switch {
	case clientCert != nil:
		for _, identity := range s.identities {
			if identity.Cert != nil && identity.Cert.X509.Equal(clientCert) {
				// Certificate identities can be added
				// manually, so we still need to verify
				// this was a self-signed client identity
				// certificate without intermediaries.
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

	case username != "" || password != "":
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

	case userID != nil:
		for _, identity := range s.identities {
			if identity.Local != nil && identity.Local.UserID == *userID {
				return identity
			}
		}
		// If UID was provided, but did not match, we bail.
		return nil
	}

	return nil
}
