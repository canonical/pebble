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

func (b *mockBootloader) GetBootVars(names ...string) (map[string]string, error) {
	if b.getBootVarsError != nil {
		return nil, b.getBootVarsError
	}
	var vars map[string]string
	for _, name := range names {
		vars[name] = b.bootVars[name]
	}
	return vars, nil
}

func (b *mockBootloader) SetBootVars(values map[string]string) error {
	if b.setBootVarsError != nil {
		return b.setBootVarsError
	}
	for name, value := range values {
		b.bootVars[name] = value
	}
	return nil
}

func (b *mockBootloader) Present() (bool, error) {
	if b.presentError != nil {
		return false, b.presentError
	}
	return b.isPresent, nil
}

func (b *mockBootloader) GetActiveSlot() (string, error) {
	if b.getActiveSlotError != nil {
		return "", b.getActiveSlotError
	}
	return b.activeSlot, nil
}

func (b *mockBootloader) SetActiveSlot(label string) error {
	if b.setActiveSlotError != nil {
		return b.setActiveSlotError
	}
	b.activeSlot = label
	return nil
}

func (b *mockBootloader) GetStatus(label string) (bootloader.Status, error) {
	if b.getStatusError != nil {
		return "", b.getStatusError
	}
	return b.statuses[label], nil
}

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&bootloaderSuite{})

type bootloaderSuite struct {
	b                 *mockBootloader
	newMockBootloader bootloader.BootloaderNewFunc

	oldBootloaders []bootloader.BootloaderNewFunc
}

func (s *bootloaderSuite) SetUpTest(c *C) {
	s.b = &mockBootloader{name: "mock"}
	s.newMockBootloader = func(string) bootloader.Bootloader {
		return s.b
	}

	s.oldBootloaders = bootloader.Bootloaders
	bootloader.Bootloaders = append(bootloader.Bootloaders, s.newMockBootloader)
}

func (s *bootloaderSuite) TearDownTest(c *C) {
	bootloader.Bootloaders = s.oldBootloaders
}

func (s *bootloaderSuite) TestFind(c *C) {
	s.b.isPresent = true
	b, err := bootloader.Find()
	c.Assert(err, IsNil)
	c.Assert(*b, DeepEquals, s.b)
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
