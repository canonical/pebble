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
		apiSource       daemon.ApiRequestSrc
		user            *daemon.UserState
		openCheckErr    daemon.Response
		adminCheckErr   daemon.Response
		userCheckErr    daemon.Response
		metricsCheckErr daemon.Response
	}{
		// API source: Unix Domain Socket
		{
			// User: nil
			apiSource:       daemon.ApiRequestSrcUnixSocket,
			user:            nil,
			openCheckErr:    nil,
			adminCheckErr:   errUnauthorized,
			userCheckErr:    errUnauthorized,
			metricsCheckErr: errUnauthorized,
		}, {
			// User access: UntrustedAccess
			apiSource:       daemon.ApiRequestSrcUnixSocket,
			user:            &daemon.UserState{Access: state.UntrustedAccess},
			openCheckErr:    nil,
			adminCheckErr:   errUnauthorized,
			userCheckErr:    errUnauthorized,
			metricsCheckErr: errUnauthorized,
		}, {
			// User access: MetricsAccess
			apiSource:       daemon.ApiRequestSrcUnixSocket,
			user:            &daemon.UserState{Access: state.MetricsAccess},
			openCheckErr:    nil,
			adminCheckErr:   errUnauthorized,
			userCheckErr:    errUnauthorized,
			metricsCheckErr: nil,
		}, {
			// User access: ReadAccess
			apiSource:       daemon.ApiRequestSrcUnixSocket,
			user:            &daemon.UserState{Access: state.ReadAccess},
			openCheckErr:    nil,
			adminCheckErr:   errUnauthorized,
			userCheckErr:    nil,
			metricsCheckErr: nil,
		}, {
			// User access: AdminAccess
			apiSource:       daemon.ApiRequestSrcUnixSocket,
			user:            &daemon.UserState{Access: state.AdminAccess},
			openCheckErr:    nil,
			adminCheckErr:   nil,
			userCheckErr:    nil,
			metricsCheckErr: nil,
		},
		// API source: HTTP
		{
			// User: nil
			apiSource:       daemon.ApiRequestSrcHTTP,
			user:            nil,
			openCheckErr:    nil,
			adminCheckErr:   errUnauthorized,
			userCheckErr:    errUnauthorized,
			metricsCheckErr: errUnauthorized,
		}, {
			// User access: UntrustedAccess
			apiSource:       daemon.ApiRequestSrcHTTP,
			user:            &daemon.UserState{Access: state.UntrustedAccess},
			openCheckErr:    nil,
			adminCheckErr:   errUnauthorized,
			userCheckErr:    errUnauthorized,
			metricsCheckErr: errUnauthorized,
		}, {
			// User access: MetricsAccess
			apiSource:       daemon.ApiRequestSrcHTTP,
			user:            &daemon.UserState{Access: state.MetricsAccess},
			openCheckErr:    nil,
			adminCheckErr:   errUnauthorized,
			userCheckErr:    errUnauthorized,
			metricsCheckErr: nil,
		}, {
			// User access: ReadAccess
			apiSource:       daemon.ApiRequestSrcHTTP,
			user:            &daemon.UserState{Access: state.ReadAccess},
			openCheckErr:    nil,
			adminCheckErr:   errUnauthorized,
			userCheckErr:    errUnauthorized,
			metricsCheckErr: errUnauthorized,
		}, {
			// User access: AdminAccess
			apiSource:       daemon.ApiRequestSrcHTTP,
			user:            &daemon.UserState{Access: state.AdminAccess},
			openCheckErr:    nil,
			adminCheckErr:   errUnauthorized,
			userCheckErr:    errUnauthorized,
			metricsCheckErr: errUnauthorized,
		},
		// API source: HTTPS
		{
			// User: nil
			apiSource:       daemon.ApiRequestSrcHTTPS,
			user:            nil,
			openCheckErr:    nil,
			adminCheckErr:   errUnauthorized,
			userCheckErr:    errUnauthorized,
			metricsCheckErr: errUnauthorized,
		}, {
			// User access: UntrustedAccess
			apiSource:       daemon.ApiRequestSrcHTTPS,
			user:            &daemon.UserState{Access: state.UntrustedAccess},
			openCheckErr:    nil,
			adminCheckErr:   errUnauthorized,
			userCheckErr:    errUnauthorized,
			metricsCheckErr: errUnauthorized,
		}, {
			// User access: MetricsAccess
			apiSource:       daemon.ApiRequestSrcHTTPS,
			user:            &daemon.UserState{Access: state.MetricsAccess},
			openCheckErr:    nil,
			adminCheckErr:   errUnauthorized,
			userCheckErr:    errUnauthorized,
			metricsCheckErr: nil,
		}, {
			// User access: ReadAccess
			apiSource:       daemon.ApiRequestSrcHTTPS,
			user:            &daemon.UserState{Access: state.ReadAccess},
			openCheckErr:    nil,
			adminCheckErr:   errUnauthorized,
			userCheckErr:    nil,
			metricsCheckErr: nil,
		}, {
			// User access: AdminAccess
			apiSource:       daemon.ApiRequestSrcHTTPS,
			user:            &daemon.UserState{Access: state.AdminAccess},
			openCheckErr:    nil,
			adminCheckErr:   nil,
			userCheckErr:    nil,
			metricsCheckErr: nil,
		}}
	for _, t := range tests {
		// Fake a test request.
		r := &http.Request{
			URL: &url.URL{},
		}
		r = r.WithContext(context.WithValue(context.Background(), daemon.ApiRequestSrcCtxKey, t.apiSource))
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
