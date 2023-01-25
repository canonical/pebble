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

package daemon

import (
	"net/http"
)

// v1PostShutdown is a handler for initiating a Pebble shutdown via the daemon.
func v1PostShutdown(c *Command, r *http.Request, _ *userState) Response {
	// We call tomb.Kill as calling stop on the daemon will block and create a
	// dead lock condition between this handler and stop.
	c.d.tomb.Kill(nil)
	return SyncResponse(nil)
}
