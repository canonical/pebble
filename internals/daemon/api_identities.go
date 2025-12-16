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

package daemon

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/overlord/identities"
)

// apiIdentity exists so the API marshalling of an Identity excludes secrets.
type apiIdentity struct {
	Access identities.Access `json:"access"`
	Local  *apiLocalIdentity `json:"local,omitempty"`
	Basic  *apiBasicIdentity `json:"basic,omitempty"`
	Cert   *apiCertIdentity  `json:"cert,omitempty"`
}

type apiLocalIdentity struct {
	// Needs to be a pointer so we can distinguish between missing and zero (UID 0).
	UserID *uint32 `json:"user-id"`
}

type apiBasicIdentity struct {
	Password string `json:"password"`
}

type apiCertIdentity struct {
	PEM string `json:"pem"`
}

// When adding a new identity type, be sure to mask secrets here.
func identityToAPI(d *identities.Identity) *apiIdentity {
	ai := &apiIdentity{
		Access: d.Access,
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
	return ai
}

func identityFromAPI(ai *apiIdentity, name string) (*identities.Identity, error) {
	if ai == nil {
		return nil, nil
	}

	identity := &identities.Identity{
		Access: ai.Access,
	}

	if ai.Local != nil {
		if ai.Local.UserID == nil {
			return nil, errors.New("local identity must specify user-id")
		}
		identity.Local = &identities.LocalIdentity{UserID: *ai.Local.UserID}
	}
	if ai.Basic != nil {
		identity.Basic = &identities.BasicIdentity{Password: ai.Basic.Password}
	}
	if ai.Cert != nil {
		block, rest := pem.Decode([]byte(ai.Cert.PEM))
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
		identity.Cert = &identities.CertIdentity{X509: cert}
	}

	// Perform additional validation using the local Identity type.
	err := identity.Validate(name)
	if err != nil {
		return nil, err
	}

	return identity, nil
}

func v1GetIdentities(c *Command, r *http.Request, _ *UserState) Response {
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	identitiesMgr := c.d.overlord.IdentitiesManager()
	idents := identitiesMgr.Identities()

	apiIdentities := make(map[string]*apiIdentity, len(idents))
	for name, identity := range idents {
		apiIdentities[name] = identityToAPI(identity)
	}
	return SyncResponse(apiIdentities)
}

func v1PostIdentities(c *Command, r *http.Request, user *UserState) Response {
	var payload struct {
		Action     string                  `json:"action"`
		Identities map[string]*apiIdentity `json:"identities"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&payload); err != nil {
		return BadRequest("cannot decode request body: %v", err)
	}

	idents := make(map[string]*identities.Identity, len(payload.Identities))
	for name, apiIdent := range payload.Identities {
		identity, err := identityFromAPI(apiIdent, name)
		if err != nil {
			return BadRequest("invalid identity for %q: %v", name, err)
		}
		idents[name] = identity
	}

	var identitiesToRemove map[string]struct{}
	switch payload.Action {
	case "add", "update":
		for name, identity := range idents {
			if identity == nil {
				return BadRequest(`identity value for %q must not be null for %s operation`, name, payload.Action)
			}
		}
	case "replace":
		break
	case "remove":
		identitiesToRemove = make(map[string]struct{})
		for name, identity := range idents {
			if identity != nil {
				return BadRequest(`identity value for %q must be null for %s operation`, name, payload.Action)
			}
			identitiesToRemove[name] = struct{}{}
		}
	default:
		return BadRequest(`invalid action %q, must be "add", "update", "replace", or "remove"`, payload.Action)
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	identitiesMgr := c.d.overlord.IdentitiesManager()

	var err error
	switch payload.Action {
	case "add":
		for name, identity := range idents {
			logger.SecurityWarn(logger.SecurityUserCreated,
				fmt.Sprintf("%s,%s,%s", userString(user), name, identity.Access),
				fmt.Sprintf("Creating %s user %s", identity.Access, name))
		}
		err = identitiesMgr.AddIdentities(idents)
	case "update":
		for name, identity := range idents {
			logger.SecurityWarn(logger.SecurityUserUpdated,
				fmt.Sprintf("%s,%s,%s", userString(user), name, identity.Access),
				fmt.Sprintf("Updating %s user %s", identity.Access, name))
		}
		err = identitiesMgr.UpdateIdentities(idents)
	case "replace":
		for name, identity := range idents {
			if identity == nil {
				logger.SecurityWarn(logger.SecurityUserDeleted,
					fmt.Sprintf("%s,%s", userString(user), name),
					fmt.Sprintf("Deleting user %s", name))
			} else {
				logger.SecurityWarn(logger.SecurityUserUpdated,
					fmt.Sprintf("%s,%s,%s", userString(user), name, identity.Access),
					fmt.Sprintf("Updating %s user %s", identity.Access, name))
			}
		}
		err = identitiesMgr.ReplaceIdentities(idents)
	case "remove":
		for name := range idents {
			logger.SecurityWarn(logger.SecurityUserDeleted,
				fmt.Sprintf("%s,%s", userString(user), name),
				fmt.Sprintf("Deleting user %s", name))
		}
		err = identitiesMgr.RemoveIdentities(identitiesToRemove)
	}
	if err != nil {
		return BadRequest("%v", err)
	}

	return SyncResponse(nil)
}
