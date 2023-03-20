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
	"time"

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

func (cs *clientSuite) TestListFiles(c *C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
		"status": "OK",
		"result": [{
			"path": "/bin",
			"name": "bin",
			"type": "symlink",
			"permissions": "777",
			"last-modified": "2022-04-21T03:02:51Z",
			"user-id": 1000,
			"user": "toor",
			"group-id": 1000,
			"group": "toor"
		}, {
			"path": "/swap.img",
			"name": "swap.img",
			"type": "file",
			"size": 1337,
			"permissions": "655",
			"last-modified": "2022-04-21T03:02:51Z"
		}]
	}`
	result, err := cs.cli.ListFiles(&client.ListFilesOptions{
		Path: "/",
	})
	c.Assert(err, IsNil)
	c.Assert(result, HasLen, 2)

	c.Check(result[0].Path(), Equals, "/bin")
	c.Check(result[0].Name(), Equals, "bin")
	c.Check(result[0].Size(), Equals, int64(0))
	c.Check(result[0].Mode(), Equals, 0o777|os.ModeSymlink)
	c.Check(result[0].ModTime(), DeepEquals, time.Date(2022, 4, 21, 3, 2, 51, 0, time.UTC))
	c.Check(*result[0].UserID(), Equals, 1000)
	c.Check(*result[0].GroupID(), Equals, 1000)
	c.Check(result[0].User(), Equals, "toor")
	c.Check(result[0].Group(), Equals, "toor")
	c.Check(result[0].IsDir(), Equals, false)
	c.Check(result[0].Sys(), IsNil)

	c.Check(result[1].Path(), Equals, "/swap.img")
	c.Check(result[1].Name(), Equals, "swap.img")
	c.Check(result[1].Size(), Equals, int64(1337))
	c.Check(result[1].Mode(), Equals, os.FileMode(0o655))
	c.Check(result[1].ModTime(), DeepEquals, time.Date(2022, 4, 21, 3, 2, 51, 0, time.UTC))
	c.Check(result[1].UserID(), IsNil)
	c.Check(result[1].GroupID(), IsNil)
	c.Check(result[1].User(), Equals, "")
	c.Check(result[1].Group(), Equals, "")
	c.Check(result[1].IsDir(), Equals, false)
	c.Check(result[1].Sys(), IsNil)
}

func (cs *clientSuite) TestListDirectoryItself(c *C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
		"status": "OK",
		"result": [{
			"path": "/bin",
			"name": "bin",
			"type": "symlink",
			"permissions": "777",
			"last-modified": "2022-04-21T03:02:51Z",
			"user-id": 1000,
			"user": "user",
			"group-id": 1000,
			"group": "user"
		}]
	}`
	result, err := cs.cli.ListFiles(&client.ListFilesOptions{
		Path:   "/bin",
		Itself: true,
	})
	c.Assert(err, IsNil)
	c.Assert(result, HasLen, 1)
	c.Check(result[0].Name(), Equals, "bin")
	c.Check(result[0].Size(), Equals, int64(0))
	c.Check(result[0].Mode(), Equals, 0o777|os.ModeSymlink)
	c.Check(result[0].ModTime(), DeepEquals, time.Date(2022, 4, 21, 3, 2, 51, 0, time.UTC))
	c.Check(result[0].Path(), Equals, "/bin")
	c.Check(*result[0].UserID(), Equals, 1000)
	c.Check(*result[0].GroupID(), Equals, 1000)
	c.Check(result[0].User(), Equals, "user")
	c.Check(result[0].Group(), Equals, "user")
	c.Check(result[0].IsDir(), Equals, false)
	c.Check(result[0].Sys(), IsNil)
}

