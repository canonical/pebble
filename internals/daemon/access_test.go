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

package daemon_test

import (
	"context"
	"net/http"
	"net/url"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/daemon"
	"github.com/canonical/pebble/internals/overlord/state"
)

type accessSuite struct{}

var _ = Suite(&accessSuite{})

var errUnauthorized = daemon.Unauthorized("access denied")

func (s *accessSuite) TestOpenAccess(c *C) {
	var ac daemon.AccessChecker = daemon.OpenAccess{}

	// OpenAccess allows access without peer credentials.
	c.Check(ac.CheckAccess(nil, nil, nil), IsNil)

	// User with "access: admin|read|untrusted" is granted access
	user := &daemon.UserState{Access: state.AdminAccess}
	c.Check(ac.CheckAccess(nil, nil, user), IsNil)
	user = &daemon.UserState{Access: state.ReadAccess}
	c.Check(ac.CheckAccess(nil, nil, user), IsNil)
	user = &daemon.UserState{Access: state.UntrustedAccess}
	c.Check(ac.CheckAccess(nil, nil, user), IsNil)
}

func (s *accessSuite) TestUserAccess(c *C) {
	var ac daemon.AccessChecker = daemon.UserAccess{}

	// UserAccess denies access without peer credentials.
	c.Check(ac.CheckAccess(nil, nil, nil), DeepEquals, errUnauthorized)

	// User with "access: admin|read" is granted access
	user := &daemon.UserState{Access: state.AdminAccess}
	c.Check(ac.CheckAccess(nil, nil, user), IsNil)
	user = &daemon.UserState{Access: state.ReadAccess}
	c.Check(ac.CheckAccess(nil, nil, user), IsNil)

	// But not UntrustedAccess
	user = &daemon.UserState{Access: state.UntrustedAccess}
	c.Check(ac.CheckAccess(nil, nil, user), DeepEquals, errUnauthorized)
}

func (s *accessSuite) TestAdminAccess(c *C) {
	var ac daemon.AccessChecker = daemon.AdminAccess{}

	// AdminAccess denies access without peer credentials.
	c.Check(ac.CheckAccess(nil, nil, nil), DeepEquals, errUnauthorized)

	// ReadAccess or UntrustedAccess always denies access.
	user := &daemon.UserState{Access: state.ReadAccess}
	c.Check(ac.CheckAccess(nil, nil, user), DeepEquals, errUnauthorized)
	user = &daemon.UserState{Access: state.UntrustedAccess}
	c.Check(ac.CheckAccess(nil, nil, user), DeepEquals, errUnauthorized)
}

func (s *accessSuite) TestIdentityWriteAccess(c *C) {
	// Enrollment window state
	activeEnrollment := false

	restore := daemon.FakeIdentEnrollmentActive(func(d *daemon.Daemon) bool {
		previous := activeEnrollment
		activeEnrollment = false
		return previous
	})
	defer restore()

	var ac daemon.AccessChecker = daemon.IdentityWriteAccess{}

	// If the end-point is not identities, this checker cannot be used.
	r := &http.Request{
		URL: &url.URL{},
	}
	r = r.WithContext(context.WithValue(context.Background(), daemon.RequestSrcCtxKey, daemon.RequestSrcUnknown))
	user := &daemon.UserState{Access: state.AdminAccess}
	c.Check(ac.CheckAccess(nil, r, user), DeepEquals, errUnauthorized)

	// If the end-point is identities, admin works fine.
	r = &http.Request{
		URL: &url.URL{
			Path: "/v1/identities",
		},
	}
	r = r.WithContext(context.WithValue(context.Background(), daemon.RequestSrcCtxKey, daemon.RequestSrcUnknown))
	user = &daemon.UserState{Access: state.AdminAccess}
	activeEnrollment = true
	c.Check(ac.CheckAccess(nil, r, user), IsNil)
	// Make sure a normal access also closes the enrollment window.
	c.Check(activeEnrollment, Equals, false)

	// ReadAccess is not authorized.
	user = &daemon.UserState{Access: state.ReadAccess}
	activeEnrollment = true
	c.Check(ac.CheckAccess(nil, r, user), DeepEquals, errUnauthorized)
	// Must also close the enrollment window.
	c.Check(activeEnrollment, Equals, false)

	// UntrustedAccess is not authorized.
	user = &daemon.UserState{Access: state.UntrustedAccess}
	activeEnrollment = true
	c.Check(ac.CheckAccess(nil, r, user), DeepEquals, errUnauthorized)
	// Must also close the enrollment window.
	c.Check(activeEnrollment, Equals, false)

	// No user is not authorized.
	activeEnrollment = false
	c.Check(ac.CheckAccess(nil, r, nil), DeepEquals, errUnauthorized)

	// No user is allows only during the enrollment window, but not without HTTPS.
	r = &http.Request{
		URL: &url.URL{
			Path: "/v1/identities",
		},
	}
	r = r.WithContext(context.WithValue(context.Background(), daemon.RequestSrcCtxKey, daemon.RequestSrcUnknown))
	activeEnrollment = true
	c.Check(ac.CheckAccess(nil, r, nil), DeepEquals, errUnauthorized)
	// Must also close the enrollment window.
	c.Check(activeEnrollment, Equals, false)

	// No user is allows only during the enrollment window, but not without HTTPS.
	r = &http.Request{
		URL: &url.URL{
			Path: "/v1/identities",
		},
	}
	r = r.WithContext(context.WithValue(context.Background(), daemon.RequestSrcCtxKey, daemon.RequestSrcHTTPS))
	activeEnrollment = true
	c.Check(ac.CheckAccess(nil, r, nil), IsNil)
	// Must also close the enrollment window.
	c.Check(activeEnrollment, Equals, false)
}
