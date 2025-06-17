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
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"gopkg.in/yaml.v2"
)

type AddLayerOptions struct {
	// Combine true means combine the new layer with an existing layer that
	// has the given label. False (the default) means append a new layer.
	Combine bool

	// Inner set to true means a new layer append may go into an existing
	// subdirectory, even though it may not result in appending it
	// to the end of the layers slice (it becomes an insert).
	Inner bool

	// Label is the label for the new layer if appending, and the label of the
	// layer to combine with if Combine is true.
	Label string

	// LayerData is the new layer in YAML format.
	LayerData []byte
}

// AddLayer adds a layer to the plan's configuration layers.
func (client *Client) AddLayer(opts *AddLayerOptions) error {
	var payload = struct {
		Action  string `json:"action"`
		Combine bool   `json:"combine"`
		Inner   bool   `json:"inner"`
		Label   string `json:"label"`
		Format  string `json:"format"`
		Layer   string `json:"layer"`
	}{
		Action:  "add",
		Combine: opts.Combine,
		Inner:   opts.Inner,
		Label:   opts.Label,
		Format:  "yaml",
		Layer:   string(opts.LayerData),
	}

	// Add label validation here once layer persistence is supported over
	// the API. We cannot do this in the plan library because JUJU already
	// has labels in production systems that violates the layers file
	// naming convention (which includes the label). Since JUJU uses its
	// own client, we can enforce the label naming convention on all other
	// systems using the Pebble supplied client by validating it here.

	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(&payload); err != nil {
		return err
	}
	_, err := client.Requester().Do(context.Background(), &RequestOptions{
		Type:   SyncRequest,
		Method: "POST",
		Path:   "/v1/layers",
		Body:   &body,
	})
	return err
}

type PlanOptions struct{}

// PlanBytes fetches the plan in YAML format.
func (client *Client) PlanBytes(_ *PlanOptions) (data []byte, err error) {
	query := url.Values{
		"format": []string{"yaml"},
	}
	var dataStr string
	resp, err := client.Requester().Do(context.Background(), &RequestOptions{
		Type:   SyncRequest,
		Method: "GET",
		Path:   "/v1/plan",
		Query:  query,
	})
	if err != nil {
		return nil, err
	}
	err = resp.DecodeResult(&dataStr)
	if err != nil {
		return nil, err
	}
	return []byte(dataStr), nil
}

type ServiceConfig struct {
	Override string `yaml:"override"`
	Summary  string `yaml:"summary"`
	Command  string `yaml:"command"`
	Startup  string `yaml:"startup"`
}

type Check struct {
	Override  string `yaml:"override"`
	Level     string `yaml:"level"`
	Startup   string `yaml:"startup"`
	Period    string `yaml:"period"`
	Timeout   string `yaml:"timeout"`
	Threshold string `yaml:"threshold"`
	HTTP      string `yaml:"http"`
	TCP       string `yaml:"tcp"`
	Exec      string `yaml:"exec"`
}

type LogTarget struct {
	Override string            `yaml:"override"`
	Type     string            `yaml:"type"`
	Location string            `yaml:"location"`
	Services []string          `yaml:"services"`
	Labels   map[string]string `yaml:"labels"`
}

type Plan struct {
	Services   map[string]ServiceConfig `yaml:"services"`
	Checks     map[string]Check         `yaml:"checks"`
	LogTargets map[string]LogTarget     `yaml:"log-targets"`
}

// Plan fetches the plan and unmarshals it into a Plan struct.
func (client *Client) Plan() (*Plan, error) {
	data, err := client.PlanBytes(nil)
	if err != nil {
		return nil, err
	}

	var plan Plan

	err = yaml.Unmarshal(data, &plan)
	if err != nil {
		return nil, fmt.Errorf("cannot unmarshal plan: %w", err)
	}

	return &plan, nil
}
