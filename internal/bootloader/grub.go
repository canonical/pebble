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
	"os"
	"path/filepath"

	"github.com/canonical/pebble/internal/bootloader/grubenv"
	"github.com/canonical/pebble/internal/osutil"
)

// GRUB implements the Bootloader interface to support bootloader operations
// on GRUB 2.
type GRUB struct {
	// rootdir is the directory where the GRUB prefix is mounted.
	rootdir string
}

// newGrub initializes a new instance of the GRUB bootloader.
func NewGRUB(rootdir string) Bootloader {
	return &GRUB{
		rootdir: rootdir,
	}
}

func (g *GRUB) envFile() string {
	return filepath.Join(g.rootdir, "grubenv")
}

func (g *GRUB) Name() string {
	return "grub"
}

func (g *GRUB) GetBootVars(names ...string) (map[string]string, error) {
	out := make(map[string]string)

	env := grubenv.NewEnv(g.envFile())
	if err := env.Load(); err != nil {
		return nil, err
	}

	for _, name := range names {
		out[name] = env.Get(name)
	}

	return out, nil
}

func (g *GRUB) SetBootVars(values map[string]string) error {
	env := grubenv.NewEnv(g.envFile())
	if err := env.Load(); err != nil && !os.IsNotExist(err) {
		return err
	}
	for k, v := range values {
		env.Set(k, v)
	}
	return env.Save()
}

func (g *GRUB) Present() (bool, error) {
	doesExist, _, err := osutil.ExistsIsDir(filepath.Join(g.rootdir, "grub.cfg"))
	return doesExist, err
}

func (g *GRUB) GetActiveSlot() (string, error) {
	vars, err := g.GetBootVars("boot.slot")
	if err != nil {
		return "", err
	}
	return vars["boot.slot"], nil
}

func (g *GRUB) SetActiveSlot(label string) error {
	return g.SetBootVars(map[string]string{
		"boot.slot": label,
	})
}

func (g *GRUB) GetStatus(label string) (Status, error) {
	varName := "boot." + label + ".status"
	vars, err := g.GetBootVars(varName)
	if err != nil {
		return "", err
	}
	s := Status(vars[varName])
	switch s {
	case Unbootable, Try, Fail:
		return s, nil
	}
	return Try, nil
}
