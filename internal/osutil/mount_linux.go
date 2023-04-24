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

// MountOptions holds the options that can be passed to Mount.
type MountOptions struct {
	// Source is the device node to be mounted.
	Source string
	// Target is the directory where the device will be mounted.
	Target string
	// MountType is the type of the file system that will be mounted.
	MountType string
	// ReadOnly, when true, will mount the device read-only. If the
	// device was already mounted and this option is specified in a
	// second call to Mount, the device will be remounted as read-only.
	ReadOnly bool
	// Remount, when true, will remount the device. For this to work,
	// you must supply the same target for the previous mount.
	Remount bool
}

// UnmountOptions has all the options that can be passed to Unmount()
type UnmountOptions struct {
	// Target is the directory where the device is currently mounted.
	Target string
	// Force, when true, will unmount the device even if it's busy.
	Force bool
}

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

// Mount attaches a filesystem accessible via the device node specified by the
// source to the specified target. If target doesn't exist, it will be created
// before mounting the filesystem.
func Mount(opts *MountOptions) error {
	if err := os.MkdirAll(opts.Target, 0755); err != nil {
		return fmt.Errorf("cannot create directory %q: %w", opts.Target, err)
	}
	flags := uintptr(0)
	if opts.ReadOnly {
		flags |= unix.MS_RDONLY
	}
	if opts.Remount {
		flags |= unix.MS_REMOUNT
	}
	if err := syscallMount(opts.Source, opts.Target, opts.MountType, flags, ""); err != nil {
		return err
	}
	return nil
}

// Unmount removes the attachment of the topmost filesystem mounted on the specified target.
func Unmount(opts *UnmountOptions) error {
	if opts.Force {
		// Force unmount without taking care of flushing data to the disk
		if err := syscallUnmount(opts.Target, unix.MNT_FORCE); err != nil {
			return err
		}
		return nil
	}
	entries, err := LoadMountInfo(procSelfMountInfo)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if opts.Target == entry.MountDir {
			syscallSync()
			// Attempt to remount as read-only
			err = Mount(&MountOptions{
				Source:    entry.MountSource,
				Target:    entry.MountDir,
				MountType: entry.FsType,
				ReadOnly:  true,
				Remount:   true,
			})
			if err != nil {
				return err
			}
			// Attempt to actually unmount
			if err := syscallUnmount(opts.Target, 0); err != nil {
				return err
			}
			return nil
		}
	}
	return fmt.Errorf("no device mounted at %q", opts.Target)
}
