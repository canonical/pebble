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

package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

// Identity holds the configuration of a single identity.
type Identity struct {
	Access IdentityAccess `json:"access" yaml:"access"`

	// One or more of the following type-specific configuration fields must be
	// non-nil (currently the only types are "local" and "basic").
	Local *LocalIdentity `json:"local,omitempty" yaml:"local,omitempty"`
	Basic *BasicIdentity `json:"basic,omitempty" yaml:"basic,omitempty"`
}

// IdentityAccess defines the access level for an identity.
type IdentityAccess string

const (
	AdminAccess     IdentityAccess = "admin"
	ReadAccess      IdentityAccess = "read"
	UntrustedAccess IdentityAccess = "untrusted"
)

// LocalIdentity holds identity configuration specific to the "local" type
// (for ucrednet/UID authentication).
type LocalIdentity struct {
	// This is a pointer so we can distinguish between not set and 0 (a valid
	// user-id meaning root).
	UserID *uint32 `json:"user-id" yaml:"user-id"`
}

// BasicIdentity holds identity configuration specific to the "basic" type
// (for username/password authentication).
type BasicIdentity struct {
	Password string `json:"password" yaml:"password"`
}

// For future extension.
type IdentitiesOptions struct{}

type identitiesPayload struct {
	Action     string               `json:"action"`
	Identities map[string]*Identity `json:"identities"`
}

// Identities returns a map of all identities in the system.
func (client *Client) Identities(opts *IdentitiesOptions) (map[string]*Identity, error) {
	resp, err := client.Requester().Do(context.Background(), &RequestOptions{
		Type:   SyncRequest,
		Method: "GET",
		Path:   "/v1/identities",
	})
	if err != nil {
		return nil, err
	}
	var identities map[string]*Identity
	err = resp.DecodeResult(&identities)
	if err != nil {
		return nil, err
	}
	for name, identity := range identities {
		if identity == nil {
			return nil, fmt.Errorf("server returned null identity %q", name)
		}
	}
	return identities, nil
}

// AddIdentities adds the given identities to the system. It's an error if any
// of the named identities already exist.
func (client *Client) AddIdentities(identities map[string]*Identity) error {
	return client.postIdentities("add", identities)
}

// UpdateIdentities updates the given identities in the system. It's an error
// if any of the named identities do not exist.
func (client *Client) UpdateIdentities(identities map[string]*Identity) error {
	return client.postIdentities("update", identities)
}

// ReplaceIdentities replaces the named identities in the system with the
// given identities (adding those that don't exist), or removes them if the
// map value is nil.
func (client *Client) ReplaceIdentities(identities map[string]*Identity) error {
	return client.postIdentities("replace", identities)
}

// RemoveIdentities removes the named identities from the system. It's an
// error if any of the named identities do not exist.
func (client *Client) RemoveIdentities(identityNames map[string]struct{}) error {
	identities := make(map[string]*Identity, len(identityNames))
	for name := range identityNames {
		identities[name] = nil
	}
	return client.postIdentities("remove", identities)
}

func (client *Client) postIdentities(action string, identities map[string]*Identity) error {
	payload := identitiesPayload{
		Action:     action,
		Identities: identities,
	}
	body, err := json.Marshal(&payload)
	if err != nil {
		return fmt.Errorf("cannot marshal identities payload: %w", err)
	}
	_, err = client.Requester().Do(context.Background(), &RequestOptions{
		Type:   SyncRequest,
		Method: "POST",
		Path:   "/v1/identities",
		Body:   bytes.NewReader(body),
	})
	return err
}
