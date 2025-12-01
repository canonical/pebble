// Copyright (C) 2024 Canonical Ltd
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

package daemon_test

import (
	"context"
	"net/http"
	"net/url"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/daemon"
	"github.com/canonical/pebble/internals/overlord/state"
)

type accessSuite struct{}

var _ = Suite(&accessSuite{})

var errUnauthorized = daemon.Unauthorized("access denied")

func (s *accessSuite) TestAccess(c *C) {
	tests := []struct {
		apiSource       daemon.TransportType
		user            *daemon.UserState
		openCheckErr    daemon.Response
		adminCheckErr   daemon.Response
		userCheckErr    daemon.Response
		metricsCheckErr daemon.Response
		pairingCheckErr daemon.Response
	}{{ // API source: Unix Domain Socket
		// User: nil
		apiSource:       daemon.TransportTypeUnixSocket,
		user:            nil,
		openCheckErr:    nil,
		adminCheckErr:   errUnauthorized,
		userCheckErr:    errUnauthorized,
		metricsCheckErr: errUnauthorized,
		pairingCheckErr: errUnauthorized,
	}, {
		// User access: UntrustedAccess
		apiSource:       daemon.TransportTypeUnixSocket,
		user:            &daemon.UserState{Access: state.UntrustedAccess},
		openCheckErr:    nil,
		adminCheckErr:   errUnauthorized,
		userCheckErr:    errUnauthorized,
		metricsCheckErr: errUnauthorized,
		pairingCheckErr: errUnauthorized,
	}, {
		// User access: MetricsAccess
		apiSource:       daemon.TransportTypeUnixSocket,
		user:            &daemon.UserState{Access: state.MetricsAccess},
		openCheckErr:    nil,
		adminCheckErr:   errUnauthorized,
		userCheckErr:    errUnauthorized,
		metricsCheckErr: nil,
		pairingCheckErr: errUnauthorized,
	}, {
		// User access: ReadAccess
		apiSource:       daemon.TransportTypeUnixSocket,
		user:            &daemon.UserState{Access: state.ReadAccess},
		openCheckErr:    nil,
		adminCheckErr:   errUnauthorized,
		userCheckErr:    nil,
		metricsCheckErr: nil,
		pairingCheckErr: errUnauthorized,
	}, {
		// User access: AdminAccess
		apiSource:       daemon.TransportTypeUnixSocket,
		user:            &daemon.UserState{Access: state.AdminAccess},
		openCheckErr:    nil,
		adminCheckErr:   nil,
		userCheckErr:    nil,
		metricsCheckErr: nil,
		pairingCheckErr: errUnauthorized,
	}, { // API source: HTTP
		// User: nil
		apiSource:       daemon.TransportTypeHTTP,
		user:            nil,
		openCheckErr:    nil,
		adminCheckErr:   errUnauthorized,
		userCheckErr:    errUnauthorized,
		metricsCheckErr: errUnauthorized,
		pairingCheckErr: errUnauthorized,
	}, {
		// User access: UntrustedAccess
		apiSource:       daemon.TransportTypeHTTP,
		user:            &daemon.UserState{Access: state.UntrustedAccess},
		openCheckErr:    nil,
		adminCheckErr:   errUnauthorized,
		userCheckErr:    errUnauthorized,
		metricsCheckErr: errUnauthorized,
		pairingCheckErr: errUnauthorized,
	}, {
		// User access: MetricsAccess
		apiSource:       daemon.TransportTypeHTTP,
		user:            &daemon.UserState{Access: state.MetricsAccess},
		openCheckErr:    nil,
		adminCheckErr:   errUnauthorized,
		userCheckErr:    errUnauthorized,
		metricsCheckErr: nil,
		pairingCheckErr: errUnauthorized,
	}, {
		// User access: ReadAccess
		apiSource:       daemon.TransportTypeHTTP,
		user:            &daemon.UserState{Access: state.ReadAccess},
		openCheckErr:    nil,
		adminCheckErr:   errUnauthorized,
		userCheckErr:    errUnauthorized,
		metricsCheckErr: errUnauthorized,
		pairingCheckErr: errUnauthorized,
	}, {
		// User access: AdminAccess
		apiSource:       daemon.TransportTypeHTTP,
		user:            &daemon.UserState{Access: state.AdminAccess},
		openCheckErr:    nil,
		adminCheckErr:   errUnauthorized,
		userCheckErr:    errUnauthorized,
		metricsCheckErr: errUnauthorized,
		pairingCheckErr: errUnauthorized,
	}, { // API source: HTTPS
		// User: nil
		apiSource:       daemon.TransportTypeHTTPS,
		user:            nil,
		openCheckErr:    nil,
		adminCheckErr:   errUnauthorized,
		userCheckErr:    errUnauthorized,
		metricsCheckErr: errUnauthorized,
		pairingCheckErr: errUnauthorized,
	}, {
		// User access: UntrustedAccess
		apiSource:       daemon.TransportTypeHTTPS,
		user:            &daemon.UserState{Access: state.UntrustedAccess},
		openCheckErr:    nil,
		adminCheckErr:   errUnauthorized,
		userCheckErr:    errUnauthorized,
		metricsCheckErr: errUnauthorized,
		pairingCheckErr: errUnauthorized,
	}, {
		// User access: MetricsAccess
		apiSource:       daemon.TransportTypeHTTPS,
		user:            &daemon.UserState{Access: state.MetricsAccess},
		openCheckErr:    nil,
		adminCheckErr:   errUnauthorized,
		userCheckErr:    errUnauthorized,
		metricsCheckErr: nil,
		pairingCheckErr: errUnauthorized,
	}, {
		// User access: ReadAccess
		apiSource:       daemon.TransportTypeHTTPS,
		user:            &daemon.UserState{Access: state.ReadAccess},
		openCheckErr:    nil,
		adminCheckErr:   errUnauthorized,
		userCheckErr:    nil,
		metricsCheckErr: nil,
		pairingCheckErr: errUnauthorized,
	}, {
		// User access: AdminAccess
		apiSource:       daemon.TransportTypeHTTPS,
		user:            &daemon.UserState{Access: state.AdminAccess},
		openCheckErr:    nil,
		adminCheckErr:   nil,
		userCheckErr:    nil,
		metricsCheckErr: nil,
		pairingCheckErr: errUnauthorized,
	}}
	for _, t := range tests {
		// Fake a test request.
		r := &http.Request{
			URL: &url.URL{},
		}
		r = r.WithContext(context.WithValue(context.Background(), daemon.TransportTypeKey{}, t.apiSource))
		// Check OpenAccess
		openAccess := daemon.OpenAccess{}
		err := openAccess.CheckAccess(nil, r, t.user)
		c.Assert(err, DeepEquals, t.openCheckErr)
		// Check AdminAccess
		adminAccess := daemon.AdminAccess{}
		err = adminAccess.CheckAccess(nil, r, t.user)
		c.Assert(err, DeepEquals, t.adminCheckErr)
		// Check UserAccess
		userAccess := daemon.UserAccess{}
		err = userAccess.CheckAccess(nil, r, t.user)
		c.Assert(err, DeepEquals, t.userCheckErr)
		// Check MetricsAccess
		metricsAccess := daemon.MetricsAccess{}
		err = metricsAccess.CheckAccess(nil, r, t.user)
		c.Assert(err, DeepEquals, t.metricsCheckErr)
		// Check PairingAccess
		pairingAccess := daemon.PairingAccess{}
		err = pairingAccess.CheckAccess(nil, r, t.user)
		c.Assert(err, DeepEquals, t.pairingCheckErr)
	}
}

// TestPairingAccessWithPairingWindow tests the pairing specific behaviour
// related to whether the pairing window is open or closed.
func (s *accessSuite) TestPairingAccessWithPairingWindow(c *C) {
	pairingAccess := daemon.PairingAccess{}

	r := &http.Request{
		URL: &url.URL{Path: "/v1/pairing"},
	}
	r = r.WithContext(context.WithValue(context.Background(), daemon.TransportTypeKey{}, daemon.TransportTypeHTTPS))

	// Test with pairing window disabled
	restore := daemon.FakePairingWindowEnabled(func(d *daemon.Daemon) bool {
		return false
	})
	defer restore()

	err := pairingAccess.CheckAccess(nil, r, nil)
	c.Assert(err, DeepEquals, errUnauthorized)

	// Test with pairing window open
	restore = daemon.FakePairingWindowEnabled(func(d *daemon.Daemon) bool {
		return true
	})
	defer restore()

	err = pairingAccess.CheckAccess(nil, r, nil)
	c.Assert(err, IsNil)
}
