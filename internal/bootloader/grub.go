//go:build termus
// +build termus

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

package bootloader

import (
	"path/filepath"

	"github.com/canonical/pebble/internal/bootloader/grubenv"
	"github.com/canonical/pebble/internal/osutil"
)

// grub implements the Bootloader interface to support bootloader operations
// on GRUB 2.
type grub struct {
	rootdir string
	env     *grubenv.Env
}

// newGrub initializes a new instance of the GRUB bootloader.
func newGrub(rootdir string) Bootloader {
	return &grub{
		rootdir: rootdir,
		env:     grubenv.NewEnv(filepath.Join(rootdir, "grub.env")),
	}
}

func (g *grub) Name() string {
	return "grub"
}

func (g *grub) Find() error {
	doesExist, _, err := osutil.ExistsIsDir(filepath.Join(g.rootdir, "grub.cfg"))
	if !doesExist || err != nil {
		return ErrNoBootloader
	}
	return nil
}

func (g *grub) ActiveSlot() string {
	if err := g.env.Load(); err != nil {
		return ""
	}
	return g.env.Get("boot_slot")
}

func (g *grub) SetActiveSlot(label string) error {
	g.env.Set("boot_slot", label)
	return g.env.Save()
}
