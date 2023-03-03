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

	"golang.org/x/sys/unix"
)

// procSelfMountInfo is a path to the mountinfo table of the current process.
var procSelfMountInfo = "/proc/self/mountinfo"

var (
	syscallSync    = unix.Sync
	syscallMount   = unix.Mount
	syscallUnmount = unix.Unmount
)

// IsMounted checks if a given directory is a mount point.
func IsMounted(baseDir string) (bool, error) {
	entries, err := LoadMountInfo(procSelfMountInfo)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if baseDir == entry.MountDir {
			return true, nil
		}
	}
	return false, nil
}

// Mount attaches a filesystem accessible via the device node specified by source
// to the specified baseDir. If not existing, baseDir will be created before
// mounting the filesystem.
func Mount(source, baseDir, fstype string, readOnly bool) error {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return fmt.Errorf("cannot create directory %q: %w", baseDir, err)
	}

	flags := uintptr(0)
	if readOnly {
		flags |= unix.MS_RDONLY
	}
	if err := syscallMount(source, baseDir, fstype, flags, ""); err != nil {
		return fmt.Errorf("cannot mount %q: %w", source, err)
	}
	return nil
}

// Unmount removes the attachment of the topmost filesystem mounted on baseDir.
func Unmount(baseDir string) error {
	syscallSync()
	if err := syscallUnmount(baseDir, 0); err != nil {
		return fmt.Errorf("cannot unmount %q: %w", baseDir, err)
	}
	return nil
}
