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
	"bytes"
	"encoding/json"
	"io"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
)

type writeFilesPayload struct {
	Action string           `json:"action"`
	Files  []writeFilesItem `json:"files"`
}

type writeFilesItem struct {
	Path        string `json:"path"`
	MakeDirs    bool   `json:"make-dirs"`
	Permissions string `json:"permissions"`
	UserID      *int   `json:"user-id"`
	User        string `json:"user"`
	GroupID     *int   `json:"group-id"`
	Group       string `json:"group"`
}

func (cs *clientSuite) TestPush(c *C) {
	cs.rsp = `{"type": "sync", "result": [{"path": "/file.dat"}]}`

	err := cs.cli.Push(&client.PushOptions{
		Path:   "/file.dat",
		Source: strings.NewReader("Hello, world!"),
	})
	c.Assert(err, IsNil)
	mr, err := cs.req.MultipartReader()
	c.Assert(err, IsNil)

	c.Assert(cs.req.URL.Path, Equals, "/v1/files")
	c.Assert(cs.req.Method, Equals, "POST")

	// Check metadata part
	metadata, err := mr.NextPart()
	c.Assert(metadata.Header.Get("Content-Type"), Equals, "application/json")
	c.Assert(metadata.FormName(), Equals, "request")

	buf := bytes.NewBuffer(make([]byte, 0))
	_, err = buf.ReadFrom(metadata)
	c.Assert(err, IsNil)

	// Decode metadata
	var payload writeFilesPayload
	err = json.NewDecoder(buf).Decode(&payload)
	c.Assert(err, IsNil)
	c.Assert(payload, DeepEquals, writeFilesPayload{
		Action: "write",
		Files: []writeFilesItem{{
			Path: "/file.dat",
		}},
	})

	// Check file part
	file, err := mr.NextPart()
	c.Assert(err, IsNil)
	c.Assert(file.Header.Get("Content-Type"), Equals, "application/octet-stream")
	c.Assert(file.FormName(), Equals, "files")
	c.Assert(file.FileName(), Equals, "file.dat")

	buf.Reset()
	_, err = buf.ReadFrom(file)
	c.Assert(err, IsNil)
	c.Assert(buf.String(), Equals, "Hello, world!")

	// Check end of multipart request
	_, err = mr.NextPart()
	c.Assert(err, Equals, io.EOF)
}

func (cs *clientSuite) TestPushFails(c *C) {
	cs.rsp = `{"type": "error", "result": {"message": "could not foo"}}`

	err := cs.cli.Push(&client.PushOptions{
		Path:   "/file.dat",
		Source: strings.NewReader("Hello, world!"),
	})
	c.Assert(err, ErrorMatches, "could not foo")
}

func (cs *clientSuite) TestPushFailsOnFile(c *C) {
	cs.rsp = `
{
	"type": "sync",
	"result": [
		{
			"path": "/file.dat",
			"error": {
				"message": "could not bar",
				"kind": "permission-denied",
				"value": 42
			}
		}
	]
}`

	err := cs.cli.Push(&client.PushOptions{
		Path:   "/file.dat",
		Source: strings.NewReader("Hello, world!"),
	})
	clientErr, ok := err.(*client.Error)
	c.Assert(ok, Equals, true)
	c.Assert(clientErr.Message, Equals, "could not bar")
	c.Assert(clientErr.Kind, Equals, "permission-denied")
}

func (cs *clientSuite) TestPushFailsWithMultipleAPIResults(c *C) {
	cs.rsp = `
{
	"type": "sync",
	"result": [
		{
			"path": "/file.dat",
			"error": {
				"message": "could not bar",
				"kind": "permission-denied",
				"value": 42
			}
		},
		{
			"path": "/file.dat",
			"error": {
				"message": "could not baz",
				"kind": "generic-file-error",
				"value": 41
			}
		}
	]
}`

	err := cs.cli.Push(&client.PushOptions{
		Path:   "/file.dat",
		Source: strings.NewReader("Hello, world!"),
	})
	c.Assert(err, ErrorMatches, "expected exactly one result from API, got 2")
}
