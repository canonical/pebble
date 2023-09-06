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

type HealthOptions ChecksOptions

// HealthInfo holds health information for a single health check.
type HealthInfo struct {
	// Healthy is the status of health.
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
