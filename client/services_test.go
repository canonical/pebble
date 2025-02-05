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

package client_test

import (
	"encoding/json"
	"net/url"
	"time"

	"gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
)

func (cs *clientSuite) TestStartStop(c *check.C) {
	cs.rsp = `{
		"result": {},
		"status": "OK",
		"status-code": 202,
		"type": "async",
		"change": "42"
	}`

	opts := client.ServiceOptions{
		Names: []string{"one", "two"},
	}

	for i := 0; i < 2; i++ {
		cs.req = nil

		startStop := cs.cli.Start
		action := "start"
		if i == 1 {
			startStop = cs.cli.Stop
			action = "stop"
		}

		changeId, err := startStop(&opts)
		c.Check(err, check.IsNil)
		c.Check(changeId, check.Equals, "42")
		c.Check(cs.req.Method, check.Equals, "POST")
		c.Check(cs.req.URL.Path, check.Equals, "/v1/services")

		var body map[string]any
		c.Assert(json.NewDecoder(cs.req.Body).Decode(&body), check.IsNil)
		c.Check(body, check.HasLen, 2)
		c.Check(body["action"], check.Equals, action)
		c.Check(body["services"], check.DeepEquals, []any{"one", "two"})
	}
}

func (cs *clientSuite) TestAutostart(c *check.C) {
	cs.rsp = `{
		"result": {},
		"status": "OK",
		"status-code": 202,
		"type": "async",
		"change": "42"
	}`

	opts := client.ServiceOptions{}

	changeId, err := cs.cli.AutoStart(&opts)
	c.Check(err, check.IsNil)
	c.Check(changeId, check.Equals, "42")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v1/services")

	var body map[string]any
	c.Assert(json.NewDecoder(cs.req.Body).Decode(&body), check.IsNil)
	c.Check(body, check.HasLen, 2)
	c.Check(body["action"], check.Equals, "autostart")
}

func (cs *clientSuite) TestServicesGet(c *check.C) {
	cs.rsp = `{
		"result": [
			{"name": "svc1", "startup": "enabled", "current": "inactive"},
			{"name": "svc2", "startup": "disabled", "current": "active", "current-since": "2022-04-28T17:05:23Z"}
		],
		"status": "OK",
		"status-code": 200,
		"type": "sync"
	}`

	opts := client.ServicesOptions{
		Names: []string{"svc1", "svc2"},
	}
	services, err := cs.cli.Services(&opts)
	c.Assert(err, check.IsNil)
	c.Assert(services, check.DeepEquals, []*client.ServiceInfo{
		{Name: "svc1", Startup: client.StartupEnabled, Current: client.StatusInactive},
		{Name: "svc2", Startup: client.StartupDisabled, Current: client.StatusActive, CurrentSince: time.Date(2022, 4, 28, 17, 5, 23, 0, time.UTC)},
	})
	c.Assert(cs.req.Method, check.Equals, "GET")
	c.Assert(cs.req.URL.Path, check.Equals, "/v1/services")
	c.Assert(cs.req.URL.Query(), check.DeepEquals, url.Values{
		"names": {"svc1,svc2"},
	})
}

func (cs *clientSuite) TestRestart(c *check.C) {
	cs.rsp = `{
		"result": {},
		"status": "OK",
		"status-code": 202,
		"type": "async",
		"change": "42"
	}`

	opts := client.ServiceOptions{
		Names: []string{"one", "two"},
	}

	changeId, err := cs.cli.Restart(&opts)
	c.Check(err, check.IsNil)
	c.Check(changeId, check.Equals, "42")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v1/services")

	var body map[string]any
	c.Assert(json.NewDecoder(cs.req.Body).Decode(&body), check.IsNil)
	c.Check(body, check.HasLen, 2)
	c.Check(body["action"], check.Equals, "restart")
	c.Check(body["services"], check.DeepEquals, []any{"one", "two"})
}

func (cs *clientSuite) TestReplan(c *check.C) {
	cs.rsp = `{
		"result": {},
		"status": "OK",
		"status-code": 202,
		"type": "async",
		"change": "42"
	}`

	opts := client.ServiceOptions{}

	changeId, err := cs.cli.Replan(&opts)
	c.Check(err, check.IsNil)
	c.Check(changeId, check.Equals, "42")
	c.Check(cs.req.Method, check.Equals, "POST")
	c.Check(cs.req.URL.Path, check.Equals, "/v1/services")

	var body map[string]any
	c.Assert(json.NewDecoder(cs.req.Body).Decode(&body), check.IsNil)
	c.Check(body, check.HasLen, 2)
	c.Check(body["action"], check.Equals, "replan")
}
