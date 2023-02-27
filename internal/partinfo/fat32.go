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
	"os"
	"strings"
)

const (
	fat32Sig          = "FAT32"   // FAT32 filesystem type signature
	emptyLabel        = "NO NAME" // Default empty volume label
	mbrSig     uint16 = 0xAA55    // MBR signature
)

type fat32Partition struct {
	f          *os.File
	superblock struct {
		_      [71]byte
		Label  [11]byte // Volume label
		FSType [8]byte  // FAT filesystem type
		_      [420]byte
		MBRSig uint16 // MBR signature
	}
}

func (p fat32Partition) Path() string {
	return p.f.Name()
}

func (p fat32Partition) FSType() string {
	return "vfat"
}

func (p fat32Partition) Label() string {
	if s := strings.TrimSpace(string(p.superblock.Label[:])); s != emptyLabel {
		return s
	} else {
		return ""
	}
}

func newFAT32Partition(f *os.File) (Partition, error) {
	p := fat32Partition{f: f}
	if err := binary.Read(f, binary.LittleEndian, &p.superblock); err != nil {
		return nil, fmt.Errorf("cannot read superblock: %w", err)
	}
	if p.superblock.MBRSig != mbrSig {
		return nil, errors.New("invalid MBR signature")
	}
	if fsType := strings.TrimSpace(string(p.superblock.FSType[:])); fsType != fat32Sig {
		return nil, errors.New("unsupported FAT filesystem type")
	}
	return p, nil
}
