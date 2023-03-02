//go:build !termus
// +build !termus

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
	"testing"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internal/bootloader"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&bootloaderSuite{})

type bootloaderSuite struct {
}

func (s *bootloaderSuite) TestFind(c *C) {
	_, err := bootloader.Find()
	c.Assert(err, ErrorMatches, "cannot find bootloader on unsupported platform")
}
