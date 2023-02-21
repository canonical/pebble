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
)

const extMagic uint16 = 0xEF53 // ext2/3/4 magic signature.

type extSuperblock struct {
	_     [56]byte // Reserved.
	Magic uint16   // ext2/3/4 magic signature.
	_     [62]byte // Reserved.
	Label [16]byte // Volume name.
}

func (sb *extSuperblock) isValid() bool {
	return sb.Magic == extMagic
}

func newExtSuperblock(r io.Reader) (*extSuperblock, error) {
	sb := &extSuperblock{}
	if err := binary.Read(r, binary.LittleEndian, sb); err != nil {
		return nil, fmt.Errorf("cannot read ext2/3/4 superblock: %w", err)
	}
	if !sb.isValid() {
		return nil, errors.New("cannot read ext2/3/4 superblock: not an ext2/3/4 partition")
	}
	return sb, nil
}
