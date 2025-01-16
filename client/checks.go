// Copyright (c) 2022 Canonical Ltd
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
	"context"
	"net/url"
)

type ChecksOptions struct {
	// Level is the check level to query for. A check is included in the
	// results if this field is not set, or if it is equal to the check's
	// level.
	Level CheckLevel

	// Names is the list of check names on which to action. For querying, a
	// check is included in the results if this field is nil or empty slice. For
	// all actions, a check is included in the results if one of the values in
	// the slice is equal to the check's name.
	Names []string
}

// CheckLevel represents the level of a health check.
type CheckLevel string

const (
	UnsetLevel CheckLevel = ""
	AliveLevel CheckLevel = "alive"
	ReadyLevel CheckLevel = "ready"
)

// CheckStatus represents the status of a health check.
type CheckStatus string

const (
	CheckStatusUp       CheckStatus = "up"
	CheckStatusDown     CheckStatus = "down"
	CheckStatusInactive CheckStatus = "inactive"
)

// CheckStartup defines the different startup modes for a check.
type CheckStartup string

const (
	CheckStartupEnabled  CheckStartup = "enabled"
	CheckStartupDisabled CheckStartup = "disabled"
)

// CheckInfo holds status information for a single health check.
type CheckInfo struct {
	// Name is the name of this check, from the layer configuration.
	Name string `json:"name"`

	// Level is this check's level, from the layer configuration.
	Level CheckLevel `json:"level"`

	// Startup is the startup mode for the check. If it is "enabled", the check
	// will be started in a Pebble replan and when Pebble starts. If it is
	// "disabled", it must be started manually.
	Startup CheckStartup `json:"startup"`

	// Status is the status of this check: "up" if healthy, "down" if the
	// number of failures has reached the configured threshold, "inactive" if
	// the check is disabled.
	Status CheckStatus `json:"status"`

	// Failures is the number of times in a row this check has failed. It is
	// reset to zero as soon as the check succeeds.
	Failures int `json:"failures"`

	// Threshold is this check's failure threshold, from the layer
	// configuration.
	Threshold int `json:"threshold"`

	// ChangeID is the ID of the change corresponding to this check operation.
	// The change will be of kind "perform-check" if the check is up, or
	// "recover-check" if it's down.
	ChangeID string `json:"change-id"`
}

// Checks fetches information about specific health checks (or all of them),
// ordered by check name.
func (client *Client) Checks(opts *ChecksOptions) ([]*CheckInfo, error) {
	query := make(url.Values)
	if opts.Level != UnsetLevel {
		query.Set("level", string(opts.Level))
	}
	if len(opts.Names) > 0 {
		query["names"] = opts.Names
	}
	var checks []*CheckInfo
	resp, err := client.Requester().Do(context.Background(), &RequestOptions{
		Type:   SyncRequest,
		Method: "GET",
		Path:   "/v1/checks",
		Query:  query,
	})
	if err != nil {
		return nil, err
	}
	err = resp.DecodeResult(&checks)
	if err != nil {
		return nil, err
	}
	return checks, nil
}

// AutoStart starts the checks marked as "startup: enabled". opts.Names must
// be empty for this call. We ignore ops.Level for this action.
func (client *Client) AutoStartChecks(opts *ChecksOptions) (response string, err error) {
	response, err = client.doMultiCheckAction("autostart", opts.Names)
	return response, err
}

// Start starts the checks named in opts.Names. We ignore ops.Level for this
// action.
func (client *Client) StartChecks(opts *ChecksOptions) (response string, err error) {
	response, err = client.doMultiCheckAction("start", opts.Names)
	return response, err
}

// Stop stops the checks named in opts.Names. We ignore ops.Level for this
// action.
func (client *Client) StopChecks(opts *ChecksOptions) (response string, err error) {
	response, err = client.doMultiCheckAction("stop", opts.Names)
	return response, err
}

type multiCheckActionData struct {
	Action string   `json:"action"`
	Checks []string `json:"checks"`
}

func (client *Client) doMultiCheckAction(actionName string, checks []string) (changeID string, err error) {
	action := multiCheckActionData{
		Action: actionName,
		Checks: checks,
	}
	data, err := json.Marshal(&action)
	if err != nil {
		return "", fmt.Errorf("cannot marshal multi-check action: %w", err)
	}
	headers := map[string]string{
		"Content-Type": "application/json",
	}

	resp, err := client.doSync("POST", "/v1/checks", nil, headers, bytes.NewBuffer(data), nil)
	if err != nil {
		return "", err
	}
	return string(resp.Result), nil
}
