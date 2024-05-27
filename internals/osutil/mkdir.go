// Copyright (c) 2014-2024 Canonical Ltd
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

// XXX: we need to come back and fix this; this is a hack to unblock us.
// Have a lock so that if one goroutine tries to mkdirallchown /foo/bar, and
// another tries to mkdirallchown /foo/baz, they can't both decide they need
// to make /foo and then have one fail.
var mu sync.Mutex

// MkdirOptions is a struct of options used for Mkdir().
type MkdirOptions struct {
	// If false (default), a missing parent raises an error.
	// If true, any missing parents of this path are created as needed.
	MakeParents bool

	// If false (default), an error is raised if the target directory already exists.
	// In case MakeParents is true but ExistOK is false, an error won't be raised if
	// the parent directory already exists but the target directory doesn't.
	//
	// If true, an error won't be raised unless the given path already exists in the
	// file system and isn't a directory (same behaviour as the POSIX mkdir -p command).
	ExistOK bool

	// If false (default), no explicit chmod is performed. In this case, the permission
	// of the created directories will be affected by umask settings.
	//
	// If true, perform an explicit chmod on any directories created.
	Chmod bool

	// If false (default), no explicit chown is performed.
	// If true, perform an explicit chown on any directories created, using the UserID
	// and GroupID provided.
	Chown bool

	UserID sys.UserID

	GroupID sys.GroupID
}

// Mkdir creates directories; depending on MkdirOptions.MakeParents, it is like os.Mkdir
// or os.MkdirAll. You can set the option MkdirOptions.Chmod to perform an explicit
// chmod on directories it creates so that the permissions won't be affected by umask
// settings. You can also set the option MkdirOptions.Chmod (and together with UserID,
// GroupId) to perform an explicit chown on newly created directories.
func Mkdir(path string, perm os.FileMode, options *MkdirOptions) error {
	mu.Lock()
	defer mu.Unlock()

	path = filepath.Clean(path)

	// if path already exists
	if s, err := os.Stat(path); err == nil {
		// If path exists but not as a directory, raise a "not a directory" error.
		if !s.IsDir() {
			return &os.PathError{
				Op:   "mkdir",
				Path: path,
				Err:  syscall.ENOTDIR,
			}
		}

		// If path exists as a directory, and ExistOK is set in options, return.
		if options != nil && options.ExistOK {
			return nil
		}

		// If path exists but ExistOK isn't set in options, raise a "file exists" error.
		return &os.PathError{
			Op:   "mkdir",
			Path: path,
			Err:  syscall.EEXIST,
		}
	}

	// If path doesn't exist, create it.
	return mkdirAll(path, perm, options)
}

// create directories recursively
func mkdirAll(path string, perm os.FileMode, options *MkdirOptions) error {
	// if path exists
	if s, err := os.Stat(path); err == nil {
		if s.IsDir() {
			return nil
		}

		// If path exists but not as a directory, raise a "not a directory" error.
		return &os.PathError{
			Op:   "mkdir",
			Path: path,
			Err:  syscall.ENOTDIR,
		}
	}

	// If path doesn't exist, and MakeParents is specified in options,
	// create all directories recursively.
	if options != nil && options.MakeParents {
		parent := filepath.Dir(path)
		if parent != "/" {
			if err := mkdirAll(parent, perm, options); err != nil {
				return err
			}
		}
	}

	// If path doesn't exist, and MakeParents isn't specified in options,
	// create a single directory.
	return mkdir(path, perm, options)
}

// Create a single directory and perform chmod/chown operations according to options.
func mkdir(path string, perm os.FileMode, options *MkdirOptions) error {
	cand := path + ".mkdir-new"

	if err := os.Mkdir(cand, perm); err != nil && !os.IsExist(err) {
		return err
	}

	if options != nil && options.Chown {
		if err := sys.ChownPath(cand, options.UserID, options.GroupID); err != nil {
			return err
		}
	}

	if err := os.Rename(cand, path); err != nil {
		return err
	}

	if options != nil && options.Chmod {
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
