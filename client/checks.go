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
	CheckStatusUp   CheckStatus = "up"
	CheckStatusDown CheckStatus = "down"
)

// CheckInfo holds status information for a single health check.
type CheckInfo struct {
	// Name is the name of this check, from the layer configuration.
	Name string `json:"name"`

	// Level is this check's level, from the layer configuration.
	Level CheckLevel `json:"level"`

	// Status is the status of this check: "up" if healthy, "down" if the
	// number of failures has reached the configured threshold.
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
	_, err := client.doSync("GET", "/v1/checks", query, nil, nil, &checks)
	if err != nil {
		return nil, err
	}
	return checks, nil
}
