// Copyright (c) 2025 Canonical Ltd
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
	"context"
	"net/url"
)

type CheckOptions struct {
	// Name is the check name to query for.
	Name string
}

// Check fetches information about a specific health check.
func (client *Client) Check(opts *CheckOptions) (*CheckInfo, error) {
	query := make(url.Values)
	query.Add("name", opts.Name)
	var check *CheckInfo
	resp, err := client.Requester().Do(context.Background(), &RequestOptions{
		Type:   SyncRequest,
		Method: "GET",
		Path:   "/v1/check",
		Query:  query,
	})
	if err != nil {
		return nil, err
	}
	err = resp.DecodeResult(&check)
	if err != nil {
		return nil, err
	}
	return check, nil
}
