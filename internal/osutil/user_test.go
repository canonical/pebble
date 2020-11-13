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

package osutil_test

import (
	"os"
	"os/user"
	"strconv"

	"gopkg.in/check.v1"

	"github.com/canonical/pebble/internal/osutil"
	"github.com/canonical/pebble/internal/osutil/sys"
	"github.com/canonical/pebble/internal/testutil"
)

type createUserSuite struct {
	testutil.BaseTest

	restorer func()
}

var _ = check.Suite(&createUserSuite{})

func (s *createUserSuite) SetUpTest(c *check.C) {
}

func (s *createUserSuite) TearDownTest(c *check.C) {
}

func (s *createUserSuite) TestRealUser(c *check.C) {
	oldUser := os.Getenv("SUDO_USER")
	defer func() { os.Setenv("SUDO_USER", oldUser) }()

	for _, t := range []struct {
		SudoUsername    string
		CurrentUsername string
		CurrentUid      int
	}{
		// simulate regular "root", no SUDO_USER set
		{"", os.Getenv("USER"), 0},
		// simulate a normal sudo invocation
		{"guy", "guy", 0},
		// simulate running "sudo -u some-user -i" as root
		// (LP: #1638656)
		{"root", os.Getenv("USER"), 1000},
	} {
		restore := osutil.FakeUserCurrent(func() (*user.User, error) {
			return &user.User{
				Username: t.CurrentUsername,
				Uid:      strconv.Itoa(t.CurrentUid),
			}, nil
		})
		defer restore()

		os.Setenv("SUDO_USER", t.SudoUsername)
		cur, err := osutil.RealUser()
		c.Assert(err, check.IsNil)
		c.Check(cur.Username, check.Equals, t.CurrentUsername)
	}
}

func (s *createUserSuite) TestUidGid(c *check.C) {
	for k, t := range map[string]struct {
		User *user.User
		Uid  sys.UserID
		Gid  sys.GroupID
		Err  string
	}{
		"happy":   {&user.User{Uid: "10", Gid: "10"}, 10, 10, ""},
		"bad uid": {&user.User{Uid: "x", Gid: "10"}, sys.FlagID, sys.FlagID, "cannot parse user id x"},
		"bad gid": {&user.User{Uid: "10", Gid: "x"}, sys.FlagID, sys.FlagID, "cannot parse group id x"},
	} {
		uid, gid, err := osutil.UidGid(t.User)
		c.Check(uid, check.Equals, t.Uid, check.Commentf(k))
		c.Check(gid, check.Equals, t.Gid, check.Commentf(k))
		if t.Err == "" {
			c.Check(err, check.IsNil, check.Commentf(k))
		} else {
			c.Check(err, check.ErrorMatches, ".*"+t.Err+".*", check.Commentf(k))
		}
	}
}
