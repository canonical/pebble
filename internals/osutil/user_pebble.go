// Copyright (C) 2014-2020 Canonical Ltd
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
	osuser "os/user"
	"strconv"

	"github.com/canonical/pebble/internals/osutil/user"
)

var (
	userLookupId    = user.LookupId
	userLookupGroup = user.LookupGroup
)

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
			if isUnknownUserOrEnoent(err) {
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
			if isUnknownUserOrEnoent(err) {
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
			if isUnknownUserOrEnoent(err) {
				// Better error message to work around https://github.com/golang/go/issues/67912
				return nil, nil, osuser.UnknownUserIdError(*uid)
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
