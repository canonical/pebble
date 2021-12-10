// Copyright (c) 2021 Canonical Ltd
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
	"encoding/json"
	"fmt"
)

type SendSignalOptions struct {
	Signal   string
	Services []string
}

// SendSignal sends a signal to each of the specified services.
func (client *Client) SendSignal(opts *SendSignalOptions) error {
	var body bytes.Buffer
	payload := signalsPayload{
		Signal:   opts.Signal,
		Services: opts.Services,
	}
	err := json.NewEncoder(&body).Encode(&payload)
	if err != nil {
		return fmt.Errorf("cannot encode JSON payload: %v", err)
	}
	_, err = client.doSync("POST", "/v1/signals", nil, nil, &body, nil)
	return err
}

type signalsPayload struct {
	Signal   string   `json:"signal"`
	Services []string `json:"services"`
}
