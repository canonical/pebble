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
	"net/url"
)

type AddLayerOptions struct {
	// Combine true means combine the new layer with an existing layer that
	// has the given label. False (the default) means append a new layer.
	Combine bool

	// Label is the label for the new layer if appending, and the label of the
	// layer to combine with if Combine is true.
	Label string

	// LayerData is the new layer in YAML format.
	LayerData []byte
}

// AddLayer adds a layer to the plan's layers according to opts.Action.
func (client *Client) AddLayer(opts *AddLayerOptions) error {
	var payload = struct {
		Action  string `json:"action"`
		Combine bool   `json:"combine"`
		Label   string `json:"label"`
		Format  string `json:"format"`
		Layer   string `json:"layer"`
	}{
		Action:  "add",
		Combine: opts.Combine,
		Label:   opts.Label,
		Format:  "yaml",
		Layer:   string(opts.LayerData),
	}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(&payload); err != nil {
		return err
	}
	_, err := client.doSync("POST", "/v1/layers", nil, nil, &body, nil)
	return err
}

type PlanOptions struct{}

// PlanBytes fetches the plan in YAML format.
func (client *Client) PlanBytes(_ *PlanOptions) (data []byte, err error) {
	query := url.Values{
		"format": []string{"yaml"},
	}
	var dataStr string
	_, err = client.doSync("GET", "/v1/plan", query, nil, nil, &dataStr)
	if err != nil {
		return nil, err
	}
	return []byte(dataStr), nil
}
