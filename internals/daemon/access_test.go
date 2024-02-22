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
	"os"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/daemon"
)

type accessSuite struct {
}

var _ = Suite(&accessSuite{})

var errUnauthorized = daemon.Unauthorized("access denied")

func (s *accessSuite) TestOpenAccess(c *C) {
	var ac daemon.AccessChecker = daemon.OpenAccess{}

	// OpenAccess allows access without peer credentials.
	c.Check(ac.CheckAccess(nil, nil, nil, nil), IsNil)

	// OpenAccess allows access from normal user
	ucred := &daemon.Ucrednet{Uid: 42, Pid: 100}
	c.Check(ac.CheckAccess(nil, nil, ucred, nil), IsNil)

	// OpenAccess allows access from root user
	ucred = &daemon.Ucrednet{Uid: 0, Pid: 100}
	c.Check(ac.CheckAccess(nil, nil, ucred, nil), IsNil)
}

func (s *accessSuite) TestUserAccess(c *C) {
	var ac daemon.AccessChecker = daemon.UserAccess{}

	// UserAccess denies access without peer credentials.
	c.Check(ac.CheckAccess(nil, nil, nil, nil), DeepEquals, errUnauthorized)

	// UserAccess allows access from root user
	ucred := &daemon.Ucrednet{Uid: 0, Pid: 100}
	c.Check(ac.CheckAccess(nil, nil, ucred, nil), IsNil)

	// UserAccess allows access form normal user
	ucred = &daemon.Ucrednet{Uid: 42, Pid: 100}
	c.Check(ac.CheckAccess(nil, nil, ucred, nil), IsNil)
}

func (s *accessSuite) TestAdminAccess(c *C) {
	var ac daemon.AccessChecker = daemon.AdminAccess{}

	// AdminAccess denies access without peer credentials.
	c.Check(ac.CheckAccess(nil, nil, nil, nil), DeepEquals, errUnauthorized)

	// Current user's UID
	uid := uint32(os.Getuid())

	// Non-root users that are different from the current user are forbidden
	ucred := &daemon.Ucrednet{Uid: uid + 1, Pid: 100}
	c.Check(ac.CheckAccess(nil, nil, ucred, nil), DeepEquals, errUnauthorized)

	// The current user is granted access
	ucred = &daemon.Ucrednet{Uid: uid, Pid: 100}
	c.Check(ac.CheckAccess(nil, nil, ucred, nil), IsNil)

	// Root is granted access
	ucred = &daemon.Ucrednet{Uid: 0, Pid: 100}
	c.Check(ac.CheckAccess(nil, nil, ucred, nil), IsNil)
}
