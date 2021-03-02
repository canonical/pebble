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

type AddLayerAction string

const (
	AddLayerCombine AddLayerAction = "combine"
)

type AddLayerOptions struct {
	// LayerData is the new layer in YAML format.
	LayerData []byte

	// Action is the action performed when adding the new layer. Only
	// "combine" is supported right now, which means combine the new layer
	// into the existing dynamic layer, or add a new dynamic layer if none
	// exists. If not set, default to "combine".
	Action AddLayerAction
}

// AddLayer adds a layer to the plan's layers according to opts.Action.
func (client *Client) AddLayer(opts *AddLayerOptions) error {
	var payload = struct {
		Action string `json:"action"`
		Format string `json:"format"`
		Layer  string `json:"layer"`
	}{
		Action: string(opts.Action),
		Format: "yaml",
		Layer:  string(opts.LayerData),
	}
	if payload.Action == "" {
		payload.Action = string(AddLayerCombine)
	}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(&payload); err != nil {
		return err
	}
	_, err := client.doSync("POST", "/v1/layers", nil, nil, &body, nil)
	return err
}

type PlanDataOptions struct{}

// PlanData fetches the plan in YAML format.
func (client *Client) PlanData(_ *PlanDataOptions) (data []byte, err error) {
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
