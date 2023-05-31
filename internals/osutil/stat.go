// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (c) 2014-2015 Canonical Ltd
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

package osutil

import (
	"os"
	"os/exec"
	"syscall"
)

// CanStat returns true if stat succeeds on the given path.
// It may return false on permission issues.
func CanStat(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsDir returns true if the given path is a directory.
// It may return false on permission issues.
func IsDir(path string) bool {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return false
	}
	return fileInfo.IsDir()
}

// IsDevice returns true if mode coresponds to a device (char/block).
func IsDevice(mode os.FileMode) bool {
	return (mode & (os.ModeDevice | os.ModeCharDevice)) != 0
}

// IsSymlink returns true if path is a symlink.
func IsSymlink(path string) bool {
	fileInfo, err := os.Lstat(path)
	if err != nil {
		return false
	}

	return (fileInfo.Mode() & os.ModeSymlink) != 0
}

// IsExec returns true if path points to an executable file.
func IsExec(path string) bool {
	stat, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !stat.IsDir() && (stat.Mode().Perm()&0111 != 0)
}

// IsExecInPath returns true if name is an executable in $PATH.
func IsExecInPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

var lookPath func(name string) (string, error) = exec.LookPath

// LookPathDefault searches for a given command name in all directories
// listed in the environment variable PATH and returns the found path or the
// provided default path.
func LookPathDefault(name string, defaultPath string) string {
	p, err := lookPath(name)
	if err != nil {
		return defaultPath
	}
	return p
}

// IsWritable checks if the given file/directory can be written by
// the current user
func IsWritable(path string) bool {
	// from "fcntl.h"
	const W_OK = 2

	err := syscall.Access(path, W_OK)
	return err == nil
}

// IsDirNotExist tells you whether the given error is due to a directory not existing.
func IsDirNotExist(err error) bool {
	switch pe := err.(type) {
	case nil:
		return false
	case *os.PathError:
		err = pe.Err
	case *os.LinkError:
		err = pe.Err
	case *os.SyscallError:
		err = pe.Err
	}

	return err == syscall.ENOTDIR || err == syscall.ENOENT || err == os.ErrNotExist
}

// ExistIsDir checks whether a given path exists, and if so whether it is a directory.
func ExistsIsDir(fn string) (exists bool, isDir bool, err error) {
	st, err := os.Stat(fn)
	if err != nil {
		if IsDirNotExist(err) {
			return false, false, nil
		}
		return false, false, err
	}
	return true, st.IsDir(), nil
}
