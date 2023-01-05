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

package boot

import (
	"testing"

	. "gopkg.in/check.v1"
)

type bootstrapSuite struct{}

var _ = Suite(&bootstrapSuite{})

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

func (s *bootstrapSuite) TestCheckBootstrap(c *C) {
	err := CheckBootstrap()
	c.Assert(err, ErrorMatches, "cannot bootstrap an unsupported platform")
}

func (s *bootstrapSuite) TestBootstrap(c *C) {
	err := Bootstrap()
	c.Assert(err, ErrorMatches, "cannot bootstrap an unsupported platform")
}
