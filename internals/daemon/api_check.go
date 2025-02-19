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

package daemon

import (
	"net/http"
)

func v1GetCheck(c *Command, r *http.Request, _ *UserState) Response {
	query := r.URL.Query()
	name := query.Get("name")

	checkMgr := c.d.overlord.CheckManager()
	checks, err := checkMgr.Checks()
	if err != nil {
		return InternalError("%v", err)
	}

	for _, check := range checks {
		if name == check.Name {
			info := checkInfo{
				Name:      check.Name,
				Level:     string(check.Level),
				Startup:   string(check.Startup),
				Status:    string(check.Status),
				Failures:  check.Failures,
				Threshold: check.Threshold,
				ChangeID:  check.ChangeID,
			}
			return SyncResponse(info)
		}
	}

	return NotFound("cannot find check with name %q", name)
}
