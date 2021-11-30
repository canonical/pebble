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
	"net/url"
)

// ChecksOptions are the filtering options for querying health checks.
type ChecksOptions struct {
	// Level is the check level to query for. If this is UnsetLevel (the zero
	// value), don't filter by level. Because "ready" implies "alive", if
	// level is AliveLevel, checks with level "ready" are included too.
	Level CheckLevel

	// Names is the list of check names to query for. If slice is nil or
	// empty, don't filter by name.
	Names []string
}

type CheckLevel string

const (
	UnsetLevel CheckLevel = ""
	AliveLevel CheckLevel = "alive"
	ReadyLevel CheckLevel = "ready"
)

// CheckInfo holds status information for a single health check.
type CheckInfo struct {
	Name         string     `json:"name"`
	Level        CheckLevel `json:"level"`
	Healthy      bool       `json:"healthy"`
	Failures     int        `json:"failures,omitempty"`
	LastError    string     `json:"last-error,omitempty"`
	ErrorDetails string     `json:"error-details,omitempty"`
}

// Checks fetches information about specific health checks (or all of them),
// ordered by check name.
func (client *Client) Checks(opts *ChecksOptions) ([]*CheckInfo, error) {
	query := url.Values{
		"level": []string{string(opts.Level)},
		"names": opts.Names,
	}
	var checks []*CheckInfo
	_, err := client.doSync("GET", "/v1/checks", query, nil, nil, &checks)
	if err != nil {
		return nil, err
	}
	return checks, nil
}
