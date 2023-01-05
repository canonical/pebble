//go:build termus
// +build termus

// Copyright (c) 2022 Canonical Ltd
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

package boot

import (
	"fmt"
	"os"
	"syscall"
)

var (
	MountImpl   = syscall.Mount
	UnmountImpl = syscall.Unmount
)

type mount struct {
	source string
	target string
	fstype string
	flags  uintptr
	data   string
}

func (m *mount) mount() error {
	if err := os.MkdirAll(m.target, 0644); err != nil {
		return fmt.Errorf("cannot create directory %q: %w", m.target, err)
	}
	if err := MountImpl(m.source, m.target, m.fstype, m.flags, m.data); err != nil {
		return fmt.Errorf("cannot mount %q: %w", m.source, err)
	}
	return nil
}

func (m *mount) unmount() error {
	if err := UnmountImpl(m.target, 0); err != nil {
		return fmt.Errorf("cannot unmount %q: %w", m.target, err)
	}
	return nil
}
