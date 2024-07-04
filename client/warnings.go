// Copyright (c) 2014-2020 Canonical Ltd
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
	"time"
)

// Warning holds a short message that's meant to alert about system events.
// There'll only ever be one Warning with the same message, and it can be
// silenced for a while before repeating. After a (supposedly longer) while
// it'll go away on its own (unless it recurs).
type Warning struct {
	Message     string        `json:"message"`
	FirstAdded  time.Time     `json:"first-added"`
	LastAdded   time.Time     `json:"last-added"`
	LastShown   time.Time     `json:"last-shown,omitempty"`
	ExpireAfter time.Duration `json:"expire-after,omitempty"`
	RepeatAfter time.Duration `json:"repeat-after,omitempty"`
}

type WarningsOptions struct {
	// All means return all warnings, instead of only the un-okayed ones.
	All bool
}

// Warnings returns the list of un-okayed warnings.
func (client *Client) Warnings(opts WarningsOptions) ([]*Warning, error) {
	// Pebble has never produced warnings (and now it can't), so we can just
	// return an empty slice here.
	return []*Warning{}, nil
}

// Okay asks the server to silence the warnings that would have been returned by
// Warnings at the given time.
func (client *Client) Okay(t time.Time) error {
	// Pebble has never produced warnings (and now it can't), so we can just
	// do nothing here.
	return nil
}
