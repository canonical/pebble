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
	"github.com/canonical/pebble/internals/overlord/identities"
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
	}{
		// API source: Unix Domain Socket
		{
			// User: nil
			apiSource:       daemon.TransportTypeUnixSocket,
			user:            nil,
			openCheckErr:    nil,
			adminCheckErr:   errUnauthorized,
			userCheckErr:    errUnauthorized,
			metricsCheckErr: errUnauthorized,
		}, {
			// User access: UntrustedAccess
			apiSource:       daemon.TransportTypeUnixSocket,
			user:            &daemon.UserState{Access: identities.UntrustedAccess},
			openCheckErr:    nil,
			adminCheckErr:   errUnauthorized,
			userCheckErr:    errUnauthorized,
			metricsCheckErr: errUnauthorized,
		}, {
			// User access: MetricsAccess
			apiSource:       daemon.TransportTypeUnixSocket,
			user:            &daemon.UserState{Access: identities.MetricsAccess},
			openCheckErr:    nil,
			adminCheckErr:   errUnauthorized,
			userCheckErr:    errUnauthorized,
			metricsCheckErr: nil,
		}, {
			// User access: ReadAccess
			apiSource:       daemon.TransportTypeUnixSocket,
			user:            &daemon.UserState{Access: identities.ReadAccess},
			openCheckErr:    nil,
			adminCheckErr:   errUnauthorized,
			userCheckErr:    nil,
			metricsCheckErr: nil,
		}, {
			// User access: AdminAccess
			apiSource:       daemon.TransportTypeUnixSocket,
			user:            &daemon.UserState{Access: identities.AdminAccess},
			openCheckErr:    nil,
			adminCheckErr:   nil,
			userCheckErr:    nil,
			metricsCheckErr: nil,
		},
		// API source: HTTP
		{
			// User: nil
			apiSource:       daemon.TransportTypeHTTP,
			user:            nil,
			openCheckErr:    nil,
			adminCheckErr:   errUnauthorized,
			userCheckErr:    errUnauthorized,
			metricsCheckErr: errUnauthorized,
		}, {
			// User access: UntrustedAccess
			apiSource:       daemon.TransportTypeHTTP,
			user:            &daemon.UserState{Access: identities.UntrustedAccess},
			openCheckErr:    nil,
			adminCheckErr:   errUnauthorized,
			userCheckErr:    errUnauthorized,
			metricsCheckErr: errUnauthorized,
		}, {
			// User access: MetricsAccess
			apiSource:       daemon.TransportTypeHTTP,
			user:            &daemon.UserState{Access: identities.MetricsAccess},
			openCheckErr:    nil,
			adminCheckErr:   errUnauthorized,
			userCheckErr:    errUnauthorized,
			metricsCheckErr: nil,
		}, {
			// User access: ReadAccess
			apiSource:       daemon.TransportTypeHTTP,
			user:            &daemon.UserState{Access: identities.ReadAccess},
			openCheckErr:    nil,
			adminCheckErr:   errUnauthorized,
			userCheckErr:    errUnauthorized,
			metricsCheckErr: errUnauthorized,
		}, {
			// User access: AdminAccess
			apiSource:       daemon.TransportTypeHTTP,
			user:            &daemon.UserState{Access: identities.AdminAccess},
			openCheckErr:    nil,
			adminCheckErr:   errUnauthorized,
			userCheckErr:    errUnauthorized,
			metricsCheckErr: errUnauthorized,
		},
	}
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
	}
}
