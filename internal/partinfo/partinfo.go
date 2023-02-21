// Copyright (c) 2023 Canonical Ltd
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

package partinfo

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"
)

type FSType string

const (
	FAT32 FSType = "fat32"
	Ext   FSType = "ext"
)

type PartInfo interface {
	GetDevNode() (string, error)
	GetLabel() (string, error)
	GetFSType() (FSType, error)
}

type partition struct {
	path   string
	label  string
	fsType FSType
}

func (b *partition) parseSuperblock() error {
	f, err := os.OpenFile(b.path, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("cannot parse superblock: %w", err)
	}
	defer f.Close()

	// Attempt to read first sector of the partition.
	s := make([]byte, 512)
	n, err := f.Read(s)
	if err != nil {
		return fmt.Errorf("cannot parse superblock: %w", err)
	}
	if n != len(s) {
		return fmt.Errorf("cannot parse superblock: cannot read first sector")
	}

	// Attempt to parse FAT32 superblock
	if sb, err := newFat32Superblock(bytes.NewReader(s)); err == nil {
		b.label = strings.TrimSpace(string(sb.Label[:]))
		b.fsType = FAT32
		return nil
	}

	// Skip the 2-sector padding of ext2/3/4
	if _, err := f.Seek(1024, io.SeekStart); err != nil {
		return fmt.Errorf("cannot parse superblock: %w", err)
	}

	// Read ext2/3/4 superblock
	s = make([]byte, 1024)
	if n, err = f.Read(s); err != nil {
		return fmt.Errorf("cannot parse superblock: %w", err)
	}
	if n != len(s) {
		return fmt.Errorf("cannot parse superblock: cannot read ext4 superblock")
	}

	// Attempt to parse ext2/3/4 superblock
	if sb, err := newExtSuperblock(bytes.NewReader(s)); err == nil {
		b.label = strings.TrimRight(string(sb.Label[:]), "\x00")
		b.fsType = Ext
		return nil
	}

	return errors.New("cannot parse superblock: unrecognized file system")
}

var SysfsPath = "/sys"
var DevfsPath = "/dev"

func enumerateDisks() ([]string, error) {
	base := path.Join(SysfsPath, "block")
	dirs, err := ioutil.ReadDir(base)
	if err != nil {
		return nil, err
	}
	disks := make([]string, 0)
	for _, d := range dirs {
		disks = append(disks, path.Join(base, d.Name()))
	}
	return disks, nil
}

func EnumeratePartitions() ([]partition, error) {
	disks, err := enumerateDisks()
	if err != nil {
		return nil, err
	}
	parts := make([]partition, 0)
	for _, bdev := range disks {
		name := path.Base(bdev)
		dirs, err := ioutil.ReadDir(bdev)
		if err != nil {
			return nil, err
		}
		for _, d := range dirs {
			// Directories starting with the block device name must be
			// partition entries
			if !d.IsDir() || !strings.HasPrefix(d.Name(), name) {
				continue
			}
			part := partition{path: path.Join(DevfsPath, d.Name())}
			if err := part.parseSuperblock(); err == nil {
				parts = append(parts, part)
			}
		}
	}
	return parts, nil
}
