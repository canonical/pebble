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
	"fmt"
	"os"
	"os/user"
	"strconv"
	"testing"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/internals/osutil"
	"github.com/canonical/pebble/internals/osutil/sys"
	"github.com/canonical/pebble/internals/testutil"
)

type userSuite struct {
	testutil.BaseTest
}

func TestUserSuite(t *testing.T) {
	tc.Run(t, &userSuite{})
}

func (s *userSuite) SetUpTest(c *tc.C) {
}

func (s *userSuite) TearDownTest(c *tc.C) {
}

func (s *userSuite) TestRealUser(c *tc.C) {
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
		c.Assert(err, tc.ErrorIsNil)
		c.Check(cur.Username, tc.Equals, t.CurrentUsername)
	}
}

func (s *userSuite) TestUidGid(c *tc.C) {
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
		c.Check(uid, tc.Equals, t.Uid, tc.Commentf(k))
		c.Check(gid, tc.Equals, t.Gid, tc.Commentf(k))
		if t.Err == "" {
			c.Assert(err, tc.ErrorIsNil, tc.Commentf(k))
		} else {
			c.Check(err, tc.ErrorMatches, ".*"+t.Err+".*", tc.Commentf(k))
		}
	}
}

func (s *userSuite) TestNormalizeUidGid(c *tc.C) {
	test := func(uid, gid *int, username, group string, expectedUid, expectedGid *int, errMatch string) {
		uid, gid, err := osutil.NormalizeUidGid(uid, gid, username, group)
		if err != nil {
			c.Check(err, tc.ErrorMatches, errMatch)
		} else {
			c.Check(errMatch, tc.Equals, "")
		}
		c.Check(uid, tc.DeepEquals, expectedUid)
		c.Check(gid, tc.DeepEquals, expectedGid)
	}
	ptr := func(n int) *int {
		return &n
	}

	var userErr error
	restoreUser := osutil.FakeUserLookup(func(name string) (*user.User, error) {
		c.Check(name, tc.Equals, "USER")
		return &user.User{Uid: "10", Gid: "20"}, userErr
	})
	defer restoreUser()

	var userIdErr error
	restoreUserId := osutil.FakeUserLookupId(func(uid string) (*user.User, error) {
		c.Check(uid, tc.Equals, "10")
		return &user.User{Uid: "10", Gid: "20"}, userIdErr
	})
	defer restoreUserId()

	var groupErr error
	restoreGroup := osutil.FakeUserLookupGroup(func(name string) (*user.Group, error) {
		c.Check(name, tc.Equals, "GROUP")
		return &user.Group{Gid: "30"}, groupErr
	})
	defer restoreGroup()

	test(nil, nil, "", "", nil, nil, "")
	test(nil, nil, "", "GROUP", nil, nil, "must specify user, not just group")
	test(nil, nil, "USER", "", ptr(10), ptr(20), "")
	test(ptr(10), nil, "", "", ptr(10), ptr(20), "")
	test(nil, nil, "USER", "GROUP", ptr(10), ptr(30), "")

	test(nil, ptr(2), "", "", nil, nil, "must specify user, not just group")
	test(nil, ptr(2), "", "GROUP", nil, nil, `group "GROUP" GID \(30\) does not match group-id \(2\)`)
	test(nil, ptr(2), "USER", "", ptr(10), ptr(2), "")
	test(nil, ptr(2), "USER", "GROUP", nil, nil, `group "GROUP" GID \(30\) does not match group-id \(2\)`)

	test(ptr(1), nil, "", "GROUP", ptr(1), ptr(30), "")
	test(ptr(1), nil, "USER", "", nil, nil, `user "USER" UID \(10\) does not match user-id \(1\)`)
	test(ptr(1), nil, "USER", "GROUP", nil, nil, `user "USER" UID \(10\) does not match user-id \(1\)`)

	test(ptr(1), ptr(2), "", "", ptr(1), ptr(2), "")
	test(ptr(1), ptr(2), "", "GROUP", nil, nil, `group "GROUP" GID \(30\) does not match group-id \(2\)`)
	test(ptr(1), ptr(2), "USER", "", nil, nil, `user "USER" UID \(10\) does not match user-id \(1\)`)
	test(ptr(1), ptr(2), "USER", "GROUP", nil, nil, `user "USER" UID \(10\) does not match user-id \(1\)`)

	userErr = fmt.Errorf("USER ERROR!")
	test(nil, nil, "USER", "", nil, nil, "USER ERROR!")
	groupErr = fmt.Errorf("GROUP ERROR!")
	test(ptr(1), nil, "", "GROUP", nil, nil, "GROUP ERROR!")
}

func (s *userSuite) TestIsCurrent(c *tc.C) {
	isCurrent, err := osutil.IsCurrent(os.Getuid(), os.Getgid())
	c.Assert(err, tc.ErrorIsNil)
	c.Check(isCurrent, tc.Equals, true)

	// Different uid and gid
	restore := osutil.FakeUserCurrent(func() (*user.User, error) {
		return &user.User{
			Uid: strconv.Itoa(os.Getuid() + 1),
			Gid: strconv.Itoa(os.Getgid() + 1),
		}, nil
	})
	defer restore()
	isCurrent, err = osutil.IsCurrent(os.Getuid(), os.Getpid())
	c.Assert(err, tc.ErrorIsNil)
	c.Check(isCurrent, tc.Equals, false)

	// Different uid only
	_ = osutil.FakeUserCurrent(func() (*user.User, error) {
		return &user.User{
			Uid: strconv.Itoa(os.Getuid() + 1),
			Gid: strconv.Itoa(os.Getgid()),
		}, nil
	})
	isCurrent, err = osutil.IsCurrent(os.Getuid(), os.Getpid())
	c.Assert(err, tc.ErrorIsNil)
	c.Check(isCurrent, tc.Equals, false)

	// Different gid only
	_ = osutil.FakeUserCurrent(func() (*user.User, error) {
		return &user.User{
			Uid: strconv.Itoa(os.Getuid()),
			Gid: strconv.Itoa(os.Getgid() + 1),
		}, nil
	})
	isCurrent, err = osutil.IsCurrent(os.Getuid(), os.Getgid())
	c.Assert(err, tc.ErrorIsNil)
	c.Check(isCurrent, tc.Equals, false)
}
