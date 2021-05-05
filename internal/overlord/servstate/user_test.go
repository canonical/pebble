// Copyright (c) 2021 Canonical Ltd
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

package servstate

import (
	"os"
	"os/user"
	"strconv"
	"testing"

	. "gopkg.in/check.v1"
)

func TestUser(t *testing.T) {
	TestingT(t)
}

type UserSuite struct{}

var _ = Suite(&UserSuite{})

func (s *UserSuite) TestGetUidGid(c *C) {
	checkGetUidGid(c, "root", "root", 0, 0, "")
	checkGetUidGid(c, "0", "0", 0, 0, "")
	checkGetUidGid(c, "", "", 0, 0, "")
	checkGetUidGid(c, "root", "", 0, 0, "")

	checkGetUidGid(c, strconv.Itoa(os.Getuid()), "", os.Getuid(), os.Getgid(), "")
	checkGetUidGid(c, strconv.Itoa(os.Getuid()), strconv.Itoa(os.Getgid()),
		os.Getuid(), os.Getgid(), "")

	u, err := user.Lookup("nobody")
	c.Check(err, IsNil)
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	checkGetUidGid(c, "nobody", "nogroup", uid, gid, "")

	checkGetUidGid(c, "", "root", 0, 0, `group "root" specified without user`)
	checkGetUidGid(c, "foobar", "", 0, 0, `user or UID "foobar" not found`)
	checkGetUidGid(c, "nobody", "boofar", 0, 0, `group or GID "boofar" not found`)
}

func checkGetUidGid(c *C, user, group string, uid, gid int, error string) {
	u, g, err := getUidGid(user, group)
	if error == "" {
		c.Check(err, IsNil)
	} else {
		c.Check(err, ErrorMatches, error)
	}
	c.Check(u, Equals, uid)
	c.Check(g, Equals, gid)
}
