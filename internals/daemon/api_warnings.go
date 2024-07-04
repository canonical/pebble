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

package daemon

import (
	"encoding/json"
	"net/http"
	"time"
)

// NOTE: warnings are not used in Pebble, but we're keeping the APIs for
// backwards-compatibility.

func v1AckWarnings(c *Command, r *http.Request, _ *UserState) Response {
	defer r.Body.Close()
	var op struct {
		Action    string    `json:"action"`
		Timestamp time.Time `json:"timestamp"`
	}
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&op); err != nil {
		return BadRequest("cannot decode request body into warnings operation: %v", err)
	}
	if op.Action != "okay" {
		return BadRequest("unknown warning action %q", op.Action)
	}

	// Do nothing; return 0 warnings okayed.
	return SyncResponse(0)
}

func v1GetWarnings(c *Command, r *http.Request, _ *UserState) Response {
	query := r.URL.Query()
	sel := query.Get("select")
	switch sel {
	case "all", "pending", "":
	default:
		return BadRequest("invalid select parameter: %q", sel)
	}

	// Do nothing; return empty JSON array (type doesn't matter).
	return SyncResponse([]string{})
}
