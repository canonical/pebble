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
	"strings"
)

const mbrSig uint16 = 0xAA55          // MBR boot signature.
const fat32Sig string = "FAT32"       // FAT32 signature.
const defaultLabel string = "NO NAME" // Default empty volume label.

type fat32Superblock struct {
	_      [71]byte  // Reserved.
	Label  [11]byte  // Volume label.
	FSType [8]byte   // FS type.
	_      [420]byte // Reserved.
	MBRSig uint16    // MBR boot signature.
}

func (sb *fat32Superblock) isValid() bool {
	fsType := strings.TrimSpace(string(sb.FSType[:]))
	return sb.MBRSig == mbrSig && fsType == fat32Sig
}

func newFat32Superblock(r io.Reader) (*fat32Superblock, error) {
	sb := &fat32Superblock{}
	if err := binary.Read(r, binary.LittleEndian, sb); err != nil {
		return nil, fmt.Errorf("cannot read FAT32 superblock: %w", err)
	}
	if !sb.isValid() {
		return nil, errors.New("cannot read FAT32 superblock: not a FAT32 partition")
	}
	if strings.TrimSpace(string(sb.Label[:])) == defaultLabel {
		for i := range sb.Label {
			sb.Label[i] = ' '
		}
	}
	return sb, nil
}
