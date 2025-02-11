// Copyright (c) 2022 Canonical Ltd
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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/internals/overlord"
	"github.com/canonical/pebble/internals/overlord/checkstate"
	"github.com/canonical/pebble/internals/plan"
)

var _ = Suite(&healthSuite{})

type healthSuite struct{}

func (s *healthSuite) TestNoChecks(c *C) {
	restore := FakeGetChecks(func(o *overlord.Overlord) ([]*checkstate.CheckInfo, error) {
		return nil, nil
	})
	defer restore()

	status, response := serveHealth(c, "GET", "/v1/health", nil)

	c.Assert(status, Equals, 200)
	c.Assert(response, DeepEquals, map[string]any{
		"healthy": true,
	})
}

func (s *healthSuite) TestHealthy(c *C) {
	restore := FakeGetChecks(func(o *overlord.Overlord) ([]*checkstate.CheckInfo, error) {
		return []*checkstate.CheckInfo{
			{Name: "chk1", Status: checkstate.CheckStatusUp},
			{Name: "chk2", Status: checkstate.CheckStatusUp},
			{Name: "chk3", Status: checkstate.CheckStatusInactive},
		}, nil
	})
	defer restore()

	status, response := serveHealth(c, "GET", "/v1/health", nil)

	c.Assert(status, Equals, 200)
	c.Assert(response, DeepEquals, map[string]any{
		"healthy": true,
	})
}

func (s *healthSuite) TestUnhealthy(c *C) {
	restore := FakeGetChecks(func(o *overlord.Overlord) ([]*checkstate.CheckInfo, error) {
		return []*checkstate.CheckInfo{
			{Name: "chk1", Status: checkstate.CheckStatusUp},
			{Name: "chk2", Status: checkstate.CheckStatusDown},
			{Name: "chk3", Status: checkstate.CheckStatusUp},
			{Name: "chk4", Status: checkstate.CheckStatusInactive},
		}, nil
	})
	defer restore()

	status, response := serveHealth(c, "GET", "/v1/health", nil)

	c.Assert(status, Equals, 502)
	c.Assert(response, DeepEquals, map[string]any{
		"healthy": false,
	})
}

