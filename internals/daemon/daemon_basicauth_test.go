//go:build !fips

// Copyright (c) 2014-2020 Canonical Ltd
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

package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"

	"github.com/GehirnInc/crypt/sha512_crypt"
	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/state"
)

func (s *daemonSuite) TestServeHTTPUserStateBasicUnixSocket(c *C) {
	d := s.newDaemon(c)

	// Set up a Basic auth identity with a hashed password.
	// Generate sha512-crypt hash for password "test".
	crypt := sha512_crypt.New()
	hashedPassword, err := crypt.Generate([]byte("test"), nil)
	c.Assert(err, IsNil)

	d.state.Lock()
	err = d.state.AddIdentities(map[string]*state.Identity{
		"basicuser": {
			Access: state.ReadAccess,
			Basic:  &state.BasicIdentity{Password: hashedPassword},
		},
	})
	d.state.Unlock()
	c.Assert(err, IsNil)

	// Capture the UserState passed to the response function.
	var capturedUser *UserState
	cmd := &Command{
		d: d,
		GET: func(c *Command, r *http.Request, user *UserState) Response {
			capturedUser = user
			return SyncResponse(true)
		},
		ReadAccess: UserAccess{},
	}

	// Make request with Basic auth credentials over Unix Socket.
	ctx := context.WithValue(context.Background(), TransportTypeKey{}, TransportTypeUnixSocket)
	req, err := http.NewRequestWithContext(ctx, "GET", "", nil)
	c.Assert(err, IsNil)
	req.SetBasicAuth("basicuser", "test")
	req.RemoteAddr = "pid=100;uid=1000;socket=;"

	rec := httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, http.StatusOK)

	// Verify UserState for Basic identity over Unix Socket.
	c.Assert(capturedUser, NotNil)
	c.Assert(capturedUser.Username, Equals, "basicuser")
	c.Assert(capturedUser.Access, Equals, state.ReadAccess)
	c.Assert(capturedUser.UID, IsNil)
}

func (s *daemonSuite) TestServeHTTPUserStateBasicHTTP(c *C) {
	d := s.newDaemon(c)

	// Set up a Basic auth identity with a hashed password.
	// Generate sha512-crypt hash for password "test".
	crypt := sha512_crypt.New()
	hashedPassword, err := crypt.Generate([]byte("test"), nil)
	c.Assert(err, IsNil)

	d.state.Lock()
	err = d.state.AddIdentities(map[string]*state.Identity{
		"basicuser": {
			Access: state.MetricsAccess,
			Basic:  &state.BasicIdentity{Password: hashedPassword},
		},
	})
	d.state.Unlock()
	c.Assert(err, IsNil)

	// Capture the UserState passed to the response function.
	var capturedUser *UserState
	cmd := &Command{
		d: d,
		GET: func(c *Command, r *http.Request, user *UserState) Response {
			capturedUser = user
			return SyncResponse(true)
		},
		ReadAccess: MetricsAccess{},
	}

	// Make request with Basic auth credentials over HTTP (not HTTPS).
	ctx := context.WithValue(context.Background(), TransportTypeKey{}, TransportTypeHTTP)
	req, err := http.NewRequestWithContext(ctx, "GET", "", nil)
	c.Assert(err, IsNil)
	req.SetBasicAuth("basicuser", "test")
	req.RemoteAddr = "192.168.1.100:8888"

	rec := httptest.NewRecorder()
	cmd.ServeHTTP(rec, req)
	c.Check(rec.Code, Equals, http.StatusOK)

	// Verify UserState for Basic identity over HTTP.
	c.Assert(capturedUser, NotNil)
	c.Assert(capturedUser.Username, Equals, "basicuser")
	c.Assert(capturedUser.Access, Equals, state.MetricsAccess)
	c.Assert(capturedUser.UID, IsNil)
}
