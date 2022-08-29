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
	"os"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
)

type makeDirPayload struct {
	Action string         `json:"action"`
	Dirs   []makeDirsItem `json:"dirs"`
}

type makeDirsItem struct {
	Path        string `json:"path"`
	MakeParents bool   `json:"make-parents"`
	Permissions string `json:"permissions"`
	UserID      *int   `json:"user-id"`
	User        string `json:"user"`
	GroupID     *int   `json:"group-id"`
	Group       string `json:"group"`
}

func (cs *clientSuite) TestMakeDir(c *C) {
	cs.rsp = `{"type": "sync", "result": [{"path": "/foo/bar"}]}`

	err := cs.cli.MakeDir(&client.MakeDirOptions{
		Path:        "/foo/bar",
		MakeParents: true,
	})
	c.Assert(err, IsNil)

	c.Assert(cs.req.URL.Path, Equals, "/v1/files")
	c.Assert(cs.req.Method, Equals, "POST")

	var payload makeDirPayload
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&payload)
	c.Assert(err, IsNil)
	c.Check(payload, DeepEquals, makeDirPayload{
		Action: "make-dirs",
		Dirs: []makeDirsItem{{
			Path:        "/foo/bar",
			MakeParents: true,
		}},
	})
}

func (cs *clientSuite) TestMakeDirWithPermissions(c *C) {
	cs.rsp = `{"type": "sync", "result": [{"path": "/foo/bar"}]}`

	err := cs.cli.MakeDir(&client.MakeDirOptions{
		Path:        "/foo/bar",
		MakeParents: true,
		Permissions: os.FileMode(0o644),
	})
	c.Assert(err, IsNil)

	c.Assert(cs.req.URL.Path, Equals, "/v1/files")
	c.Assert(cs.req.Method, Equals, "POST")

	var payload makeDirPayload
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&payload)
	c.Assert(err, IsNil)
	c.Check(payload, DeepEquals, makeDirPayload{
		Action: "make-dirs",
		Dirs: []makeDirsItem{{
			Path:        "/foo/bar",
			MakeParents: true,
			Permissions: "644",
		}},
	})
}

func (cs *clientSuite) TestMakeDirFails(c *C) {
	cs.rsp = `{"type": "error", "result": {"message": "could not foo"}}`
	err := cs.cli.MakeDir(&client.MakeDirOptions{
		Path: "/foobar",
	})
	c.Assert(err, ErrorMatches, "could not foo")

	c.Assert(cs.req.URL.Path, Equals, "/v1/files")
	c.Assert(cs.req.Method, Equals, "POST")

	var payload makeDirPayload
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&payload)
	c.Assert(err, IsNil)
	c.Check(payload, DeepEquals, makeDirPayload{
		Action: "make-dirs",
		Dirs: []makeDirsItem{{
			Path: "/foobar",
		}},
	})
}

func (cs *clientSuite) TestMakeDirFailsOnDirectory(c *C) {
	cs.rsp = `
{
	"type": "sync",
	"result": [
		{
			"path": "/foobar",
			"error": {
				"message": "could not bar",
				"kind": "permission-denied",
				"value": 42
			}
		}
	]
}`

	err := cs.cli.MakeDir(&client.MakeDirOptions{
		Path: "/foobar",
	})
	clientErr, ok := err.(*client.Error)
	c.Assert(ok, Equals, true)
	c.Assert(clientErr.Message, Equals, "could not bar")
	c.Assert(clientErr.Kind, Equals, "permission-denied")

	c.Assert(cs.req.URL.Path, Equals, "/v1/files")
	c.Assert(cs.req.Method, Equals, "POST")

	var payload makeDirPayload
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&payload)
	c.Assert(err, IsNil)
	c.Check(payload, DeepEquals, makeDirPayload{
		Action: "make-dirs",
		Dirs: []makeDirsItem{{
			Path: "/foobar",
		}},
	})
}

func (cs *clientSuite) TestMakeDirFailsWithMultipleAPIResults(c *C) {
	cs.rsp = `
{
	"type": "sync",
	"result": [
		{
			"path": "/foobar",
			"error": {
				"message": "could not bar",
				"kind": "permission-denied",
				"value": 42
			}
		},
		{
			"path": "/foobar",
			"error": {
				"message": "could not baz",
				"kind": "generic-file-error",
				"value": 47
			}
		}
	]
}`

	err := cs.cli.MakeDir(&client.MakeDirOptions{
		Path: "/foobar",
	})
	c.Assert(err, ErrorMatches, "expected exactly one result from API, got 2")

	c.Assert(cs.req.URL.Path, Equals, "/v1/files")
	c.Assert(cs.req.Method, Equals, "POST")

	var payload makeDirPayload
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&payload)
	c.Assert(err, IsNil)
	c.Check(payload, DeepEquals, makeDirPayload{
		Action: "make-dirs",
		Dirs: []makeDirsItem{{
			Path: "/foobar",
		}},
	})
}
