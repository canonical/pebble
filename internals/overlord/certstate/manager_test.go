// Copyright (c) 2025 Canonical Ltd
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
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/certstate"
)

// The cert manager should successfully start up even if the supplied TLS
// keypair directory does not exist.
func (cs *certSuite) TestNoDirectory(c *C) {
	tlsDir := filepath.Join(c.MkDir(), "tls")
	_, err := certstate.NewManager(tlsDir)
	c.Assert(err, IsNil)
}

// The tls directory should have 0o700 permissions
func (cs *certSuite) TestDirectoryInvalid(c *C) {
	tlsDir := filepath.Join(c.MkDir(), "tls")
	os.MkdirAll(tlsDir, 0740)
	_, err := certstate.NewManager(tlsDir)
	c.Assert(err, ErrorMatches, ".* expected permission 0o700 .*")
}

var tests = []struct {
	summary  string
	keypairs []map[int]struct {
		order     int
		notBefore time.Time
		notAfter  time.Time
		generated *certstate.X509KeyPair
	}
	errorResult   string
	selectedOrder int
}{{
	summary:     "Driectory does not exist",
	errorResult: "fred",
}}
