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

package client

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
)

type pairingPayload struct {
	Action string `json:"action"`
}

// Pair pairs the client with the Pebble server using mTLS authentication.
// This establishes a trusted relationship between the client and server
// while the server has pairing mode enabled.
func (client *Client) Pair() (idCert *x509.Certificate, err error) {
	payload := pairingPayload{
		Action: "pair",
	}
	body, err := json.Marshal(&payload)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal pairing payload: %w", err)
	}
	resp, err := client.Requester().Do(context.Background(), &RequestOptions{
		Type:   SyncRequest,
		Method: "POST",
		Path:   "/v1/pairing",
		Body:   bytes.NewReader(body),
	})
	if err == nil && resp != nil {
		idCert = resp.TLSServerIDCert
	}
	return idCert, err
}
