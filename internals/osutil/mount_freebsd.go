// Copyright (c) 2026 Canonical Ltd
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

import "golang.org/x/sys/unix"

// IsMounted checks if a given directory is a mount point.
func IsMounted(baseDir string) (bool, error) {
	n, err := unix.Getfsstat(nil, unix.MNT_NOWAIT)
	if err != nil {
		return false, err
	}
	entries := make([]unix.Statfs_t, n)
	n, err = unix.Getfsstat(entries, unix.MNT_NOWAIT)
	if err != nil {
		return false, err
	}
	for _, entry := range entries[:n] {
		if baseDir == unix.ByteSliceToString(entry.Mntonname[:]) {
			return true, nil
		}
	}
	return false, nil
}
