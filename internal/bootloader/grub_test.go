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
}

func (s *grubSuite) SetUpTest(c *C) {
	s.rootdir = c.MkDir()
	s.b = bootloader.NewGRUB(s.rootdir)

	s.envFile = filepath.Join(s.rootdir, "grubenv")
	s.cfgFile = filepath.Join(s.rootdir, "grub.cfg")

	env := grubenv.NewEnv(s.envFile)
	env.Set("my_var", "42")
	env.Set("my_other_var", "foo")
	err := env.Save()
	c.Assert(err, IsNil)

	_, err = os.Create(s.cfgFile)
	c.Assert(err, IsNil)
}

func (s *grubSuite) TestName(c *C) {
	c.Assert(s.b.Name(), Equals, "grub")
}

func (s *grubSuite) TestGetBootVars(c *C) {
	vars, err := s.b.GetBootVars("my_var", "my_other_var")
	c.Assert(err, IsNil)
	c.Assert(vars, DeepEquals, map[string]string{
		"my_var":       "42",
		"my_other_var": "foo",
	})
}

func (s *grubSuite) TestGetBootVarsFails(c *C) {
	newEnvFile := s.envFile + "bak"
	err := os.Rename(s.envFile, newEnvFile)
	c.Assert(err, IsNil)
	defer os.Rename(newEnvFile, s.envFile)

	vars, err := s.b.GetBootVars("my_var", "my_other_var")
	c.Assert(os.IsNotExist(err), Equals, true)
	c.Assert(vars, IsNil)
}

func (s *grubSuite) TestSetBootVars(c *C) {
	err := s.b.SetBootVars(map[string]string{
		"my_var":     "43",
		"my_new_var": "bar",
	})
	c.Assert(err, IsNil)

	vars, err := s.b.GetBootVars("my_var", "my_other_var", "my_new_var")
	c.Assert(err, IsNil)
	c.Assert(vars, DeepEquals, map[string]string{
		"my_var":       "43",
		"my_other_var": "foo",
		"my_new_var":   "bar",
	})
}

func (s *grubSuite) TestSetBootVarsNotExist(c *C) {
	os.Remove(s.envFile)
	err := s.b.SetBootVars(map[string]string{
		"my_var":     "43",
		"my_new_var": "bar",
	})
	c.Assert(err, IsNil)

	vars, err := s.b.GetBootVars("my_var", "my_other_var", "my_new_var")
	c.Assert(err, IsNil)
	c.Assert(vars, DeepEquals, map[string]string{
		"my_var":       "43",
		"my_other_var": "",
		"my_new_var":   "bar",
	})
}

func (s *grubSuite) TestSetBootVarsFails(c *C) {
	err := os.Chmod(s.envFile, os.FileMode(0o400))
	c.Assert(err, IsNil)
	defer os.Chmod(s.envFile, os.FileMode(0o644))

	err = s.b.SetBootVars(map[string]string{
		"my_var":     "43",
		"my_new_var": "bar",
	})
	c.Assert(os.IsPermission(err), Equals, true)
}

func (s *grubSuite) TestPresent(c *C) {
	isPresent, err := s.b.Present()
	c.Assert(isPresent, Equals, true)
	c.Assert(err, IsNil)
}

func (s *grubSuite) TestNotPresent(c *C) {
	newCfgFile := s.cfgFile + "bak"
	os.Rename(s.cfgFile, newCfgFile)
	defer os.Rename(newCfgFile, s.cfgFile)

	isPresent, err := s.b.Present()
	c.Assert(isPresent, Equals, false)
	c.Assert(err, IsNil)
}

func (s *grubSuite) TestPresentFails(c *C) {
	err := os.Chmod(s.rootdir, os.FileMode(0o000))
	c.Assert(err, IsNil)
	defer os.Chmod(s.rootdir, os.FileMode(0o644))

	isPresent, err := s.b.Present()
	c.Assert(isPresent, Equals, false)
	c.Assert(os.IsPermission(err), Equals, true)
}

func (s *grubSuite) TestGetActiveSlot(c *C) {
	slot, err := s.b.GetActiveSlot()
	c.Assert(slot, Equals, "")
	c.Assert(err, IsNil)

	err = s.b.SetBootVars(map[string]string{
		"boot.slot": "a",
	})
	slot, err = s.b.GetActiveSlot()
	c.Assert(slot, Equals, "a")
	c.Assert(err, IsNil)
}

func (s *grubSuite) TestGetActiveSlotFails(c *C) {
	newEnvFile := s.envFile + "bak"
	err := os.Rename(s.envFile, newEnvFile)
	c.Assert(err, IsNil)
	defer os.Rename(newEnvFile, s.envFile)

	slot, err := s.b.GetActiveSlot()
	c.Assert(slot, Equals, "")
	c.Assert(os.IsNotExist(err), Equals, true)
}

func (s *grubSuite) TestSetActiveSlot(c *C) {
	err := s.b.SetActiveSlot("x")
	c.Assert(err, IsNil)
	slot, err := s.b.GetActiveSlot()
	c.Assert(err, IsNil)
	c.Assert(slot, Equals, "x")
}

func (s *grubSuite) TestSetActiveSlotFails(c *C) {
	err := os.Chmod(s.envFile, os.FileMode(0o400))
	c.Assert(err, IsNil)
	defer os.Chmod(s.envFile, os.FileMode(0o644))

	err = s.b.SetActiveSlot("x")
	c.Assert(os.IsPermission(err), Equals, true)
}

func (s *grubSuite) TestGetStatusUndefined(c *C) {
	st, err := s.b.GetStatus("a")
	c.Assert(err, IsNil)
	c.Assert(st, Equals, bootloader.Try)
}

func (s *grubSuite) TestGetStatus(c *C) {
	err := s.b.SetBootVars(map[string]string{"boot.b.status": "unbootable"})
	st, err := s.b.GetStatus("b")
	c.Assert(err, IsNil)
	c.Assert(st, Equals, bootloader.Unbootable)
}
