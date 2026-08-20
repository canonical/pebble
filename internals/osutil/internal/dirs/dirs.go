// Copyright (C) 2014-2015 Canonical Ltd
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

package dirs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/canonical/x-go/strutil"
)

// the various file paths
var (
	GlobalRootDir string = "/"

	SysfsDir string

	SnapMountDir string
)

var (
	hiddenSnapDataHomeGlob []string
	snapDataHomeGlob       []string
)

// User defined home directory variables
// Not exported, use SnapHomeDirs() and SetSnapHomeDirs() instead
var (
	snapHomeDirsMu sync.Mutex
	snapHomeDirs   []string
)

func AllDataHomeGlobs() []string {
	snapHomeDirsMu.Lock()
	defer snapHomeDirsMu.Unlock()

	globs := make([]string, len(hiddenSnapDataHomeGlob)+len(snapDataHomeGlob))
	copy(globs, hiddenSnapDataHomeGlob)
	copy(globs[len(hiddenSnapDataHomeGlob):], snapDataHomeGlob)
	return globs
}

// SetSnapHomeDirs sets SnapHomeDirs to the user defined values and appends /home if needed.
// homedirs must be a comma separated list of paths to home directories.
// If homedirs is empty, SnapHomeDirs will be a slice of length 1 containing "/home".
// Also generates the data directory globbing expressions for each user.
// Expected to be run by configstate.Init, returns a slice of home directories.
func SetSnapHomeDirs(homedirs string) []string {
	snapHomeDirsMu.Lock()
	defer snapHomeDirsMu.Unlock()

	//clear old values
	snapHomeDirs = nil
	snapDataHomeGlob = nil
	hiddenSnapDataHomeGlob = nil

	// Do not set the root directory as home unless explicitly specified with "."
	if homedirs != "" {
		snapHomeDirs = strings.Split(homedirs, ",")
		for i := range snapHomeDirs {
			// clean the path
			snapHomeDirs[i] = filepath.Clean(snapHomeDirs[i])
			globalRootDir := GlobalRootDir
			// Avoid false positives with HasPrefix
			if globalRootDir != "/" && !strings.HasSuffix(globalRootDir, "/") {
				globalRootDir += "/"
			}
			if !strings.HasPrefix(snapHomeDirs[i], globalRootDir) {
				snapHomeDirs[i] = filepath.Join(GlobalRootDir, snapHomeDirs[i])
			}
			// Generate data directory globbing expressions for each user.
			snapDataHomeGlob = append(snapDataHomeGlob, filepath.Join(snapHomeDirs[i], "*", UserHomeSnapDir))
			hiddenSnapDataHomeGlob = append(hiddenSnapDataHomeGlob, filepath.Join(snapHomeDirs[i], "*", HiddenSnapDataHomeDir))
		}
	}

	// Make sure /home is part of the list.
	hasHome := strutil.ListContains(snapHomeDirs, filepath.Join(GlobalRootDir, "/home"))

	// if not add it and create the glob expressions.
	if !hasHome {
		snapHomeDirs = append(snapHomeDirs, filepath.Join(GlobalRootDir, "/home"))
		snapDataHomeGlob = append(snapDataHomeGlob, filepath.Join(GlobalRootDir, "/home", "*", UserHomeSnapDir))
		hiddenSnapDataHomeGlob = append(hiddenSnapDataHomeGlob, filepath.Join(GlobalRootDir, "/home", "*", HiddenSnapDataHomeDir))
	}

	return snapHomeDirs
}

const (
	DefaultSnapMountDir = "/snap"
	AltSnapMountDir     = "/var/lib/snapd/snap"

	// UserHomeSnapDir is the directory with snap data inside user's home
	UserHomeSnapDir = "snap"

	// HiddenSnapDataHomeDir is an experimental hidden directory for snap data
	HiddenSnapDataHomeDir = ".snap/data"
)

var (
	// a well known default value, with which it will be impossible to carry out
	// operations on the filesystem
	snapMountDirUnresolvedPlaceholder = "mount-dir-is-unset"
)

func snapMountDirProbe(rootdir string) (string, error) {
	defaultDir := filepath.Join(rootdir, DefaultSnapMountDir)
	altDir := filepath.Join(rootdir, AltSnapMountDir)

	// observe the system state to find out how snapd was packaged,
	// essentially use the same logic as
	// sc_probe_snap_mount_dir_from_pid_1_mount_ns() used in snap-confine,
	// except for hard errors
	fi, err := os.Lstat(defaultDir)
	switch {
	case err != nil:
		if errors.Is(err, fs.ErrNotExist) {
			// path does not exist, given that well-known distros are
			// handled explicitly we are dealing with a distribution we have
			// no knowledge of and the packaging does not include a default
			// mount path
			return altDir, nil
		} else {
			return "", fmt.Errorf("cannot stat %s: %w", defaultDir, err)
		}
	case fi.Mode().Type()&fs.ModeSymlink != 0:
		// exists and is a symlink, find out what the target is, but keep the
		// checks simple and read the symlink rather than trying
		// filepath.EvalSymlinks() which needs intermediate directories to
		// exist; the symlink can be relative so cehck both with and without the
		// leading /
		p, err := os.Readlink(defaultDir)
		switch {
		case err != nil:
			return "", err
		case p != AltSnapMountDir && p != AltSnapMountDir[1:] && p != altDir:
			return "", fmt.Errorf("%v must be a symbolic link to %v", defaultDir, AltSnapMountDir)
		default:
			// we read the symlink and it points to the alternative location
			return altDir, nil
		}
	case fi.Mode().Type().IsDir():
		// exists and is a directory
		return defaultDir, nil
	}

	return "", errors.New("internal error: unresolved snap mount dir")
}

var metaSnapPath = "/meta/snap.yaml"

// isInsideBaseSnap returns true if the process is inside a base snap environment.
//
// The things that count as a base snap are:
// - any base snap mounted at /
// - any os snap mounted at /
func isInsideBaseSnap() (bool, error) {
	_, err := os.Stat(metaSnapPath)
	if err != nil && os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

// SetRootDir allows settings a new global root directory, this is useful
// for e.g. chroot operations
func SetRootDir(rootdir string) {
	if rootdir == "" {
		rootdir = "/"
	}
	GlobalRootDir = rootdir

	isInsideBase, _ := isInsideBaseSnap()
	if isInsideBase {
		// when inside the base, the mount directory is always /snap
		SnapMountDir = filepath.Join(rootdir, DefaultSnapMountDir)
	} else {
		if dir, err := snapMountDirProbe(rootdir); err == nil {
			SnapMountDir = dir
		} else {
			SnapMountDir = snapMountDirUnresolvedPlaceholder
		}
	}

	SysfsDir = filepath.Join(rootdir, "/sys")

	// If the root directory changes we also need to reset snapHomeDirs.
	SetSnapHomeDirs("/home")
}
