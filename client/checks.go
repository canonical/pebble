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
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

type ChecksOptions struct {
	// Level is the check level to query for. A check is included in the
	// results if this field is not set, or if it is equal to the check's
	// level.
	Level CheckLevel

	// Names is the list of check names to query for. A check is included in
	// the results if this field is nil or empty slice, or if one of the
	// values in the slice is equal to the check's name.
	Names []string
}

type ChecksActionOptions struct {
	// Names is the list of check names on which to perform the action.
	Names []string
}

// ChecksActionResult holds the results of a check action.
type ChecksActionResult struct {
	Changed []string `json:"changed"`
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
	// number of failures has reached the configured threshold, or "inactive" if
	// the check is inactive.
	Status CheckStatus `json:"status"`

	// Successes is the number of times this check has succeeded. It is reset
	// when the check succeeds again after the check's failure threshold was
	// reached, or if the check is stopped and started again. This will be
	// zero if the check has never run, or has never run successfully.
	//
	// This field will be nil if running against a version of the daemon
	// before this field was added to the API.
	Successes *int `json:"successes"`

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

	// PrevChangeID is the ID of the previous change. For a "recover-check"
	// change, this is the "perform-check" change that was running before the
	// check started failing. For a "perform-check" change, this is the
	// "recover-check" change that was running before the check recovered, or
	// empty if the check has never had to recover.
	PrevChangeID string `json:"prev-change-id"`
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

// StartChecks starts the checks named in opts.Names.
func (client *Client) StartChecks(opts *ChecksActionOptions) (*ChecksActionResult, error) {
	return client.doMultiCheckAction("start", opts.Names)
}

// StopChecks stops the checks named in opts.Names.
func (client *Client) StopChecks(opts *ChecksActionOptions) (*ChecksActionResult, error) {
	return client.doMultiCheckAction("stop", opts.Names)
}

type multiCheckActionData struct {
	Action string   `json:"action"`
	Checks []string `json:"checks"`
}

func (client *Client) doMultiCheckAction(actionName string, checks []string) (*ChecksActionResult, error) {
	action := multiCheckActionData{
		Action: actionName,
		Checks: checks,
	}
	data, err := json.Marshal(&action)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal multi-check action: %w", err)
	}

	resp, err := client.Requester().Do(context.Background(), &RequestOptions{
		Type:   SyncRequest,
		Method: "POST",
		Path:   "/v1/checks",
		Body:   bytes.NewBuffer(data),
	})
	if err != nil {
		return nil, err
	}

	var results *ChecksActionResult
	err = resp.DecodeResult(&results)
	if err != nil {
		return nil, err
	}

	return results, nil
}

type RefreshCheckOptions struct {
	// Name of the check to refresh (required).
	Name string
}

type RefreshCheckResult struct {
	Info CheckInfo `json:"info"`
	// The error message from running the check; empty string on success.
	Error string `json:"error"`
}

// RefreshCheck runs a specific health check immediately.
func (client *Client) RefreshCheck(opts *RefreshCheckOptions) (*RefreshCheckResult, error) {
	payload := struct {
		Name string `json:"name"`
	}{
		Name: opts.Name,
	}
	data, err := json.Marshal(&payload)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal checks payload: %w", err)
	}

	resp, err := client.Requester().Do(context.Background(), &RequestOptions{
		Type:   SyncRequest,
		Method: "POST",
		Path:   "/v1/checks/refresh",
		Body:   bytes.NewBuffer(data),
	})
	if err != nil {
		return nil, err
	}
	var result RefreshCheckResult
	err = resp.DecodeResult(&result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}
