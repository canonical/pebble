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

package daemon

import (
	"net/http"
	"net/http/httptest"

	. "gopkg.in/check.v1"
)

// TestShutdownKillsDaemonTomb checks to make sure that the call to shutdown
// sets of the necessary events in the daemon to have it shutdown.
func (s *apiSuite) TestShutdownKillsDaemonTomb(c *C) {
	daemon := s.daemon(c)
	shutdownCmd := apiCmd("/v1/shutdown")
	req, err := http.NewRequest("GET", "/v1/shutdown", nil)
	c.Assert(err, IsNil)
	rsp := v1PostShutdown(shutdownCmd, req, nil).(*resp)
	rec := httptest.NewRecorder()
	rsp.ServeHTTP(rec, req)
	c.Assert(rec.Code, Equals, 200)
	c.Assert(rsp.Status, Equals, 200)
	c.Assert(rsp.Type, Equals, ResponseTypeSync)

	_, open := <-daemon.tomb.Dying()
	c.Assert(open, Equals, false)
}
