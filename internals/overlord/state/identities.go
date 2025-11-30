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
	"regexp"
	"sort"
	"strings"
)

// Identity holds the configuration of a single identity.
type Identity struct {
	Name   string
	Access IdentityAccess

	// One or more of the following type-specific configuration fields must be
	// non-nil.
	Local *LocalIdentity
	Basic *BasicIdentity
	// This field is always nil in FIPS builds.
	Cert *CertIdentity
}

// IdentityAccess defines the access level for an identity.
type IdentityAccess string

const (
	AdminAccess     IdentityAccess = "admin"
	ReadAccess      IdentityAccess = "read"
	MetricsAccess   IdentityAccess = "metrics"
	UntrustedAccess IdentityAccess = "untrusted"
)

// LocalIdentity holds identity configuration specific to the "local" type
// (for ucrednet/UID authentication).
type LocalIdentity struct {
	UserID uint32
}

// BasicIdentity holds identity configuration specific to the "basic" type
// (for HTTP basic authentication).
type BasicIdentity struct {
	// Password holds the user's sha512-crypt-hashed password.
	Password string
}

// Certificate identity represents the client in an mTLS connection. We
// only support a self-signed x509 certificate without intermediaries.
//
// In FIPS builds, certificate authentication is not supported. The X509
// field will always be nil, and any identity with Type="certificate" will
// fail validation.
type CertIdentity struct {
	// X509 is the parsed certificate. In FIPS builds, this field is always
	// nil as certificate authentication is disabled.
	X509 *x509.Certificate
}

