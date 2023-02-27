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
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	extMagic uint16 = 0xEF53 // Ext magic signature
)

type extPartition struct {
	f          *os.File
	superblock struct {
		_       [56]byte
		Magic   uint16 // Ext magic signature
		_       [62]byte
		VolName [16]byte // Volume name
	}
}

func (p extPartition) Path() string {
	return p.f.Name()
}

func (p extPartition) FSType() string {
	// TODO: Could this lead to issues when attempting to mount ext2/3 partitions?
	return "ext4"
}

func (p extPartition) Label() string {
	return strings.TrimRight(string(p.superblock.VolName[:]), "\x00")
}

func newExtPartition(f *os.File) (Partition, error) {
	p := extPartition{f: f}

	// Skip the 2-sector padding of ext2/3/4
	if _, err := f.Seek(1024, io.SeekStart); err != nil {
		return nil, fmt.Errorf("cannot seek: %w", err)
	}

	if err := binary.Read(f, binary.LittleEndian, &p.superblock); err != nil {
		return nil, fmt.Errorf("cannot read superblock: %w", err)
	}
	if p.superblock.Magic != extMagic {
		return nil, errors.New("invalid Ext magic")
	}
	return p, nil
}
