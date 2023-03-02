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
	"errors"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internal/bootloader"
)

type mockBootloader struct {
	name string

	bootVars         map[string]string
	getBootVarsError error
	setBootVarsError error

	isPresent    bool
	presentError error

	activeSlot         string
	getActiveSlotError error
	setActiveSlotError error

	getStatusError error
	statuses       map[string]bootloader.Status
}

func (b *mockBootloader) Name() string {
	return b.name
}

func (b *mockBootloader) Present() (bool, error) {
	if b.presentError != nil {
		return false, b.presentError
	}
	return b.isPresent, nil
}

func (b *mockBootloader) ActiveSlot() string {
	return b.activeSlot
}

func (b *mockBootloader) SetActiveSlot(label string) error {
	if b.setActiveSlotError != nil {
		return b.setActiveSlotError
	}
	b.activeSlot = label
	return nil
}

func (b *mockBootloader) Status(label string) bootloader.Status {
	return b.statuses[label]
}

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&bootloaderSuite{})

type bootloaderSuite struct {
	b                  *mockBootloader
	newMockBootloader  bootloader.BootloaderNewFunc
	restoreBootloaders func()
}

func (s *bootloaderSuite) SetUpTest(_ *C) {
	s.b = &mockBootloader{name: "mock"}
	s.newMockBootloader = func(string) bootloader.Bootloader {
		return s.b
	}

	bootloaders := []bootloader.BootloaderNewFunc{s.newMockBootloader}
	s.restoreBootloaders = bootloader.MockBootloaders(bootloaders)
}

func (s *bootloaderSuite) TearDownTest(_ *C) {
	s.restoreBootloaders()
}

func (s *bootloaderSuite) TestFind(c *C) {
	s.b.isPresent = true
	b, err := bootloader.Find()
	c.Assert(err, IsNil)
	c.Assert(b, DeepEquals, s.b)
}

func (s *bootloaderSuite) TestFindFailsNotPresent(c *C) {
	s.b.isPresent = false
	_, err := bootloader.Find()
	c.Assert(err, ErrorMatches, "cannot determine bootloader")
}

func (s *bootloaderSuite) TestFindFailsPresentError(c *C) {
	s.b.presentError = errors.New("foobar")
	_, err := bootloader.Find()
	c.Assert(err, ErrorMatches, `bootloader "mock" found but not usable: foobar`)
}
