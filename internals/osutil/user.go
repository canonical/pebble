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

package osutil

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"strings"
	"syscall"

	"github.com/canonical/pebble/internals/osutil/sys"
)

var (
	userCurrent     = user.Current
	userLookup      = user.Lookup
	userLookupId    = user.LookupId
	userLookupGroup = user.LookupGroup

	enoentMessage = syscall.ENOENT.Error()
)

// RealUser finds the user behind a sudo invocation when root, if applicable
// and possible.
//
// Don't check SUDO_USER when not root and simply return the current uid
// to properly support sudo'ing from root to a non-root user
func RealUser() (*user.User, error) {
	cur, err := userCurrent()
	if err != nil {
		return nil, err
	}

	// not root, so no sudo invocation we care about
	if cur.Uid != "0" {
		return cur, nil
	}

	realName := os.Getenv("SUDO_USER")
	if realName == "" {
		// not sudo; current is correct
		return cur, nil
	}

	real, err := user.Lookup(realName)
	// can happen when sudo is used to enter a chroot (e.g. pbuilder)
	if _, ok := err.(user.UnknownUserError); ok {
		return cur, nil
	}
	// Workaround for https://github.com/golang/go/issues/67912, until our
	// minimum Go version has a fix for that. In short, user.Lookup sometimes
	// doesn't return UnknownUserError when it should.
	if err != nil && strings.Contains(err.Error(), enoentMessage) {
		return cur, nil
	}
	if err != nil {
		return nil, err
	}

	return real, nil
}

// UidGid returns the uid and gid of the given user, as uint32s
//
// XXX this should go away soon
func UidGid(u *user.User) (sys.UserID, sys.GroupID, error) {
	// XXX this will be wrong for high uids on 32-bit arches (for now)
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return sys.FlagID, sys.FlagID, fmt.Errorf("cannot parse user id %s: %s", u.Uid, err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return sys.FlagID, sys.FlagID, fmt.Errorf("cannot parse group id %s: %s", u.Gid, err)
	}

	return sys.UserID(uid), sys.GroupID(gid), nil
}

// NormalizeUidGid returns the "normalized" UID and GID for the given IDs and
// names. If both uid and username are specified, the username's UID must match
// the given uid (similar for gid and group), otherwise an error is returned.
func NormalizeUidGid(uid, gid *int, username, group string) (*int, *int, error) {
	if uid == nil && username == "" && gid == nil && group == "" {
		return nil, nil, nil
	}
	if username != "" {
		u, err := userLookup(username)
		if err != nil {
			if strings.Contains(err.Error(), enoentMessage) {
				// Better error message to work around https://github.com/golang/go/issues/67912
				return nil, nil, user.UnknownUserError(username)
			}
			return nil, nil, err
		}
		n, _ := strconv.Atoi(u.Uid)
		if uid != nil && *uid != n {
			return nil, nil, fmt.Errorf("user %q UID (%d) does not match user-id (%d)",
				username, n, *uid)
		}
		uid = &n
	}
	if group != "" {
		g, err := userLookupGroup(group)
		if err != nil {
			if strings.Contains(err.Error(), enoentMessage) {
				// Better error message to work around https://github.com/golang/go/issues/67912
				return nil, nil, user.UnknownGroupError(group)
			}
			return nil, nil, err
		}
		n, _ := strconv.Atoi(g.Gid)
		if gid != nil && *gid != n {
			return nil, nil, fmt.Errorf("group %q GID (%d) does not match group-id (%d)",
				group, n, *gid)
		}
		gid = &n
	}
	if gid == nil {
		// Neither gid nor group was specified
		// Either uid or user must have been specified; use user's primary group ID
		uidInfo, err := userLookupId(strconv.Itoa(*uid))
		if err != nil {
			if strings.Contains(err.Error(), enoentMessage) {
				// Better error message to work around https://github.com/golang/go/issues/67912
				return nil, nil, user.UnknownUserIdError(*uid)
			}
			return nil, nil, err
		}
		gidVal, _ := strconv.Atoi(uidInfo.Gid)
		gid = &gidVal
	}
	if uid == nil && gid != nil {
		return nil, nil, fmt.Errorf("must specify user, not just group")
	}
	return uid, gid, nil
}

// IsCurrent reports whether the given user ID and group ID are those of the
// current user.
func IsCurrent(uid, gid int) (bool, error) {
	current, err := userCurrent()
	if err != nil {
		return false, err
	}
	currentUid, err := strconv.Atoi(current.Uid)
	if err != nil {
		return false, err
	}
	currentGid, err := strconv.Atoi(current.Gid)
	if err != nil {
		return false, err
	}
	return uid == currentUid && gid == currentGid, nil
}