func (s *healthSuite) TestLevel(c *C) {
	type levelTest struct {
		aliveCheck   string // alive check: "up", "down", or no alive check
		readyCheck   string // ready check: "up", "down", or no ready check
		aliveHealthy bool   // expected response with ?level=alive filter
		readyHealthy bool   // expected response with ?level=ready filter
	}

	// All combinations of alive and ready checks (ready implies alive).
	tests := []levelTest{
		{aliveCheck: "up", readyCheck: "up", aliveHealthy: true, readyHealthy: true},       // alive and ready
		{aliveCheck: "up", readyCheck: "down", aliveHealthy: true, readyHealthy: false},    // alive but not ready
		{aliveCheck: "down", readyCheck: "up", aliveHealthy: false, readyHealthy: false},   // not alive but ready => ready unhealthy
		{aliveCheck: "down", readyCheck: "down", aliveHealthy: false, readyHealthy: false}, // not alive nor ready
		{aliveCheck: "up", readyCheck: "", aliveHealthy: true, readyHealthy: true},         // alive and no ready check
		{aliveCheck: "down", readyCheck: "", aliveHealthy: false, readyHealthy: false},     // not alive, no ready check => ready unhealthy
		{aliveCheck: "", readyCheck: "up", aliveHealthy: true, readyHealthy: true},         // no alive check, but ready
		{aliveCheck: "", readyCheck: "down", aliveHealthy: true, readyHealthy: false},      // no alive check, not ready
		{aliveCheck: "", readyCheck: "", aliveHealthy: true, readyHealthy: true},           // no alive or ready check
	}

	for _, test := range tests {
		func() {
			c.Logf("TestHealthLevels check alive=%q ready=%q, healthy alive=%t ready=%t",
				test.aliveCheck, test.readyCheck, test.aliveHealthy, test.readyHealthy)

			restore := FakeGetChecks(func(o *overlord.Overlord) ([]*checkstate.CheckInfo, error) {
				var checks []*checkstate.CheckInfo
				if test.aliveCheck != "" {
					checks = append(checks, &checkstate.CheckInfo{Name: "a", Level: plan.AliveLevel, Status: checkstate.CheckStatus(test.aliveCheck)})
				}
				if test.readyCheck != "" {
					checks = append(checks, &checkstate.CheckInfo{Name: "r", Level: plan.ReadyLevel, Status: checkstate.CheckStatus(test.readyCheck)})
				}
				// Add a check which is down with level unset, to ensure that
				// the level-unset checks do not impact the outcomes of level-queries.
				checks = append(checks, &checkstate.CheckInfo{Name: "u", Level: plan.UnsetLevel, Status: checkstate.CheckStatusDown})
				return checks, nil
			})
			defer restore()

			status, response := serveHealth(c, "GET", "/v1/health?level=alive", nil)
			if test.aliveHealthy {
				c.Check(status, Equals, 200)
				c.Check(response, DeepEquals, map[string]any{"healthy": true})
			} else {
				c.Check(status, Equals, 502)
				c.Check(response, DeepEquals, map[string]any{"healthy": false})
			}

			status, response = serveHealth(c, "GET", "/v1/health?level=ready", nil)
			if test.readyHealthy {
				c.Check(status, Equals, 200)
				c.Check(response, DeepEquals, map[string]any{"healthy": true})
			} else {
				c.Check(status, Equals, 502)
				c.Check(response, DeepEquals, map[string]any{"healthy": false})
			}
		}()
	}
}

func (s *healthSuite) TestNames(c *C) {
	restore := FakeGetChecks(func(o *overlord.Overlord) ([]*checkstate.CheckInfo, error) {
		return []*checkstate.CheckInfo{
			{Name: "chk1", Status: checkstate.CheckStatusDown},
			{Name: "chk2", Status: checkstate.CheckStatusUp},
			{Name: "chk3", Status: checkstate.CheckStatusUp},
			{Name: "chk4", Status: checkstate.CheckStatusInactive},
		}, nil
	})
	defer restore()

	status, response := serveHealth(c, "GET", "/v1/health?names=chk1&names=chk3", nil)
	c.Assert(status, Equals, 502)
	c.Assert(response, DeepEquals, map[string]any{
		"healthy": false,
	})

	status, response = serveHealth(c, "GET", "/v1/health?names=chk1,chk3", nil)
	c.Assert(status, Equals, 502)
	c.Assert(response, DeepEquals, map[string]any{
		"healthy": false,
	})

	status, response = serveHealth(c, "GET", "/v1/health?names=chk2", nil)
	c.Assert(status, Equals, 200)
	c.Assert(response, DeepEquals, map[string]any{
		"healthy": true,
	})

	status, response = serveHealth(c, "GET", "/v1/health?names=chk3", nil)
	c.Assert(status, Equals, 200)
	c.Assert(response, DeepEquals, map[string]any{
		"healthy": true,
	})

	// With only an inactive check, this is the same as no checks, so healthy.
	status, response = serveHealth(c, "GET", "/v1/health?names=chk4", nil)
	c.Assert(status, Equals, 200)
	c.Assert(response, DeepEquals, map[string]any{
		"healthy": true,
	})

	// One healthy check, one that should be ignored.
	status, response = serveHealth(c, "GET", "/v1/health?names=chk2,chk4", nil)
	c.Assert(status, Equals, 200)
	c.Assert(response, DeepEquals, map[string]any{
		"healthy": true,
	})

	// One unhealthy check, one that should be ignored.
	status, response = serveHealth(c, "GET", "/v1/health?names=chk1,chk4", nil)
	c.Assert(status, Equals, 502)
	c.Assert(response, DeepEquals, map[string]any{
		"healthy": false,
	})
}

