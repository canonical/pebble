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

package client_test

import (
	"encoding/json"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
)

type removePathsPayload struct {
	Action string            `json:"action"`
	Paths  []removePathsItem `json:"paths"`
}

type removePathsItem struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
}

func (cs *clientSuite) TestRemovePath(c *C) {
	cs.rsp = `{"type": "sync", "result": [{"path": "/foo/bar"}]}`

	err := cs.cli.RemovePath(&client.RemovePathOptions{
		Path: "/foo/bar",
	})
	c.Assert(err, IsNil)

	c.Assert(cs.req.URL.Path, Equals, "/v1/files")
	c.Assert(cs.req.Method, Equals, "POST")

	var payload removePathsPayload
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&payload)
	c.Assert(err, IsNil)
	c.Check(payload, DeepEquals, removePathsPayload{
		Action: "remove",
		Paths: []removePathsItem{{
			Path: "/foo/bar",
		}},
	})
}

func (cs *clientSuite) TestRemovePathRecursive(c *C) {
	cs.rsp = `{"type": "sync", "result": [{"path": "/foo/bar"}]}`

	err := cs.cli.RemovePath(&client.RemovePathOptions{
		Path:      "/foo/bar",
		Recursive: true,
	})
	c.Assert(err, IsNil)

	c.Assert(cs.req.URL.Path, Equals, "/v1/files")
	c.Assert(cs.req.Method, Equals, "POST")

	var payload removePathsPayload
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&payload)
	c.Assert(err, IsNil)
	c.Check(payload, DeepEquals, removePathsPayload{
		Action: "remove",
		Paths: []removePathsItem{{
			Path:      "/foo/bar",
			Recursive: true,
		}},
	})
}

func (cs *clientSuite) TestRemovePathFails(c *C) {
	cs.rsp = `{"type": "error", "result": {"message": "could not foo"}}`
	err := cs.cli.RemovePath(&client.RemovePathOptions{
		Path: "/foobar",
	})
	c.Assert(err, ErrorMatches, "could not foo")

	c.Assert(cs.req.URL.Path, Equals, "/v1/files")
	c.Assert(cs.req.Method, Equals, "POST")

	var payload removePathsPayload
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&payload)
	c.Assert(err, IsNil)
	c.Check(payload, DeepEquals, removePathsPayload{
		Action: "remove",
		Paths: []removePathsItem{{
			Path: "/foobar",
		}},
	})
}

func (cs *clientSuite) TestRemovePathFailsOnPath(c *C) {
	cs.rsp = ` {
		"type": "sync",
		"result": [{
			"path": "/foo/bar/baz.qux",
			"error": {
				"message": "could not bar",
				"kind": "permission-denied",
				"value": 42
			}
		}]
	}`

	err := cs.cli.RemovePath(&client.RemovePathOptions{
		Path:      "/foo/bar",
		Recursive: true,
	})
	clientErr, ok := err.(*client.Error)
	c.Assert(ok, Equals, true)
	c.Assert(clientErr.Message, Equals, "could not bar")
	c.Assert(clientErr.Kind, Equals, "permission-denied")

	c.Assert(cs.req.URL.Path, Equals, "/v1/files")
	c.Assert(cs.req.Method, Equals, "POST")

	var payload removePathsPayload
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&payload)
	c.Assert(err, IsNil)
	c.Check(payload, DeepEquals, removePathsPayload{
		Action: "remove",
		Paths: []removePathsItem{{
			Path:      "/foo/bar",
			Recursive: true,
		}},
	})
}

func (cs *clientSuite) TestMakeDirFailsWithMultipleAPIResults(c *C) {
	cs.rsp = `{
		"type": "sync",
		"result": [{
			"path": "/foobar",
			"error": {
				"message": "could not bar",
				"kind": "permission-denied",
				"value": 42
			}
		}, {
			"path": "/foobar",
			"error": {
				"message": "could not baz",
				"kind": "generic-file-error",
				"value": 47
			}
		}]
	}`

	err := cs.cli.RemovePath(&client.RemovePathOptions{
		Path: "/foobar",
	})
	c.Assert(err, ErrorMatches, "expected exactly one result from API, got 2")

	c.Assert(cs.req.URL.Path, Equals, "/v1/files")
	c.Assert(cs.req.Method, Equals, "POST")

	var payload removePathsPayload
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&payload)
	c.Assert(err, IsNil)
	c.Check(payload, DeepEquals, removePathsPayload{
		Action: "remove",
		Paths: []removePathsItem{{
			Path: "/foobar",
		}},
	})
}
