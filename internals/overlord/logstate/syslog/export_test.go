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

package syslog

import "github.com/canonical/pebble/internals/servicelog"

func GetBuffer(c *Client) []servicelog.Entry {
	return c.buffer
}

func GetMessage(e servicelog.Entry) string {
	return e.Message
}

func ResetBufferToIndex(c *Client, index int) {
	c.resetBufferToIndex(index)
}
