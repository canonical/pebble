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

package identities

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"sort"
	"strings"

	"github.com/GehirnInc/crypt/sha512_crypt"

	"github.com/canonical/pebble/internals/overlord/state"
)

const (
	identitiesKey = "identities"
)

type Manager struct {
	state *state.State

	// Keep a local copy to avoid having to deserialize from state each time
	// Get is called.
	identities map[string]*Identity
}

func NewManager(st *state.State) (*Manager, error) {
	m := &Manager{
		state:      st,
		identities: make(map[string]*Identity),
	}

	m.state.Lock()
	defer m.state.Unlock()

	// Read existing identities from state, if any.
	err := st.Get(identitiesKey, &m.identities)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	// Populate the Name field for each identity from the JSON object key.
	for name, identity := range m.identities {
		identity.Name = name
	}

	return m, nil
}

func (m *Manager) Ensure() error {
	return nil
}

// Identity holds the configuration of a single identity.
//
// IMPORTANT: When adding a new identity type, if there's sensitive fields in it
// (like passwords), be sure to omit it from API marshalling in api_identities.go.
type Identity struct {
	Name   string `json:"-"`
	Access Access `json:"access"`

	// One or more of the following type-specific configuration fields must be
	// non-nil.
	Local *LocalIdentity `json:"local,omitempty"`
	Basic *BasicIdentity `json:"basic,omitempty"`
	Cert  *CertIdentity  `json:"cert,omitempty"`
}

// Access defines the access level for an identity.
type Access string

const (
	AdminAccess     Access = "admin"
	ReadAccess      Access = "read"
	MetricsAccess   Access = "metrics"
	UntrustedAccess Access = "untrusted"
)

// LocalIdentity holds identity configuration specific to the "local" type
// (for ucrednet/UID authentication).
type LocalIdentity struct {
	UserID uint32 `json:"user-id"`
}

// BasicIdentity holds identity configuration specific to the "basic" type
// (for HTTP basic authentication).
type BasicIdentity struct {
	// Password holds the user's sha512-crypt-hashed password.
	Password string `json:"password"`
}

// Certificate identity represents the client in an mTLS connection. We
// only support a self-signed x509 certificate without intermediaries.
type CertIdentity struct {
	X509 *x509.Certificate
}

type marshalledCertIdentity struct {
	PEM string `json:"pem"`
}

func (c *CertIdentity) MarshalJSON() ([]byte, error) {
	pemBlock := &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: c.X509.Raw,
	}
	marshalled := marshalledCertIdentity{
		PEM: string(pem.EncodeToMemory(pemBlock)),
	}
	return json.Marshal(marshalled)
}

func (c *CertIdentity) UnmarshalJSON(data []byte) error {
	var unmarshalled marshalledCertIdentity
	err := json.Unmarshal(data, &unmarshalled)
	if err != nil {
		return err
	}
	block, _ := pem.Decode([]byte(unmarshalled.PEM))
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("cannot parse certificate from cert identity: %w", err)
	}
	c.X509 = cert
	return nil
}

// This is used to ensure we send a well-formed identity Name.
var identityNameRegexp = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_\-]*$`)

// Validate checks that the identity's fields (and name) are valid, returning an error if not.
func (d *Identity) Validate(name string) error {
	if d == nil {
		return errors.New("identity must not be nil")
	}

	if !identityNameRegexp.MatchString(name) {
		return fmt.Errorf("identity name %q invalid: must start with an alphabetic character and only contain alphanumeric characters, underscore, and hyphen", d.Name)
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

// AddIdentities adds the given identities to the system. It's an error if any
// of the named identities already exist.
//
// The state lock must be held for the duration of this call.
func (m *Manager) AddIdentities(identities map[string]*Identity) error {
	// If any of the named identities already exist, return an error.
	var existing []string
	for name, identity := range identities {
		if _, ok := m.identities[name]; ok {
			existing = append(existing, name)
		}
		err := identity.Validate(name)
		if err != nil {
			return fmt.Errorf("identity %q invalid: %w", name, err)
		}
	}
	if len(existing) > 0 {
		sort.Strings(existing)
		return fmt.Errorf("identities already exist: %s", strings.Join(existing, ", "))
	}

	newIdentities := maps.Clone(m.identities)
	for name, identity := range identities {
		identity.Name = name
		newIdentities[name] = identity
	}

	err := verifyUniqueUserIDs(newIdentities)
	if err != nil {
		return err
	}

	m.identities = newIdentities
	m.state.Set(identitiesKey, newIdentities)
	return nil
}

// UpdateIdentities updates the given identities in the system. It's an error
// if any of the named identities do not exist.
//
// The state lock must be held for the duration of this call.
func (m *Manager) UpdateIdentities(identities map[string]*Identity) error {
	// If any of the named identities don't exist, return an error.
	var missing []string
	for name, identity := range identities {
		if _, ok := m.identities[name]; !ok {
			missing = append(missing, name)
		}
		err := identity.Validate(name)
		if err != nil {
			return fmt.Errorf("identity %q invalid: %w", name, err)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("identities do not exist: %s", strings.Join(missing, ", "))
	}

	newIdentities := maps.Clone(m.identities)
	for name, identity := range identities {
		identity.Name = name
		newIdentities[name] = identity
	}

	err := verifyUniqueUserIDs(newIdentities)
	if err != nil {
		return err
	}

	m.identities = newIdentities
	m.state.Set(identitiesKey, newIdentities)
	return nil
}

// ReplaceIdentities replaces the named identities in the system with the
// given identities (adding those that don't exist), or removes them if the
// map value is nil.
//
// The state lock must be held for the duration of this call.
func (m *Manager) ReplaceIdentities(identities map[string]*Identity) error {
	for name, identity := range identities {
		if identity != nil {
			err := identity.Validate(name)
			if err != nil {
				return fmt.Errorf("identity %q invalid: %w", name, err)
			}
		}
	}

	newIdentities := maps.Clone(m.identities)
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

	m.identities = newIdentities
	m.state.Set(identitiesKey, newIdentities)
	return nil
}

// RemoveIdentities removes the named identities from the system. It's an
// error if any of the named identities do not exist.
//
// The state lock must be held for the duration of this call.
func (m *Manager) RemoveIdentities(identities map[string]struct{}) error {
	// If any of the named identities don't exist, return an error.
	var missing []string
	for name := range identities {
		if _, ok := m.identities[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("identities do not exist: %s", strings.Join(missing, ", "))
	}

	for name := range identities {
		delete(m.identities, name)
	}
	m.state.Set(identitiesKey, m.identities)
	return nil
}

// Identities returns all the identities in the system. The returned map is a
// shallow clone, so map mutations won't affect state.
//
// The state lock must be held for the duration of this call.
func (m *Manager) Identities() map[string]*Identity {
	return maps.Clone(m.identities)
}

// IdentityFromInputs returns an identity matching the given inputs.
//
// We prioritize clientCert and username/password if either is provided,
// because they are intentionally setup by the client.
//
// If no matching identity is found for the given inputs, nil is returned.
//
// The state lock must be held for the duration of this call.
func (m *Manager) IdentityFromInputs(userID *uint32, username, password string, clientCert *x509.Certificate) *Identity {
	switch {
	case clientCert != nil:
		for _, identity := range m.identities {
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
		for _, identity := range m.identities {
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
		for _, identity := range m.identities {
			if identity.Local != nil && identity.Local.UserID == *userID {
				return identity
			}
		}
		// If UID was provided, but did not match, we bail.
		return nil
	}

	return nil
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