func (s *healthSuite) TestBadLevel(c *C) {
	restore := FakeGetChecks(func(o *overlord.Overlord) ([]*checkstate.CheckInfo, error) {
		return nil, nil
	})
	defer restore()

	status, response := serveHealth(c, "GET", "/v1/health?level=foo", nil)

	c.Assert(status, Equals, 400)
	c.Assert(response, DeepEquals, map[string]any{
		"message": `level must be "alive" or "ready"`,
	})
}

func (s *healthSuite) TestChecksError(c *C) {
	restore := FakeGetChecks(func(o *overlord.Overlord) ([]*checkstate.CheckInfo, error) {
		return nil, errors.New("oops!")
	})
	defer restore()

	status, response := serveHealth(c, "GET", "/v1/health", nil)

	c.Assert(status, Equals, 500)
	c.Assert(response, DeepEquals, map[string]any{
		"message": "internal server error",
	})
}

// Ensure state lock is not held at all for GET /v1/health requests.
// Regression test for issue described at:
//
// - https://github.com/canonical/pebble/issues/366
// - https://bugs.launchpad.net/juju/+bug/2052517
func (s *apiSuite) TestHealthStateLockNotHeldSuccess(c *C) {
	s.testHealthStateLockNotHeld(c, "", false)
}

func (s *apiSuite) TestHealthStateLockNotHeldError(c *C) {
	s.testHealthStateLockNotHeld(c, "badlevel", true)
}

func (s *apiSuite) testHealthStateLockNotHeld(c *C, level string, expectErr bool) {
	daemonOpts := &Options{
		Dir:        s.pebbleDir,
		SocketPath: s.pebbleDir + ".pebble.socket",
	}
	daemon, err := New(daemonOpts)
	c.Assert(err, IsNil)
	c.Assert(daemon.Init(), IsNil)
	c.Assert(daemon.Start(), IsNil)
	defer func() {
		c.Assert(daemon.Stop(nil), IsNil)
	}()

	// Acquire state lock so that the health endpoint can't.
	daemon.state.Lock()
	defer daemon.state.Unlock()

	// Call health check endpoint in a goroutine so we can have a timeout.
	errCh := make(chan error)
	go func() (err error) {
		defer func() {
			errCh <- err
		}()

		// Use real HTTP client so we're exercising the full ServeHTTP flow.
		// Could use daemon.serve.Handler directly, but this seems even better.
		pebble, err := client.New(&client.Config{Socket: daemonOpts.SocketPath})
		if err != nil {
			return err
		}
		healthy, err := pebble.Health(&client.HealthOptions{
			Level: client.CheckLevel(level),
		})
		if err != nil {
			return err
		}
		if !healthy {
			return fmt.Errorf("/v1/health returned false")
		}
		return nil
	}()

	select {
	case healthErr := <-errCh:
		if expectErr {
			c.Assert(healthErr, NotNil)
		} else {
			c.Assert(healthErr, IsNil)
		}
	case <-time.After(5 * time.Second):
		c.Fatalf("timed out waiting for /v1/health - it must be trying to acquire the state lock")
	}
}

func serveHealth(c *C, method, url string, body io.Reader) (int, map[string]any) {
	recorder := httptest.NewRecorder()
	request, err := http.NewRequest(method, url, body)
	c.Assert(err, IsNil)

	server := v1Health(&Command{d: &Daemon{}}, request, nil)
	server.ServeHTTP(recorder, request)

	c.Assert(recorder.Result().Header.Get("Content-Type"), Equals, "application/json")
	var response map[string]any
	err = json.NewDecoder(recorder.Result().Body).Decode(&response)
	c.Assert(err, IsNil)
	return recorder.Result().StatusCode, response["result"].(map[string]any)
}
