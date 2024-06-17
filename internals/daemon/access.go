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
	"os"

	"github.com/canonical/pebble/internals/overlord/state"
)

// AccessChecker checks whether a particular request is allowed.
type AccessChecker interface {
	// Check if access should be granted or denied. In case of granting access,
	// return nil. In case access is denied, return a non-nil error response,
	// such as Unauthorized("access denied").
	CheckAccess(d *Daemon, r *http.Request, ucred *Ucrednet, user *UserState) Response
}

// OpenAccess allows all requests, including non-local sockets (e.g. TCP)
type OpenAccess struct{}

func (ac OpenAccess) CheckAccess(d *Daemon, r *http.Request, ucred *Ucrednet, user *UserState) Response {
	return nil
}

// AdminAccess allows requests over the UNIX domain socket from the root uid and the current user's uid
type AdminAccess struct{}

func (ac AdminAccess) CheckAccess(d *Daemon, r *http.Request, ucred *Ucrednet, user *UserState) Response {
	// If there is a validated user and they're allowed admin access, let them in.
	if user != nil && user.Access == state.AdminAccess {
		return nil
	}
	// Otherwise they need to be UID 0 (root) or the UID Pebble is running as.
	if ucred != nil && (ucred.Uid == 0 || ucred.Uid == uint32(os.Getuid())) {
		return nil
	}
	return Unauthorized("access denied")
}

// UserAccess allows requests over the UNIX domain socket from any local user
type UserAccess struct{}

func (ac UserAccess) CheckAccess(d *Daemon, r *http.Request, ucred *Ucrednet, user *UserState) Response {
	// If there is a validated user and they're allowed read or admin access, let them in.
	if user != nil && (user.Access == state.ReadAccess || user.Access == state.AdminAccess) {
		return nil
	}
	// Otherwise if it's any local UID user, let them in.
	if ucred != nil {
		return nil
	}
	return Unauthorized("access denied")
}
