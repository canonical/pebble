// Copyright (c) 2021 Canonical Ltd
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

package httpapi_test

import (
	"errors"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internal/httpapi"
	"github.com/canonical/pebble/internal/overlord/checkstate"
	"github.com/canonical/pebble/internal/plan"
)

func (s *APISuite) TestHealthNoChecks(c *C) {
	checkMgr := &checkManager{checks: func() ([]*checkstate.CheckInfo, error) {
		return nil, nil
	}}
	api := httpapi.NewAPI(checkMgr)
	status, response := serve(c, api, "GET", "/v1/health", nil)

	c.Assert(status, Equals, 200)
	c.Assert(response, DeepEquals, map[string]interface{}{
		"healthy": true,
	})
}

func (s *APISuite) TestHealthHealthy(c *C) {
	checkMgr := &checkManager{checks: func() ([]*checkstate.CheckInfo, error) {
		return []*checkstate.CheckInfo{
			{Name: "chk1", Healthy: true},
			{Name: "chk2", Healthy: true},
		}, nil
	}}
	api := httpapi.NewAPI(checkMgr)
	status, response := serve(c, api, "GET", "/v1/health", nil)

	c.Assert(status, Equals, 200)
	c.Assert(response, DeepEquals, map[string]interface{}{
		"healthy": true,
	})
}

func (s *APISuite) TestHealthUnhealthy(c *C) {
	checkMgr := &checkManager{checks: func() ([]*checkstate.CheckInfo, error) {
		return []*checkstate.CheckInfo{
			{Name: "chk1", Healthy: true},
			{Name: "chk2", Healthy: false},
			{Name: "chk3", Healthy: true},
		}, nil
	}}
	api := httpapi.NewAPI(checkMgr)
	status, response := serve(c, api, "GET", "/v1/health", nil)

	c.Assert(status, Equals, 502)
	c.Assert(response, DeepEquals, map[string]interface{}{
		"healthy": false,
	})
}

func (s *APISuite) TestHealthLevel(c *C) {
	const (
		unhealthy = iota
		healthy
		none
	)
	type levelTest struct {
		aliveCheck   int  // alive check: unhealthy, healthy, or no alive check
		readyCheck   int  // ready check: unhealthy, healthy, or no ready check
		aliveHealthy bool // expected response with ?level=alive filter
		readyHealthy bool // expected response with ?level=ready filter
	}

	// All combinations of alive and ready checks (ready implies alive).
	tests := []levelTest{
		{aliveCheck: healthy, readyCheck: healthy, aliveHealthy: true, readyHealthy: true},       // alive and ready
		{aliveCheck: healthy, readyCheck: unhealthy, aliveHealthy: true, readyHealthy: false},    // alive but not ready
		{aliveCheck: unhealthy, readyCheck: healthy, aliveHealthy: false, readyHealthy: false},   // not alive but ready => ready unhealthy
		{aliveCheck: unhealthy, readyCheck: unhealthy, aliveHealthy: false, readyHealthy: false}, // not alive nor ready
		{aliveCheck: healthy, readyCheck: none, aliveHealthy: true, readyHealthy: true},          // alive and no ready check
		{aliveCheck: unhealthy, readyCheck: none, aliveHealthy: false, readyHealthy: false},      // not alive, no ready check => ready unhealthy
		{aliveCheck: none, readyCheck: healthy, aliveHealthy: true, readyHealthy: true},          // no alive check, but ready
		{aliveCheck: none, readyCheck: unhealthy, aliveHealthy: true, readyHealthy: false},       // no alive check, not ready
		{aliveCheck: none, readyCheck: none, aliveHealthy: true, readyHealthy: true},             // no alive or ready check
	}

	for _, test := range tests {
		c.Logf("TestHealthLevels check alive=%d ready=%d, healthy alive=%t ready=%t",
			test.aliveCheck, test.readyCheck, test.aliveHealthy, test.readyHealthy)

		checkMgr := &checkManager{checks: func() ([]*checkstate.CheckInfo, error) {
			var checks []*checkstate.CheckInfo
			if test.aliveCheck != none {
				checks = append(checks, &checkstate.CheckInfo{Name: "a", Level: plan.AliveLevel, Healthy: test.aliveCheck == healthy})
			}
			if test.readyCheck != none {
				checks = append(checks, &checkstate.CheckInfo{Name: "r", Level: plan.ReadyLevel, Healthy: test.readyCheck == healthy})
			}
			return checks, nil
		}}
		api := httpapi.NewAPI(checkMgr)

		status, response := serve(c, api, "GET", "/v1/health?level=alive", nil)
		if test.aliveHealthy {
			c.Check(status, Equals, 200)
			c.Check(response, DeepEquals, map[string]interface{}{"healthy": true})
		} else {
			c.Check(status, Equals, 502)
			c.Check(response, DeepEquals, map[string]interface{}{"healthy": false})
		}

		status, response = serve(c, api, "GET", "/v1/health?level=ready", nil)
		if test.readyHealthy {
			c.Check(status, Equals, 200)
			c.Check(response, DeepEquals, map[string]interface{}{"healthy": true})
		} else {
			c.Check(status, Equals, 502)
			c.Check(response, DeepEquals, map[string]interface{}{"healthy": false})
		}
	}
}

func (s *APISuite) TestHealthNames(c *C) {
	checkMgr := &checkManager{checks: func() ([]*checkstate.CheckInfo, error) {
		return []*checkstate.CheckInfo{
			{Name: "chk1", Healthy: false},
			{Name: "chk2", Healthy: true},
			{Name: "chk3", Healthy: true},
		}, nil
	}}
	api := httpapi.NewAPI(checkMgr)

	status, response := serve(c, api, "GET", "/v1/health?names=chk1&names=chk3", nil)
	c.Assert(status, Equals, 502)
	c.Assert(response, DeepEquals, map[string]interface{}{
		"healthy": false,
	})

	status, response = serve(c, api, "GET", "/v1/health?names=chk2", nil)
	c.Assert(status, Equals, 200)
	c.Assert(response, DeepEquals, map[string]interface{}{
		"healthy": true,
	})

	status, response = serve(c, api, "GET", "/v1/health?names=chk3", nil)
	c.Assert(status, Equals, 200)
	c.Assert(response, DeepEquals, map[string]interface{}{
		"healthy": true,
	})
}

func (s *APISuite) TestHealthBadLevel(c *C) {
	api := httpapi.NewAPI(nil)
	status, response := serve(c, api, "GET", "/v1/health?level=foo", nil)

	c.Assert(status, Equals, 400)
	c.Assert(response, DeepEquals, map[string]interface{}{
		"error": `level must be "alive" or "ready"`,
	})
}

func (s *APISuite) TestHealthChecksError(c *C) {
	checkMgr := &checkManager{checks: func() ([]*checkstate.CheckInfo, error) {
		return nil, errors.New("oops!")
	}}
	api := httpapi.NewAPI(checkMgr)
	status, response := serve(c, api, "GET", "/v1/health", nil)

	c.Assert(status, Equals, 500)
	c.Assert(response, DeepEquals, map[string]interface{}{
		"error": "oops!",
	})
}

type checkManager struct {
	checks checksFunc
}

type checksFunc func() ([]*checkstate.CheckInfo, error)

func (c *checkManager) Checks() ([]*checkstate.CheckInfo, error) {
	return c.checks()
}
