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

const vfatMagic = "FAT32"
const emptyLabel = "NO NAME"
const mbrSig = uint16(0xAA55)

type vfatPartition struct {
	f *os.File

	// See <https://github.com/util-linux/util-linux/blob/master/libblkid/src/superblocks/vfat.c#L24>
	superblock struct {
		_      [0x47]byte               // [0x000:0x046] Padding
		Label  [11]byte                 // [0x047:0x051] Volume label
		Magic  [8]byte                  // [0x052:0x059] VFAT magic
		_      [0x1FE - (0x52 + 8)]byte // [0x05A:0x1FD] Padding
		MBRSig uint16                   // [0x1FE:0x1FF] MBR signature
	}
}

func (p vfatPartition) DevicePath() string {
	return p.f.Name()
}

func (p vfatPartition) MountType() MountType {
	return MountTypeFAT32
}

func (p vfatPartition) MountLabel() string {
	if s := strings.TrimSpace(string(p.superblock.Label[:])); s != emptyLabel {
		return s
	}
	return ""
}

func newVFATPartition(f *os.File) (Partition, error) {
	p := vfatPartition{f: f}
	if err := binary.Read(f, binary.LittleEndian, &p.superblock); err != nil {
		return nil, fmt.Errorf("cannot read superblock: %w", err)
	}
	if p.superblock.MBRSig != mbrSig {
		return nil, errors.New("invalid MBR signature")
	}
	if magic := strings.TrimSpace(string(p.superblock.Magic[:])); magic != vfatMagic {
		return nil, errors.New("invalid vfat magic")
	}
	return p, nil
}