func (cs *clientSuite) TestListFilesWithPattern(c *C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
		"status": "OK",
		"result": [{
			"path": "/bin",
			"name": "bin",
			"type": "symlink",
			"permissions": "777",
			"last-modified": "2022-04-21T03:02:51Z",
			"user-id": 1000,
			"user": "user",
			"group-id": 1000,
			"group": "user"
		}]
	}`

	result, err := cs.cli.ListFiles(&client.ListFilesOptions{
		Path:    "/",
		Pattern: "[a-z][a-z][a-z]",
	})
	c.Assert(err, IsNil)
	c.Assert(result, HasLen, 1)
}

func (cs *clientSuite) TestListFilesFails(c *C) {
	cs.rsp = `{"type": "error", "result": {"message": "could not foo"}}`
	_, err := cs.cli.ListFiles(&client.ListFilesOptions{
		Path:   "/",
		Itself: true,
	})
	c.Assert(err, ErrorMatches, "could not foo")
}

func (cs *clientSuite) TestListFilesFailsWithInvalidDate(c *C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
		"status": "OK",
		"result": [{
			"path": "/irreg",
			"name": "irreg",
			"type": "sfdeljknesv",
			"permissions": "777",
			"last-modified": "2022-08-32T12:42:49Z",
			"user-id": 1000,
			"user": "toor",
			"group-id": 1000,
			"group": "toor"
		}]
	}`
	_, err := cs.cli.ListFiles(&client.ListFilesOptions{
		Path: "/irreg",
	})
	c.Assert(err, ErrorMatches, `remote file "irreg" has invalid last modified time: "2022-08-32T12:42:49Z"`)
}

func (cs *clientSuite) TestListFilesFailsWithInvalidPermissions(c *C) {
	cs.rsp = `{
		"type": "sync",
		"status-code": 200,
		"status": "OK",
		"result": [{
			"path": "/irreg",
			"name": "irreg",
			"type": "sfdeljknesv",
			"permissions": "not a number",
			"last-modified": "2022-08-32T12:42:49Z",
			"user-id": 1000,
			"user": "toor",
			"group-id": 1000,
			"group": "toor"
		}]
	}`
	_, err := cs.cli.ListFiles(&client.ListFilesOptions{
		Path: "/irreg",
	})
	c.Assert(err, ErrorMatches, `remote file "irreg" has invalid permission bits: "not a number"`)
}

func (cs *clientSuite) TestCalculateFileMode(c *C) {
	expectedResults := []struct {
		fileType, permissions string
		mode                  os.FileMode
	}{
		{"file", "655", 0o655},
		{"directory", "600", os.ModeDir | 0o600},
		{"symlink", "777", os.ModeSymlink | 0o777},
		{"socket", "750", os.ModeSocket | 0o750},
		{"named-pipe", "500", os.ModeNamedPipe | 0o500},
		{"device", "550", os.ModeDevice | 0o550},
		{"unknown", "766", os.ModeIrregular | 0o766},
		{"sfdeljknesv", "776", os.ModeIrregular | 0o776},
	}

	for _, expected := range expectedResults {
		mode, err := client.CalculateFileMode(expected.fileType, expected.permissions)
		c.Assert(err, IsNil)
		c.Check(mode, Equals, expected.mode)
	}
}

func (cs *clientSuite) TestCalculateFileModeFails(c *C) {
	for _, p := range []string{"-1", "x", "778"} {
		_, err := client.CalculateFileMode("file", p)
		c.Check(err, ErrorMatches, `invalid permission bits: ".*"`)
	}
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

func (cs *clientSuite) TestMakeDirWithSpecialPermissions(c *C) {
	cs.rsp = `{"type": "sync", "result": [{"path": "/foo/bar"}]}`

	err := cs.cli.MakeDir(&client.MakeDirOptions{
		Path:        "/foo/bar",
		MakeParents: true,
		Permissions: os.FileMode(0o1077),
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
			Permissions: "1077",
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
	cs.rsp = ` {
		"type": "sync",
		"result": [{
			"path": "/foobar",
			"error": {
				"message": "could not bar",
				"kind": "permission-denied",
				"value": 42
			}
		}]
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
	cs.rsp = ` {
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

func (cs *clientSuite) TestRemovePathFailsWithMultipleAPIResults(c *C) {
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
