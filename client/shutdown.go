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

// ShutdownOptions specifies the options struct to use when calling shutdown.
// Currently no options are available. This struct is left here for future
// proofing.
type ShutdownOptions struct {
}

// Shutdown issues a call to Pebble instructing Pebble to shutdown.
func (client *Client) Shutdown(opts *ShutdownOptions) error {
	_, err := client.doSync("POST", "/v1/shutdown", nil, nil, nil, nil)
	return err
}
