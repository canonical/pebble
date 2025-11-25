//go:build fips

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
	"errors"
	"fmt"
)

// validateAccess checks that the identity's access and type are valid, returning an error if not.
// FIPS version: allows Basic identities (storing password hashes), blocks Cert identities.
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
		return errors.New("certificate authentication is not supported in FIPS builds")
	}
	if !gotType {
		return errors.New(`identity must have at least one type ("local" or "basic"; cert auth not supported in FIPS builds)`)
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
		return errors.New("certificate authentication is not supported in FIPS builds")
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
// FIPS version: blocks login with basic auth (password verification) and certificate auth.
func (s *State) identityFromInputs(userID *uint32, username, password string, clientCert *x509.Certificate) *Identity {
	switch {
	case clientCert != nil:
		// Certificate authentication is not supported in FIPS builds
		return nil

	case username != "" || password != "":
		// Basic authentication login is not supported in FIPS builds (password verification
		// requires the github.com/GehirnInc/crypt library which is not FIPS-certified)
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
