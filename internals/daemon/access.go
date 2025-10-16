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

// OpenAccess allows all incoming requests over unix domain sockets, HTTP
// and HTTPS, even without user credentials (or invalid credentials).
type OpenAccess struct{}

func (ac OpenAccess) CheckAccess(d *Daemon, r *http.Request, user *UserState) Response {
	return nil
}

// AdminAccess only allows incoming requests over unix domain sockets and
// HTTPS, and only if the user is valid and has AdminAccess role.
type AdminAccess struct{}

func (ac AdminAccess) CheckAccess(d *Daemon, r *http.Request, user *UserState) Response {
	if user == nil {
		return Unauthorized(accessDenied)
	}
	if !RequestTransportType(r).IsConcealed() {
		// Not Unix Domain Socket or HTTPS.
		return Unauthorized(accessDenied)
	}
	if user.Access == state.AdminAccess {
		return nil
	}
	// An identity explicitly set to "access: read" or "access: untrusted" isn't allowed.
	return Unauthorized(accessDenied)
}

// UserAccess only allows incoming requests over unix domain sockets and
// HTTPS, and only if the user is valid and has the ReadAccess or
// AdminAccess role.
type UserAccess struct{}

func (ac UserAccess) CheckAccess(d *Daemon, r *http.Request, user *UserState) Response {
	if user == nil {
		return Unauthorized(accessDenied)
	}
	if !RequestTransportType(r).IsConcealed() {
		// Not Unix Domain Socket or HTTPS.
		return Unauthorized(accessDenied)
	}
	switch user.Access {
	case state.ReadAccess, state.AdminAccess:
		return nil
	}
	// An identity explicitly set to "access: untrusted" isn't allowed.
	return Unauthorized(accessDenied)
}

// MetricsAccess allows incoming requests over unix domain sockets, HTTP and
// HTTPS. In the case of unix domain sockets and HTTPS, access is granted if
// the user is valid and has the MetricsAccess, ReadAccess or AdminAccess
// role. If HTTP is used, access is only available for a valid user with
// the MetricsAccess user role (to restrict the credentials we are exposing
// over the clear text channel).
type MetricsAccess struct{}

func (ac MetricsAccess) CheckAccess(d *Daemon, r *http.Request, user *UserState) Response {
	if user == nil {
		return Unauthorized(accessDenied)
	}
	// HTTP access (only basic auth is possible here, so no need to
	// check with identity type).
	transport := RequestTransportType(r)
	if transport == TransportTypeHTTP && user.Access == state.MetricsAccess {
		return nil
	}
	if !transport.IsConcealed() {
		// Not Unix Domain Socket or HTTPS.
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

// pairingWindowEnabled simplifies testing without a pairing manager.
var pairingWindowEnabled = (*Daemon).pairingWindowEnabled

// PairingAccess is only intended for use as an access checker for the pairing
// endpoint. This access checker allows a new mTLS client identity to be
// forwarded to the pairing manager, without identity verification. This access
// checker will only allow pairing requests while the pairing manager has its
// pairing window enabled, which typically involves a proof of server ownership
// procedure, such as a controlled power cycle or button press.
type PairingAccess struct{}

func (ac PairingAccess) CheckAccess(d *Daemon, r *http.Request, user *UserState) Response {
	// This checker is only for pairing.
	if r.URL.Path != "/v1/pairing" {
		return Unauthorized(accessDenied)
	}

	// We only support pairing an mTLS client certificate at this point, so
	// the transport has to be HTTPS.
	if RequestTransportType(r) != TransportTypeHTTPS {
		return Unauthorized(accessDenied)
	}

	if pairingWindowEnabled(d) {
		// Only permit a pairing request during an open pairing window.
		//
		// Note that this is not the final decision on whether this
		// request will succeed. This check is simply a sanity check
		// that prevents forwarding the pairing request to the
		// manager unnecessarily. The final check is made inside the
		// pairing manager where all incoming requests will be
		// serialized, and only the first request will be accepted,
		// after which the pairing window will be closed.
		return nil
	}

	return Unauthorized(accessDenied)
}
