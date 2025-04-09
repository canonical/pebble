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

	"github.com/canonical/pebble/internals/overlord/state"
)

const (
	accessDenied = "access denied"
)

// AccessChecker checks whether a particular request is allowed.
type AccessChecker interface {
	// CheckAccess reports whether access should be granted or denied. If
	// access is granted, return nil. If access is denied, return a non-nil
	// error such as Unauthorized("access denied").
	CheckAccess(d *Daemon, r *http.Request, user *UserState) Response
}

// OpenAccess allows all requests, including non-local sockets (for example, TCP).
type OpenAccess struct{}

func (ac OpenAccess) CheckAccess(d *Daemon, r *http.Request, user *UserState) Response {
	return nil
}

// AdminAccess allows requests over the unix domain socket from the root UID
// and the current user's UID.
type AdminAccess struct{}

func (ac AdminAccess) CheckAccess(d *Daemon, r *http.Request, user *UserState) Response {
	if user == nil {
		return Unauthorized(accessDenied)
	}
	if user.Access == state.AdminAccess {
		return nil
	}
	// An identity explicitly set to "access: read" or "access: untrusted" isn't allowed.
	return Unauthorized(accessDenied)
}

// UserAccess allows requests over the UNIX domain socket from any local user
type UserAccess struct{}

func (ac UserAccess) CheckAccess(d *Daemon, r *http.Request, user *UserState) Response {
	if user == nil {
		return Unauthorized(accessDenied)
	}
	switch user.Access {
	case state.ReadAccess, state.AdminAccess:
		return nil
	}
	// An identity explicitly set to "access: untrusted" isn't allowed.
	return Unauthorized(accessDenied)
}

// MetricsAccess allows requests over HTTP from authenticated users.
type MetricsAccess struct{}

func (ac MetricsAccess) CheckAccess(d *Daemon, r *http.Request, user *UserState) Response {
	if user == nil {
		return Unauthorized(accessDenied)
	}
	switch user.Access {
	case state.MetricsAccess, state.ReadAccess, state.AdminAccess:
		return nil
	default:
		// All other access levels, including "access: untrusted", are denied.
		return Unauthorized(accessDenied)
	}
}

// IdentityWriteAccess is a custom access checker only intended for the indentity endpoint. The
// default behaviour is the same as the AdminAccess checker. However, it adds a special mode for
// write requests over HTTPS, where it allows the daemon to enable open access for a carefully
// controlled enrollment period, used during an encrypted client-server pairing process.
type IdentityWriteAccess struct{}

var identityEnrollmentActive = (*Daemon).identityEnrollmentActive

func (ac IdentityWriteAccess) CheckAccess(d *Daemon, r *http.Request, user *UserState) Response {
	// This checker is only for identities.
	if r.URL.Path != "/v1/identities" {
		return Unauthorized(accessDenied)
	}

	// Any identity write attempt always closes an active enrollment window. The
	// enrollment window is a carefully controlled opportunity for an client
	// without an identity to register a new identity with the server. If this
	// window is active for any other identities write request, it will be seen
	// as an abuse of the intended purpose, and as a result the widnow will also
	// be closed immediately.
	enrollmentActive := identityEnrollmentActive(d)

	// Zero value is requestSrcUnknown.
	source, _ := r.Context().Value(requestSrcCtxKey).(requestSrc)

	// The identity enrollment will only proceed if the client provided no
	// credentials for this request, and if the connection is HTTPS.
	if user == nil && source == requestSrcHTTPS && enrollmentActive {
		// Identity enrollment window is active.
		return nil
	}

	// If the user has admin, that is OK.
	if user != nil && user.Access == state.AdminAccess {
		return nil
	}

	// All other access levels, including "access: untrusted", are denied.
	return Unauthorized(accessDenied)
}
