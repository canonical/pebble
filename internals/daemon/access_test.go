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

	// But not ReadAccess or UntrustedAccess
	user := &daemon.UserState{Access: state.ReadAccess}
	c.Check(ac.CheckAccess(nil, nil, user), DeepEquals, errUnauthorized)
	user = &daemon.UserState{Access: state.UntrustedAccess}
	c.Check(ac.CheckAccess(nil, nil, user), DeepEquals, errUnauthorized)
}
