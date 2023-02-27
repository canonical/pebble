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
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"
)

// Partition contains information for identifying and mounting disk partitions.
type Partition interface {
	// Path is the path to the device node file associated to the disk partinfo.
	Path() string
	// FSType is the string representing the filesystem type to be used for mounting the partinfo.
	FSType() string
	// Label is an optional user-supplied volume name.
	Label() string
}

var newPartitionFuncs = []func(*os.File) (Partition, error){
	newFAT32Partition,
	newExtPartition,
}

func newPartition(path string) (Partition, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	for _, newFn := range newPartitionFuncs {
		if p, err := newFn(f); err != nil {
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return nil, fmt.Errorf("cannot seek: %w", err)
			}
			continue
		} else {
			return p, nil
		}
	}

	return nil, errors.New("unrecognized partition")
}

var (
	sysfsPath = "/sys"
	devfsPath = "/dev"
)

// Enumerate returns a slice of Partition elements representing the disk partitions that can be mounted.
func Enumerate() ([]Partition, error) {
	// Enumerate disks
	bdevBase := path.Join(sysfsPath, "block")
	disks, err := ioutil.ReadDir(bdevBase)
	if err != nil {
		return nil, err
	}

	var partitions []Partition
	for _, diskNode := range disks {
		bdevPath := path.Join(bdevBase, diskNode.Name())
		dirs, err := ioutil.ReadDir(bdevPath)
		if err != nil {
			return nil, err
		}
		for _, partitionNode := range dirs {
			// Directories starting with the block device name must be
			// partinfo entries
			if !strings.HasPrefix(partitionNode.Name(), diskNode.Name()) {
				continue
			}
			if p, err := newPartition(path.Join(devfsPath, partitionNode.Name())); err == nil {
				partitions = append(partitions, p)
			}

		}
	}
	return partitions, nil
}
