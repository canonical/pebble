// Copyright (c) 2023 Canonical Ltd
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
	"time"
)

type NotifyOptions struct {
	// Key is the client notice's key. Must be in "domain.com/key" format.
	Key string

	// RepeatAfter, if provided, allows the notice to repeat after this duration.
	RepeatAfter time.Duration

	// Data are optional key=value pairs for this occurrence of the notice.
	Data map[string]string
}

// Notify records an occurrence of a "client" notice with the specified options.
func (client *Client) Notify(opts *NotifyOptions) error {
	var payload = struct {
		Action      string            `json:"action"`
		Type        string            `json:"type"`
		Key         string            `json:"key"`
		RepeatAfter string            `json:"repeat-after,omitempty"`
		Data        map[string]string `json:"data,omitempty"`
	}{
		Action: "add",
		Type:   "client",
		Key:    opts.Key,
		Data:   opts.Data,
	}
	if opts.RepeatAfter != 0 {
		payload.RepeatAfter = opts.RepeatAfter.String()
	}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(&payload); err != nil {
		return err
	}
	_, err := client.doSync("POST", "/v1/notices", nil, nil, &body, nil)
	return err
}
