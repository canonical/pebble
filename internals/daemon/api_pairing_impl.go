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

package daemon

import (
	"encoding/json"
	"net/http"
)

func v1PostPairing(c *Command, r *http.Request, user *UserState) Response {
	var payload struct {
		Action string `json:"action"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&payload); err != nil {
		return BadRequest("cannot decode request body: %v", err)
	}

	switch payload.Action {
	case "pair":
		if r.TLS == nil {
			return InternalError("cannot find TLS connection state")
		}
		// Validate that exactly one peer certificate is provided
		if len(r.TLS.PeerCertificates) != 1 {
			return BadRequest("cannot support client: single certificate expected, got %d", len(r.TLS.PeerCertificates))
		}
		// The leaf peer certificate is the client identity certificate.
		clientCert := r.TLS.PeerCertificates[0]

		pairingMgr := c.d.overlord.PairingManager()
		if err := pairingMgr.PairMTLS(clientCert); err != nil {
			return BadRequest("cannot pair client: %v", err)
		}

	default:
		return BadRequest(`invalid action %q, must be "pair"`, payload.Action)
	}

	return SyncResponse(nil)
}
