// Copyright (c) 2026 Canonical Ltd
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

//go:build linux

package httputil_test

import (
	"crypto/x509"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/httputil"
)

func Test(t *testing.T) { TestingT(t) }

type transportSuite struct{}

var _ = Suite(&transportSuite{})

// TestLoadSystemRootsConformance validates that our generated
// loadSystemRoots produces a cert pool equal to x509.SystemCertPool.
// This confirms the generated code correctly replicates stdlib behaviour.
func (s *transportSuite) TestLoadSystemRootsConformance(c *C) {
	systemPool, err := x509.SystemCertPool()
	c.Assert(err, IsNil)

	ourPool, err := httputil.LoadSystemRoots()
	c.Assert(err, IsNil)

	c.Check(ourPool.Equal(systemPool), Equals, true)
}

// TestTransportLazyLoad verifies no cert loading occurs until RoundTrip
// is called.
func (s *transportSuite) TestTransportLazyLoad(c *C) {
	t := httputil.NewTransport()
	// Transport is initialised but transport field must be nil.
	c.Check(t.Initialised(), Equals, false)
}

// TestTransportRefreshBeforeUseIsNoop verifies Refresh is a no-op
// before the transport has been used.
func (s *transportSuite) TestTransportRefreshBeforeUseIsNoop(c *C) {
	t := httputil.NewTransport()
	err := t.Refresh()
	c.Assert(err, IsNil)
	c.Check(t.Initialised(), Equals, false)
}
