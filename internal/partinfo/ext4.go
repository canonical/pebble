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
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"strings"
)

type checksumType byte

const (
	checksumTypeNone   checksumType = 0
	checksumTypeCRC32C checksumType = 1
)

const extMagic uint16 = 0xEF53

type ext4Partition struct {
	f *os.File

	// See <https://www.kernel.org/doc/html/latest/filesystems/ext4/globals.html>
	superblock struct {
		_            [0x38]byte                // [0x000:0x037] Padding
		Magic        uint16                    // [0x038:0x03A] Ext4 magic signature
		_            [0x78 - (0x38 + 2)]byte   // [0x03A:0x077] Padding
		VolName      [16]byte                  // [0x078:0x087] Volume name
		_            [0x175 - (0x78 + 16)]byte // [0x088:0x174] Padding
		ChecksumType checksumType              // [0x175:0x176] Superblock checksum type
		_            [0x3FC - (0x175 + 1)]byte // [0x178:0x3FB] Padding
		Checksum     uint32                    // [0x3FC:0x3FF] Superblock checksum
	}
}

func (p ext4Partition) DevicePath() string {
	return p.f.Name()
}

func (p ext4Partition) MountType() MountType {
	return MountTypeExt4
}

func (p ext4Partition) MountLabel() string {
	return strings.TrimRight(string(p.superblock.VolName[:]), "\x00")
}

func newExt4Partition(f *os.File) (Partition, error) {
	p := ext4Partition{f: f}

	// Skip the 2-sector padding of ext4
	if _, err := f.Seek(1024, io.SeekStart); err != nil {
		return nil, fmt.Errorf("cannot seek: %w", err)
	}

	// Allocate buffer for the 1K superblock
	sb := make([]byte, 1024)
	if _, err := f.Read(sb); err != nil {
		return nil, fmt.Errorf("cannot read superblock: %w", err)
	}
	buf := bytes.NewBuffer(sb)

	// Parse superblock structure
	if err := binary.Read(buf, binary.LittleEndian, &p.superblock); err != nil {
		return nil, fmt.Errorf("cannot parse superblock structure: %w", err)
	}
	if p.superblock.Magic != extMagic {
		return nil, errors.New("invalid ext4 magic")
	}

	switch p.superblock.ChecksumType {
	case checksumTypeNone:
		return p, nil
	case checksumTypeCRC32C:
		// Ext4 uses the inverse of CRC32-C for the superblock checksum
		// See <https://ext4.wiki.kernel.org/index.php/Ext4_Disk_Layout#Checksums>
		t := crc32.MakeTable(crc32.Castagnoli)
		crc32c := crc32.New(t)
		if _, err := crc32c.Write(sb[:len(sb)-4]); err != nil {
			return nil, fmt.Errorf("cannot calculate superblock checksum: %w", err)
		}
		if p.superblock.Checksum != ^crc32c.Sum32() {
			return nil, errors.New("invalid checksum")
		}
		return p, nil
	default:
		return nil, errors.New("invalid checksum type")
	}
}
