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
)

type MergeLayerOptions struct {
	Layer string
}

func (client *Client) MergeLayer(opts *MergeLayerOptions) error {
	var payload = struct {
		Action string `json:"action"`
		Format string `json:"format"`
		Layer  string `json:"layer"`
	}{
		Action: "merge",
		Format: "yaml",
		Layer:  opts.Layer,
	}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(&payload); err != nil {
		return err
	}
	_, err := client.doSync("POST", "/v1/layers", nil, nil, &body, nil)
	return err
}

type FlattenedSetupOptions struct{}

func (client *Client) FlattenedSetup(_ *FlattenedSetupOptions) (layerYAML string, err error) {
	var payload = struct {
		Action string `json:"action"`
		Format string `json:"format"`
	}{
		Action: "flatten",
		Format: "yaml",
	}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(&payload); err != nil {
		return "", err
	}
	_, err = client.doSync("POST", "/v1/layers", nil, nil, &body, &layerYAML)
	if err != nil {
		return "", err
	}
	return layerYAML, nil
}
