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

import "net/url"

// HealthOptions holds query options to pass to a Health call.
type HealthOptions struct {
	// Level may be set to CheckAlive, to query whether alive checks are up, and
	// CheckReady, to query whether both alive and ready checks are up.
	// Defaults to CheckReady if unset.
	Level CheckLevel

	// Names defines which checks should be considered for the query. Defaults to all.
	Names []string
}

type healthInfo struct {
	Healthy bool `json:"healthy"`
}

// Health fetches healthy status of specified checks.
func (client *Client) Health(opts *HealthOptions) (health bool, err error) {
	query := make(url.Values)
	if opts.Level != UnsetLevel {
		query.Set("level", string(opts.Level))
	}
	if len(opts.Names) > 0 {
		query["names"] = opts.Names
	}

	var info healthInfo
	_, err = client.doSync("GET", "/v1/health", query, nil, nil, &info)
	if err != nil {
		return false, err
	}
	return info.Healthy, nil
}
