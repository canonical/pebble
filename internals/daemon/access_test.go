// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

// Based on: https://github.com/snapcore/snapd/blob/master/daemon/access.go

package daemon_test

import (
	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/daemon"
)

type accessSuite struct {
}

var _ = Suite(&accessSuite{})

var (
	errForbidden    = daemon.StatusForbidden("access denied")
	errUnauthorized = daemon.StatusUnauthorized("access denied")
)

const (
	socketPath = "/tmp/foo.sock"
)

func (s *accessSuite) TestOpenAccess(c *C) {
	var ac daemon.AccessChecker = daemon.OpenAccess{}

	// openAccess allows access without peer credentials.
	c.Check(ac.CheckAccess(nil, nil, nil, nil), IsNil)

	// openAccess allows access from normal user
	ucred := &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: socketPath}
	c.Check(ac.CheckAccess(nil, nil, ucred, nil), IsNil)

	// openAccess allows access from root user
	ucred = &daemon.Ucrednet{Uid: 0, Pid: 100, Socket: socketPath}
	c.Check(ac.CheckAccess(nil, nil, ucred, nil), IsNil)
}

func (s *accessSuite) TestUserAccess(c *C) {
	var ac daemon.AccessChecker = daemon.UserAccess{}

	// userAccess denies access without peer credentials.
	c.Check(ac.CheckAccess(nil, nil, nil, nil), DeepEquals, errForbidden)

	// userAccess allows access from root user
	ucred := &daemon.Ucrednet{Uid: 0, Pid: 100, Socket: socketPath}
	c.Check(ac.CheckAccess(nil, nil, ucred, nil), IsNil)

	// userAccess allows access form normal user
	ucred = &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: socketPath}
	c.Check(ac.CheckAccess(nil, nil, ucred, nil), IsNil)
}

func (s *accessSuite) TestRootAccess(c *C) {
	var ac daemon.AccessChecker = daemon.RootAccess{}

	// rootAccess denies access without peer credentials.
	c.Check(ac.CheckAccess(nil, nil, nil, nil), DeepEquals, errForbidden)

	// Non-root users are forbidden
	ucred := &daemon.Ucrednet{Uid: 42, Pid: 100, Socket: socketPath}
	c.Check(ac.CheckAccess(nil, nil, ucred, nil), DeepEquals, errForbidden)

	// Root is granted access
	ucred = &daemon.Ucrednet{Uid: 0, Pid: 100, Socket: socketPath}
	c.Check(ac.CheckAccess(nil, nil, ucred, nil), IsNil)
}
