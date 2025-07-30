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
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/overlord/state"
)

func v1GetIdentities(c *Command, r *http.Request, _ *UserState) Response {
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	identities := st.Identities()
	return SyncResponse(identities)
}

func v1PostIdentities(c *Command, r *http.Request, user *UserState) Response {
	var payload struct {
		Action     string                     `json:"action"`
		Identities map[string]*state.Identity `json:"identities"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&payload); err != nil {
		return BadRequest("cannot decode request body: %v", err)
	}

	var identityNames map[string]struct{}
	switch payload.Action {
	case "add", "update":
		for name, identity := range payload.Identities {
			if identity == nil {
				return BadRequest(`identity value for %q must not be null for %s operation`, name, payload.Action)
			}
		}
	case "replace":
		break
	case "remove":
		identityNames = make(map[string]struct{})
		for name, identity := range payload.Identities {
			if identity != nil {
				return BadRequest(`identity value for %q must be null for %s operation`, name, payload.Action)
			}
			identityNames[name] = struct{}{}
		}
	default:
		return BadRequest(`invalid action %q, must be "add", "update", "replace", or "remove"`, payload.Action)
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	var err error
	switch payload.Action {
	case "add":
		for name, identity := range payload.Identities {
			logger.SecurityWarn(logger.SecurityUserCreated,
				fmt.Sprintf("%s,%s,%s", userString(user), name, identity.Access),
				fmt.Sprintf("Creating user %s (%s)", name, identity.Access))
		}
		err = st.AddIdentities(payload.Identities)
	case "update":
		for name, identity := range payload.Identities {
			logger.SecurityWarn(logger.SecurityUserUpdated,
				fmt.Sprintf("%s,%s,%s", userString(user), name, identity.Access),
				fmt.Sprintf("Updating user %s (%s)", name, identity.Access))
		}
		err = st.UpdateIdentities(payload.Identities)
	case "replace":
		for name, identity := range payload.Identities {
			if identity == nil {
				logger.SecurityWarn(logger.SecurityUserDeleted,
					fmt.Sprintf("%s,%s", userString(user), name),
					fmt.Sprintf("Deleting user %s", name))
			} else {
				logger.SecurityWarn(logger.SecurityUserUpdated,
					fmt.Sprintf("%s,%s,%s", userString(user), name, identity.Access),
					fmt.Sprintf("Updating user %s (%s)", name, identity.Access))
			}
		}
		err = st.ReplaceIdentities(payload.Identities)
	case "remove":
		for name := range payload.Identities {
			logger.SecurityWarn(logger.SecurityUserDeleted,
				fmt.Sprintf("%s,%s", userString(user), name),
				fmt.Sprintf("Deleting user %s", name))
		}
		err = st.RemoveIdentities(identityNames)
	}
	if err != nil {
		return BadRequest("%v", err)
	}

	return SyncResponse(nil)
}
