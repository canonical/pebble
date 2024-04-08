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
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/canonical/pebble/internals/osutil/sys"
)

// MkdirFlags are a bitfield of flags for the Mkdir* functions.
type MkdirFlags uint

const (
	// MkdirChmod performs an explicit chmod to directory permissions after
	// creation to fix any umask modifications.
	MkdirChmod MkdirFlags = 1 << iota
)

// XXX: we need to come back and fix this; this is a hack to unblock us.
// Have a lock so that if one goroutine tries to mkdirallchown /foo/bar, and
// another tries to mkdirallchown /foo/baz, they can't both decide they need
// to make /foo and then have one fail.
var mu sync.Mutex

// MkdirAllChown is like os.MkdirAll but it calls os.Chown on any
// directories it creates.
func MkdirAllChown(path string, perm os.FileMode, flags MkdirFlags, uid sys.UserID, gid sys.GroupID) error {
	mu.Lock()
	defer mu.Unlock()
	if uid == NoChown && gid == NoChown {
		if flags&MkdirChmod != 0 {
			return mkdirAllChmod(filepath.Clean(path), perm)
		} else {
			return os.MkdirAll(path, perm)
		}
	}
	return mkdirAllChown(filepath.Clean(path), perm, flags, uid, gid)
}

func mkdirAllChmod(path string, perm os.FileMode) error {
	if s, err := os.Stat(path); err == nil {
		if s.IsDir() {
			return nil
		}

		// emulate os.MkdirAll
		return &os.PathError{
			Op:   "mkdir",
			Path: path,
			Err:  syscall.ENOTDIR,
		}
	}

	parent := filepath.Dir(path)
	if parent != "/" {
		if err := mkdirAllChmod(parent, perm); err != nil {
			return err
		}
	}

	return mkdirChown(path, perm, MkdirChmod, NoChown, NoChown)
}

func mkdirAllChown(path string, perm os.FileMode, flags MkdirFlags, uid sys.UserID, gid sys.GroupID) error {
	// split out so filepath.Clean isn't called twice for each inner path
	if s, err := os.Stat(path); err == nil {
		if s.IsDir() {
			return nil
		}

		// emulate os.MkdirAll
		return &os.PathError{
			Op:   "mkdir",
			Path: path,
			Err:  syscall.ENOTDIR,
		}
	}

	dir := filepath.Dir(path)
	if dir != "/" {
		if err := mkdirAllChown(dir, perm, flags, uid, gid); err != nil {
			return err
		}
	}

	return mkdirChown(path, perm, flags, uid, gid)
}

func mkdirChown(path string, perm os.FileMode, flags MkdirFlags, uid sys.UserID, gid sys.GroupID) error {
	cand := path + ".mkdir-new"

	if err := os.Mkdir(cand, perm); err != nil && !os.IsExist(err) {
		return err
	}

	if err := sys.ChownPath(cand, uid, gid); err != nil {
		return err
	}

	if err := os.Rename(cand, path); err != nil {
		return err
	}

	if flags&MkdirChmod != 0 {
		if err := os.Chmod(path, perm); err != nil {
			return err
		}
	}

	fd, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer fd.Close()

	return fd.Sync()
}

// MkdirChown is like os.Mkdir but it also calls os.Chown on the directory it
// creates.
func MkdirChown(path string, perm os.FileMode, flags MkdirFlags, uid sys.UserID, gid sys.GroupID) error {
	mu.Lock()
	defer mu.Unlock()
	if uid == NoChown && gid == NoChown {
		err := os.Mkdir(path, perm)
		if err != nil {
			return err
		}
		if flags&MkdirChmod != 0 {
			return os.Chmod(path, perm)
		}
		return nil
	}
	return mkdirChown(filepath.Clean(path), perm, flags, uid, gid)
}