// This is used to ensure we send a well-formed identity Name.
var identityNameRegexp = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_\-]*$`)

// validate checks that the identity is valid, returning an error if not.
func (d *Identity) validate(name string) error {
	if d == nil {
		return errors.New("identity must not be nil")
	}

	if !identityNameRegexp.MatchString(name) {
		return fmt.Errorf("identity name %q invalid: must start with an alphabetic character and only contain alphanumeric characters, underscore, and hyphen", d.Name)
	}

	return d.validateAccess()
}

// apiIdentity exists so the default JSON marshalling of an Identity (used
// for API responses) excludes secrets. The marshalledIdentity type is used
// for saving secrets in state.
type apiIdentity struct {
	Access string            `json:"access"`
	Local  *apiLocalIdentity `json:"local,omitempty"`
	Basic  *apiBasicIdentity `json:"basic,omitempty"`
	Cert   *apiCertIdentity  `json:"cert,omitempty"`
}

type apiLocalIdentity struct {
	UserID *uint32 `json:"user-id"`
}

type apiBasicIdentity struct {
	Password string `json:"password"`
}

type apiCertIdentity struct {
	PEM string `json:"pem"`
}

// IMPORTANT NOTE: be sure to exclude secrets when adding to this!
func (d *Identity) MarshalJSON() ([]byte, error) {
	ai := apiIdentity{
		Access: string(d.Access),
	}
	if d.Local != nil {
		ai.Local = &apiLocalIdentity{UserID: &d.Local.UserID}
	}
	if d.Basic != nil {
		ai.Basic = &apiBasicIdentity{Password: "*****"}
	}
	if d.Cert != nil {
		// This isn't actually secret, it's a public key by design, but we
		// replace it with ***** for consistency with the password field to
		// avoid confusion for the user. We can show it in future if needed.
		ai.Cert = &apiCertIdentity{PEM: "*****"}
	}
	return json.Marshal(ai)
}

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

	// Build-tag-specific cert validation
	certValid, err := d.validateCert()
	if err != nil {
		return err
	}
	if certValid {
		gotType = true
	}

	if !gotType {
		return noTypeError()
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
		certIdentity, err := unmarshalCert(ai.Cert)
		if err != nil {
			return err
		}
		identity.Cert = certIdentity
	}

	// Perform additional validation using the local Identity type.
	err = identity.validateAccess()
	if err != nil {
		return err
	}

	*d = identity
	return nil
}

// AddIdentities adds the given identities to the system. It's an error if any
// of the named identities already exist.
func (s *State) AddIdentities(identities map[string]*Identity) error {
	s.reading()

	// If any of the named identities already exist, return an error.
	var existing []string
	for name, identity := range identities {
		if _, ok := s.identities[name]; ok {
			existing = append(existing, name)
		}
		err := identity.validate(name)
		if err != nil {
			return fmt.Errorf("identity %q invalid: %w", name, err)
		}
	}
	if len(existing) > 0 {
		sort.Strings(existing)
		return fmt.Errorf("identities already exist: %s", strings.Join(existing, ", "))
	}

	newIdentities := s.cloneIdentities()
	for name, identity := range identities {
		identity.Name = name
		newIdentities[name] = identity
	}

	err := verifyUniqueUserIDs(newIdentities)
	if err != nil {
		return err
	}

	s.writing()
	s.identities = newIdentities
	return nil
}

// UpdateIdentities updates the given identities in the system. It's an error
// if any of the named identities do not exist.
func (s *State) UpdateIdentities(identities map[string]*Identity) error {
	s.reading()

	// If any of the named identities don't exist, return an error.
	var missing []string
	for name, identity := range identities {
		if _, ok := s.identities[name]; !ok {
			missing = append(missing, name)
		}
		err := identity.validate(name)
		if err != nil {
			return fmt.Errorf("identity %q invalid: %w", name, err)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("identities do not exist: %s", strings.Join(missing, ", "))
	}

	newIdentities := s.cloneIdentities()
	for name, identity := range identities {
		identity.Name = name
		newIdentities[name] = identity
	}

	err := verifyUniqueUserIDs(newIdentities)
	if err != nil {
		return err
	}

	s.writing()
	s.identities = newIdentities
	return nil
}

// ReplaceIdentities replaces the named identities in the system with the
// given identities (adding those that don't exist), or removes them if the
// map value is nil.
func (s *State) ReplaceIdentities(identities map[string]*Identity) error {
	s.reading()

	for name, identity := range identities {
		if identity != nil {
			err := identity.validate(name)
			if err != nil {
				return fmt.Errorf("identity %q invalid: %w", name, err)
			}
		}
	}

	newIdentities := s.cloneIdentities()
	for name, identity := range identities {
		if identity == nil {
			delete(newIdentities, name)
		} else {
			identity.Name = name
			newIdentities[name] = identity
		}
	}

	err := verifyUniqueUserIDs(newIdentities)
	if err != nil {
		return err
	}

	s.writing()
	s.identities = newIdentities
	return nil
}

// RemoveIdentities removes the named identities from the system. It's an
// error if any of the named identities do not exist.
func (s *State) RemoveIdentities(identities map[string]struct{}) error {
	s.reading()

	// If any of the named identities don't exist, return an error.
	var missing []string
	for name := range identities {
		if _, ok := s.identities[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("identities do not exist: %s", strings.Join(missing, ", "))
	}

	s.writing()
	for name := range identities {
		delete(s.identities, name)
	}
	return nil
}

// Identities returns all the identities in the system. The returned map is a
// shallow clone, so map mutations won't affect state.
func (s *State) Identities() map[string]*Identity {
	s.reading()

	result := make(map[string]*Identity, len(s.identities))
	for name, identity := range s.identities {
		result[name] = identity
	}
	return result
}

// IdentityFromInputs returns an identity matching the given inputs.
//
// We prioritize clientCert and username/password if either is provided,
// because they are intentionally setup by the client.
//
// If no matching identity is found for the given inputs, nil is returned.
//
// In FIPS builds, the clientCert parameter is accepted for API compatibility
// but will never match an identity, as certificate authentication is disabled.
func (s *State) IdentityFromInputs(userID *uint32, username, password string, clientCert *x509.Certificate) *Identity {
	s.reading()
	return s.identityFromInputs(userID, username, password, clientCert)
}

// identityFromInputs returns an identity matching the given inputs.
//
// In FIPS builds, clientCert will never match (identityFromCert always returns
// nil), and basic auth credentials will not match (identityFromBasicAuth always
// returns nil).
func (s *State) identityFromInputs(userID *uint32, username, password string, clientCert *x509.Certificate) *Identity {
	if clientCert != nil {
		return s.identityFromCert(clientCert)
	}

	if username != "" || password != "" {
		return s.identityFromBasicAuth(username, password)
	}

	if userID != nil {
		return s.identityFromUserID(userID)
	}

	return nil
}

// identityFromUserID returns an identity matching the given user ID.
func (s *State) identityFromUserID(userID *uint32) *Identity {
	for _, identity := range s.identities {
		if identity.Local != nil && identity.Local.UserID == *userID {
			return identity
		}
	}
	return nil
}

func (s *State) cloneIdentities() map[string]*Identity {
	newIdentities := make(map[string]*Identity, len(s.identities))
	for name, identity := range s.identities {
		newIdentities[name] = identity
	}
	return newIdentities
}

func verifyUniqueUserIDs(identities map[string]*Identity) error {
	userIDs := make(map[uint32][]string) // maps user ID to identity names
	for name, identity := range identities {
		switch {
		case identity.Local != nil:
			uid := identity.Local.UserID
			userIDs[uid] = append(userIDs[uid], name)
		}
	}
	for userID, names := range userIDs {
		if len(names) > 1 {
			sort.Strings(names) // ensure error message is stable
			return fmt.Errorf("cannot have multiple identities with user ID %d (%s)",
				userID, strings.Join(names, ", "))
		}
	}
	return nil
}
