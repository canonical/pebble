// Copyright (C) 2024 Canonical Ltd
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

// AccessChecker checks whether a particular request is allowed.
type AccessChecker interface {
	// Check if access should be granted or denied. In case of granting access,
	// return nil. In case access is denied, return a non-nil error response,
	// such as statusForbidden("access denied").
	CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *UserState) Response
}

// openAccess allows all requests
type openAccess struct{}

func (ac openAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *UserState) Response {
	return nil
}

// rootAccess allows requests from the root uid
type rootAccess struct{}

func (ac rootAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *UserState) Response {
	if ucred != nil && ucred.Uid == 0 {
		return nil
	}
	return statusForbidden("access denied")
}

// userAccess allows requests from any local user
type userAccess struct{}

func (ac userAccess) CheckAccess(d *Daemon, r *http.Request, ucred *ucrednet, user *UserState) Response {
	if ucred == nil {
		return statusForbidden("access denied")
	}
	return nil
}
