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

package bootloader_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internal/bootloader"
	"github.com/canonical/pebble/internal/bootloader/grubenv"
)

var _ = Suite(&grubSuite{})

type grubSuite struct {
	b bootloader.Bootloader

	rootdir string
	envFile string
	cfgFile string

	env *grubenv.Env
}

func (s *grubSuite) SetUpTest(c *C) {
	s.rootdir = c.MkDir()
	s.b = bootloader.NewGRUB(s.rootdir)

	s.envFile = filepath.Join(s.rootdir, "grubenv")
	s.cfgFile = filepath.Join(s.rootdir, "grub.cfg")

	s.env = grubenv.NewEnv(s.envFile)
	s.env.Set("boot.slot", "a")
	s.env.Set("my_var", "42")
	s.env.Set("my_other_var", "foo")
	err := s.env.Save()
	c.Assert(err, IsNil)

	_, err = os.Create(s.cfgFile)
	c.Assert(err, IsNil)
}

func (s *grubSuite) TestName(c *C) {
	c.Assert(s.b.Name(), Equals, "grub")
}

func (s *grubSuite) TestPresent(c *C) {
	isPresent, err := s.b.Present()
	c.Assert(isPresent, Equals, true)
	c.Assert(err, IsNil)
}

func (s *grubSuite) TestNotPresent(c *C) {
	newCfgFile := s.cfgFile + "bak"
	err := os.Rename(s.cfgFile, newCfgFile)
	c.Assert(err, IsNil)
	defer os.Rename(newCfgFile, s.cfgFile)

	isPresent, err := s.b.Present()
	c.Assert(isPresent, Equals, false)
	c.Assert(err, IsNil)
}

func (s *grubSuite) TestPresentFails(c *C) {
	err := os.Chmod(s.rootdir, os.FileMode(0o000))
	c.Assert(err, IsNil)
	defer os.Chmod(s.rootdir, os.FileMode(0o755))

	isPresent, err := s.b.Present()
	c.Assert(isPresent, Equals, false)
	c.Assert(os.IsPermission(err), Equals, true)
}

func (s *grubSuite) TestActiveSlot(c *C) {
	slot := s.b.ActiveSlot()
	c.Assert(slot, Equals, "a")
}

func (s *grubSuite) TestActiveSlotFails(c *C) {
	newEnvFile := s.envFile + "bak"
	err := os.Rename(s.envFile, newEnvFile)
	c.Assert(err, IsNil)
	defer os.Rename(newEnvFile, s.envFile)

	slot := s.b.ActiveSlot()
	c.Assert(slot, Equals, "")
}

func (s *grubSuite) TestSetActiveSlot(c *C) {
	err := s.b.SetActiveSlot("x")
	c.Assert(err, IsNil)
	slot := s.b.ActiveSlot()
	c.Assert(slot, Equals, "x")
}

func (s *grubSuite) TestSetActiveSlotFails(c *C) {
	err := os.Chmod(s.envFile, os.FileMode(0o400))
	c.Assert(err, IsNil)
	defer os.Chmod(s.envFile, os.FileMode(0o644))

	err = s.b.SetActiveSlot("x")
	c.Assert(os.IsPermission(err), Equals, true)
}

func (s *grubSuite) TestStatusUndefined(c *C) {
	st := s.b.Status("a")
	c.Assert(st, Equals, bootloader.Try)
}

func (s *grubSuite) TestGetStatus(c *C) {
	s.env.Set("boot.b.status", "unbootable")
	err := s.env.Save()
	c.Assert(err, IsNil)
	st := s.b.Status("b")
	c.Assert(st, Equals, bootloader.Unbootable)
}
