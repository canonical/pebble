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
	// Level is the check level to filter for. A check is included in the
	// healthiness determination if this field is not set, or if it is
	// equal to the check's level.
	Level CheckLevel

	// Names is the list of check names to filter for. A check is included in
	// the healthiness determination if this field is nil or empty slice, or
	// if one of the values in the slice is equal to the check's name.
	Names []string
}

// HealthInfo holds the result of a Health call.
type HealthInfo struct {
	// Healthy is the status of health. A set of checks are deemed "healthy"
	// if all of them of are up, and "unhealthy" otherwise. When queried
	// using level, if the queried level equals to "alive", only the alive
	// checks are selected. However, "ready" implies alive. Thus, if the
	// queried level is "ready", both the alive and ready leveled checks are
	// considered.
	Healthy bool `json:"healthy"`
}

// Health fetches healthy status of specified checks.
func (client *Client) Health(opts *HealthOptions) (*HealthInfo, error) {
	query := make(url.Values)
	if opts.Level != UnsetLevel {
		query.Set("level", string(opts.Level))
	}
	if len(opts.Names) > 0 {
		query["names"] = opts.Names
	}

	var health HealthInfo
	_, err := client.doSync("GET", "/v1/health", query, nil, nil, &health)
	if err != nil {
		return nil, err
	}
	return &health, nil
}
