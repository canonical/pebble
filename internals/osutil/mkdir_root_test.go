//go:build roottest

// Copyright (c) 2024 Canonical Ltd
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
	"syscall"
	"testing"

	"github.com/canonical/pebble/internals/osutil"
	"github.com/canonical/pebble/internals/osutil/sys"
)

func TestWithRootMakeParentsChmodAndChown(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("requires running as root")
	}

	username := os.Getenv("PEBBLE_TEST_USER")
	group := os.Getenv("PEBBLE_TEST_GROUP")
	if username == "" || group == "" {
		t.Fatalf("must set PEBBLE_TEST_USER and PEBBLE_TEST_GROUP")
	}

	u, err := user.Lookup(username)
	if err != nil {
		t.Fatalf("cannot look up username: %v", err)
	}
	g, err := user.LookupGroup(group)
	if err != nil {
		t.Fatalf("cannot look up group: %v", err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		t.Fatalf("cannot convert uid to int: %v", err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		t.Fatalf("cannot convert gid to int: %v", err)
	}
	tmpDir := t.TempDir()

	err = osutil.Mkdir(tmpDir+"/foo/bar", 0o777, &osutil.MkdirOptions{
		MakeParents: true,
		Chmod:       true,
		Chown:       true,
		UserID:      sys.UserID(uid),
		GroupID:     sys.GroupID(gid),
	})
	if err != nil {
		t.Fatalf(": %v", err)
	}
	if !osutil.IsDir(tmpDir + "/foo") {
		t.Fatalf("file %s is not a directory", tmpDir+"/foo")
	}
	if !osutil.IsDir(tmpDir + "/foo/bar") {
		t.Fatalf("file %s is not a directory", tmpDir+"/foo/bar")
	}

	info, err := os.Stat(tmpDir + "/foo")
	if err != nil {
		t.Fatalf("cannot stat dir %s: %v", tmpDir+"/foo", err)
	}
	if info.Mode().Perm() != os.FileMode(0o777) {
		t.Fatalf("error checking dir %s permission, expected: %v, got: %v", tmpDir+"/foo", 0o777, info.Mode().Perm())
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("syscall stat on dir %s error", tmpDir+"/foo")
	}
	if int(stat.Uid) != uid {
		t.Fatalf("dir %s uid error, expected: %v, got: %v", tmpDir+"/foo", uid, int(stat.Uid))
	}
	if int(stat.Uid) != uid {
		t.Fatalf("dir %s gid error, expected: %v, got: %v", tmpDir+"/foo", gid, int(stat.Gid))
	}

	info, err = os.Stat(tmpDir + "/foo/bar")
	if err != nil {
		t.Fatalf(": %v", err)
	}
	if info.Mode().Perm() != os.FileMode(0o777) {
		t.Fatalf("error checking dir %s permission, expected: %v, got: %v", tmpDir+"/foo/bar", 0o777, info.Mode().Perm())
	}
	stat, ok = info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("syscall stat on dir %s error", tmpDir+"/foo/bar")
	}
	if int(stat.Uid) != uid {
		t.Fatalf("dir %s uid error, expected: %v, got: %v", tmpDir+"/foo/bar", uid, int(stat.Uid))
	}
	if int(stat.Uid) != uid {
		t.Fatalf("dir %s gid error, expected: %v, got: %v", tmpDir+"/foo/bar", gid, int(stat.Gid))
	}
}
