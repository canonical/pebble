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
	"fmt"
	"os/user"
	"strconv"
)

func getUidGid(userStr, groupStr string) (int, int, error) {
	if userStr == "" {
		if groupStr != "" {
			return 0, 0, fmt.Errorf("group %q specified without user", groupStr)
		}
		return 0, 0, nil // No user or group is okay (means use current user)
	}
	uid, gid, err := lookupUserOrUid(userStr)
	if err != nil {
		return 0, 0, err
	}
	// Default GID is primary group ID of service's "user" field
	if groupStr != "" {
		gid, err = lookupGroupOrGid(groupStr)
		if err != nil {
			return 0, 0, err
		}
	}
	return uid, gid, nil
}

// Lookup user or UID, ensure it exists, and return the user ID and its group ID.
func lookupUserOrUid(s string) (int, int, error) {
	u, err := user.Lookup(s)
	if err != nil {
		u, err = user.LookupId(s)
		if err != nil {
			return 0, 0, fmt.Errorf("user or UID %q not found", s)
		}
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	return uid, gid, nil
}

// Lookup group and GID, ensure it exists, and return the group ID.
func lookupGroupOrGid(s string) (int, error) {
	g, err := user.LookupGroup(s)
	if err != nil {
		g, err = user.LookupGroupId(s)
		if err != nil {
			return 0, fmt.Errorf("group or GID %q not found", s)
		}
	}
	gid, _ := strconv.Atoi(g.Gid)
	return gid, nil
}
