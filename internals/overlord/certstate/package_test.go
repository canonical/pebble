// Copyright (C) 2025 Canonical Ltd
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

package certstate_test

import (
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/certstate"
)

// Hook up check.v1 into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type certSuite struct{}

var _ = Suite(&certSuite{})

func (cs *certSuite) createKeypair(c *C, tlsDir string, order int, notBefore time.Time, notAfter time.Time) {
	keypair, err := certstate.GenerateX509ECP256Keypair(notBefore, notAfter)
	c.Assert(err, IsNil)
	keypair.Order = order
	err = certstate.WriteX509Keypair(keypair, tlsDir)
	c.Assert(err, IsNil)
}
