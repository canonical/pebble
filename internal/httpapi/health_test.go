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
	checkMgr := &checkManager{checks: func(level plan.CheckLevel, names []string) ([]*checkstate.CheckInfo, error) {
		c.Assert(level, Equals, plan.UnsetLevel)
		c.Assert(names, HasLen, 0)
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
	checkMgr := &checkManager{checks: func(level plan.CheckLevel, names []string) ([]*checkstate.CheckInfo, error) {
		c.Assert(level, Equals, plan.UnsetLevel)
		c.Assert(names, HasLen, 0)
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

func (s *APISuite) TestHealthFilters(c *C) {
	checkMgr := &checkManager{checks: func(level plan.CheckLevel, names []string) ([]*checkstate.CheckInfo, error) {
		c.Assert(level, Equals, plan.AliveLevel)
		c.Assert(names, DeepEquals, []string{"chk1", "chk3"})
		return []*checkstate.CheckInfo{
			{Name: "chk1", Level: plan.AliveLevel, Healthy: true},
			{Name: "chk3", Level: plan.ReadyLevel, Healthy: true},
		}, nil
	}}
	api := httpapi.NewAPI(checkMgr)
	status, response := serve(c, api, "GET", "/v1/health?level=alive&names=chk1&names=chk3", nil)

	c.Assert(status, Equals, 200)
	c.Assert(response, DeepEquals, map[string]interface{}{
		"healthy": true,
	})
}

func (s *APISuite) TestHealthUnhealthy(c *C) {
	checkMgr := &checkManager{checks: func(level plan.CheckLevel, names []string) ([]*checkstate.CheckInfo, error) {
		c.Assert(level, Equals, plan.UnsetLevel)
		c.Assert(names, HasLen, 0)
		return []*checkstate.CheckInfo{
			{Name: "chk1", Healthy: true},
			{Name: "chk2", Healthy: false, Failures: 1, LastError: "error"},
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

func (s *APISuite) TestHealthBadLevel(c *C) {
	api := httpapi.NewAPI(nil)
	status, response := serve(c, api, "GET", "/v1/health?level=foo", nil)

	c.Assert(status, Equals, 400)
	c.Assert(response, DeepEquals, map[string]interface{}{
		"error": `level must be "alive" or "ready"`,
	})
}

func (s *APISuite) TestHealthChecksError(c *C) {
	checkMgr := &checkManager{checks: func(level plan.CheckLevel, names []string) ([]*checkstate.CheckInfo, error) {
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

type checksFunc func(level plan.CheckLevel, names []string) ([]*checkstate.CheckInfo, error)

func (c *checkManager) Checks(level plan.CheckLevel, names []string) ([]*checkstate.CheckInfo, error) {
	return c.checks(level, names)
}
