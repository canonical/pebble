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
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path"
	"strings"
	"time"

	"github.com/canonical/tc"

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

func (cs *clientSuite) TestListFiles(c *tc.C) {
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
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(result, tc.HasLen, 2)

	c.Check(result[0].Path(), tc.Equals, "/bin")
	c.Check(result[0].Name(), tc.Equals, "bin")
	c.Check(result[0].Size(), tc.Equals, int64(0))
	c.Check(result[0].Mode(), tc.Equals, 0o777|os.ModeSymlink)
	c.Check(result[0].ModTime(), tc.DeepEquals, time.Date(2022, 4, 21, 3, 2, 51, 0, time.UTC))
	c.Check(*result[0].UserID(), tc.Equals, 1000)
	c.Check(*result[0].GroupID(), tc.Equals, 1000)
	c.Check(result[0].User(), tc.Equals, "toor")
	c.Check(result[0].Group(), tc.Equals, "toor")
	c.Check(result[0].IsDir(), tc.Equals, false)
	c.Check(result[0].Sys(), tc.IsNil)

	c.Check(result[1].Path(), tc.Equals, "/swap.img")
	c.Check(result[1].Name(), tc.Equals, "swap.img")
	c.Check(result[1].Size(), tc.Equals, int64(1337))
	c.Check(result[1].Mode(), tc.Equals, os.FileMode(0o655))
	c.Check(result[1].ModTime(), tc.DeepEquals, time.Date(2022, 4, 21, 3, 2, 51, 0, time.UTC))
	c.Check(result[1].UserID(), tc.IsNil)
	c.Check(result[1].GroupID(), tc.IsNil)
	c.Check(result[1].User(), tc.Equals, "")
	c.Check(result[1].Group(), tc.Equals, "")
	c.Check(result[1].IsDir(), tc.Equals, false)
	c.Check(result[1].Sys(), tc.IsNil)
}

func (cs *clientSuite) TestListDirectoryItself(c *tc.C) {
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
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(result, tc.HasLen, 1)
	c.Check(result[0].Name(), tc.Equals, "bin")
	c.Check(result[0].Size(), tc.Equals, int64(0))
	c.Check(result[0].Mode(), tc.Equals, 0o777|os.ModeSymlink)
	c.Check(result[0].ModTime(), tc.DeepEquals, time.Date(2022, 4, 21, 3, 2, 51, 0, time.UTC))
	c.Check(result[0].Path(), tc.Equals, "/bin")
	c.Check(*result[0].UserID(), tc.Equals, 1000)
	c.Check(*result[0].GroupID(), tc.Equals, 1000)
	c.Check(result[0].User(), tc.Equals, "user")
	c.Check(result[0].Group(), tc.Equals, "user")
	c.Check(result[0].IsDir(), tc.Equals, false)
	c.Check(result[0].Sys(), tc.IsNil)
}

func (cs *clientSuite) TestListFilesWithPattern(c *tc.C) {
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
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(result, tc.HasLen, 1)
}

func (cs *clientSuite) TestListFilesFails(c *tc.C) {
	cs.rsp = `{"type": "error", "result": {"message": "could not foo"}}`
	_, err := cs.cli.ListFiles(&client.ListFilesOptions{
		Path:   "/",
		Itself: true,
	})
	c.Assert(err, tc.ErrorMatches, "could not foo")
}

func (cs *clientSuite) TestListFilesFailsWithInvalidDate(c *tc.C) {
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
	c.Assert(err, tc.ErrorMatches, `remote file "irreg" has invalid last modified time: "2022-08-32T12:42:49Z"`)
}

func (cs *clientSuite) TestListFilesFailsWithInvalidPermissions(c *tc.C) {
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
	c.Assert(err, tc.ErrorMatches, `remote file "irreg" has invalid permission bits: "not a number"`)
}

func (cs *clientSuite) TestCalculateFileMode(c *tc.C) {
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
		c.Assert(err, tc.ErrorIsNil)
		c.Check(mode, tc.Equals, expected.mode)
	}
}

func (cs *clientSuite) TestCalculateFileModeFails(c *tc.C) {
	for _, p := range []string{"-1", "x", "778"} {
		_, err := client.CalculateFileMode("file", p)
		c.Check(err, tc.ErrorMatches, `invalid permission bits: ".*"`)
	}
}

func (cs *clientSuite) TestMakeDir(c *tc.C) {
	cs.rsp = `{"type": "sync", "result": [{"path": "/foo/bar"}]}`

	err := cs.cli.MakeDir(&client.MakeDirOptions{
		Path:        "/foo/bar",
		MakeParents: true,
	})
	c.Assert(err, tc.ErrorIsNil)

	c.Assert(cs.req.URL.Path, tc.Equals, "/v1/files")
	c.Assert(cs.req.Method, tc.Equals, "POST")

	var payload makeDirPayload
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&payload)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(payload, tc.DeepEquals, makeDirPayload{
		Action: "make-dirs",
		Dirs: []makeDirsItem{{
			Path:        "/foo/bar",
			MakeParents: true,
		}},
	})
}

func (cs *clientSuite) TestMakeDirWithPermissions(c *tc.C) {
	cs.rsp = `{"type": "sync", "result": [{"path": "/foo/bar"}]}`

	err := cs.cli.MakeDir(&client.MakeDirOptions{
		Path:        "/foo/bar",
		MakeParents: true,
		Permissions: os.FileMode(0o644),
	})
	c.Assert(err, tc.ErrorIsNil)

	c.Assert(cs.req.URL.Path, tc.Equals, "/v1/files")
	c.Assert(cs.req.Method, tc.Equals, "POST")

	var payload makeDirPayload
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&payload)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(payload, tc.DeepEquals, makeDirPayload{
		Action: "make-dirs",
		Dirs: []makeDirsItem{{
			Path:        "/foo/bar",
			MakeParents: true,
			Permissions: "644",
		}},
	})
}

func (cs *clientSuite) TestMakeDirWithSpecialPermissions(c *tc.C) {
	cs.rsp = `{"type": "sync", "result": [{"path": "/foo/bar"}]}`

	err := cs.cli.MakeDir(&client.MakeDirOptions{
		Path:        "/foo/bar",
		MakeParents: true,
		Permissions: os.FileMode(0o1077),
	})
	c.Assert(err, tc.ErrorIsNil)

	c.Assert(cs.req.URL.Path, tc.Equals, "/v1/files")
	c.Assert(cs.req.Method, tc.Equals, "POST")

	var payload makeDirPayload
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&payload)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(payload, tc.DeepEquals, makeDirPayload{
		Action: "make-dirs",
		Dirs: []makeDirsItem{{
			Path:        "/foo/bar",
			MakeParents: true,
			Permissions: "1077",
		}},
	})
}

func (cs *clientSuite) TestMakeDirFails(c *tc.C) {
	cs.rsp = `{"type": "error", "result": {"message": "could not foo"}}`
	err := cs.cli.MakeDir(&client.MakeDirOptions{
		Path: "/foobar",
	})
	c.Assert(err, tc.ErrorMatches, "could not foo")

	c.Assert(cs.req.URL.Path, tc.Equals, "/v1/files")
	c.Assert(cs.req.Method, tc.Equals, "POST")

	var payload makeDirPayload
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&payload)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(payload, tc.DeepEquals, makeDirPayload{
		Action: "make-dirs",
		Dirs: []makeDirsItem{{
			Path: "/foobar",
		}},
	})
}

func (cs *clientSuite) TestMakeDirFailsOnDirectory(c *tc.C) {
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
	c.Assert(ok, tc.Equals, true)
	c.Assert(clientErr.Message, tc.Equals, "could not bar")
	c.Assert(clientErr.Kind, tc.Equals, "permission-denied")

	c.Assert(cs.req.URL.Path, tc.Equals, "/v1/files")
	c.Assert(cs.req.Method, tc.Equals, "POST")

	var payload makeDirPayload
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&payload)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(payload, tc.DeepEquals, makeDirPayload{
		Action: "make-dirs",
		Dirs: []makeDirsItem{{
			Path: "/foobar",
		}},
	})
}

func (cs *clientSuite) TestMakeDirFailsWithMultipleAPIResults(c *tc.C) {
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
	c.Assert(err, tc.ErrorMatches, "expected exactly one result from API, got 2")

	c.Assert(cs.req.URL.Path, tc.Equals, "/v1/files")
	c.Assert(cs.req.Method, tc.Equals, "POST")

	var payload makeDirPayload
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&payload)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(payload, tc.DeepEquals, makeDirPayload{
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

func (cs *clientSuite) TestRemovePath(c *tc.C) {
	cs.rsp = `{"type": "sync", "result": [{"path": "/foo/bar"}]}`

	err := cs.cli.RemovePath(&client.RemovePathOptions{
		Path: "/foo/bar",
	})
	c.Assert(err, tc.ErrorIsNil)

	c.Assert(cs.req.URL.Path, tc.Equals, "/v1/files")
	c.Assert(cs.req.Method, tc.Equals, "POST")

	var payload removePathsPayload
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&payload)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(payload, tc.DeepEquals, removePathsPayload{
		Action: "remove",
		Paths: []removePathsItem{{
			Path: "/foo/bar",
		}},
	})
}

func (cs *clientSuite) TestRemovePathRecursive(c *tc.C) {
	cs.rsp = `{"type": "sync", "result": [{"path": "/foo/bar"}]}`

	err := cs.cli.RemovePath(&client.RemovePathOptions{
		Path:      "/foo/bar",
		Recursive: true,
	})
	c.Assert(err, tc.ErrorIsNil)

	c.Assert(cs.req.URL.Path, tc.Equals, "/v1/files")
	c.Assert(cs.req.Method, tc.Equals, "POST")

	var payload removePathsPayload
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&payload)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(payload, tc.DeepEquals, removePathsPayload{
		Action: "remove",
		Paths: []removePathsItem{{
			Path:      "/foo/bar",
			Recursive: true,
		}},
	})
}

func (cs *clientSuite) TestRemovePathFails(c *tc.C) {
	cs.rsp = `{"type": "error", "result": {"message": "could not foo"}}`
	err := cs.cli.RemovePath(&client.RemovePathOptions{
		Path: "/foobar",
	})
	c.Assert(err, tc.ErrorMatches, "could not foo")

	c.Assert(cs.req.URL.Path, tc.Equals, "/v1/files")
	c.Assert(cs.req.Method, tc.Equals, "POST")

	var payload removePathsPayload
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&payload)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(payload, tc.DeepEquals, removePathsPayload{
		Action: "remove",
		Paths: []removePathsItem{{
			Path: "/foobar",
		}},
	})
}

func (cs *clientSuite) TestRemovePathFailsOnPath(c *tc.C) {
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
	c.Assert(ok, tc.Equals, true)
	c.Assert(clientErr.Message, tc.Equals, "could not bar")
	c.Assert(clientErr.Kind, tc.Equals, "permission-denied")

	c.Assert(cs.req.URL.Path, tc.Equals, "/v1/files")
	c.Assert(cs.req.Method, tc.Equals, "POST")

	var payload removePathsPayload
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&payload)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(payload, tc.DeepEquals, removePathsPayload{
		Action: "remove",
		Paths: []removePathsItem{{
			Path:      "/foo/bar",
			Recursive: true,
		}},
	})
}

func (cs *clientSuite) TestRemovePathFailsWithMultipleAPIResults(c *tc.C) {
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
	c.Assert(err, tc.ErrorMatches, "expected exactly one result from API, got 2")

	c.Assert(cs.req.URL.Path, tc.Equals, "/v1/files")
	c.Assert(cs.req.Method, tc.Equals, "POST")

	var payload removePathsPayload
	decoder := json.NewDecoder(cs.req.Body)
	err = decoder.Decode(&payload)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(payload, tc.DeepEquals, removePathsPayload{
		Action: "remove",
		Paths: []removePathsItem{{
			Path: "/foobar",
		}},
	})
}

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

func (cs *clientSuite) TestPush(c *tc.C) {
	cs.rsp = `{"type": "sync", "result": [{"path": "/file.dat"}]}`

	err := cs.cli.Push(&client.PushOptions{
		Path:   "/file.dat",
		Source: strings.NewReader("Hello, world!"),
	})
	c.Assert(err, tc.ErrorIsNil)
	mr, err := cs.req.MultipartReader()
	c.Assert(err, tc.ErrorIsNil)

	c.Assert(cs.req.URL.Path, tc.Equals, "/v1/files")
	c.Assert(cs.req.Method, tc.Equals, "POST")

	// Check metadata part
	metadata, err := mr.NextPart()
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(metadata.Header.Get("Content-Type"), tc.Equals, "application/json")
	c.Assert(metadata.FormName(), tc.Equals, "request")

	buf := bytes.NewBuffer(make([]byte, 0))
	_, err = buf.ReadFrom(metadata)
	c.Assert(err, tc.ErrorIsNil)

	// Decode metadata
	var payload writeFilesPayload
	err = json.NewDecoder(buf).Decode(&payload)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(payload, tc.DeepEquals, writeFilesPayload{
		Action: "write",
		Files: []writeFilesItem{{
			Path: "/file.dat",
		}},
	})

	// Check file part
	file, err := mr.NextPart()
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(file.Header.Get("Content-Type"), tc.Equals, "application/octet-stream")
	c.Assert(file.FormName(), tc.Equals, "files")
	c.Assert(path.Base(file.FileName()), tc.Equals, "file.dat")

	buf.Reset()
	_, err = buf.ReadFrom(file)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(buf.String(), tc.Equals, "Hello, world!")

	// Check end of multipart request
	_, err = mr.NextPart()
	c.Assert(err, tc.Equals, io.EOF)
}

func (cs *clientSuite) TestPushFails(c *tc.C) {
	cs.rsp = `{"type": "error", "result": {"message": "could not foo"}}`

	err := cs.cli.Push(&client.PushOptions{
		Path:   "/file.dat",
		Source: strings.NewReader("Hello, world!"),
	})
	c.Assert(err, tc.ErrorMatches, "could not foo")
}

func (cs *clientSuite) TestPushFailsOnFile(c *tc.C) {
	cs.rsp = `{
		"type": "sync",
		"result": [{
			"path": "/file.dat",
			"error": {
				"message": "could not bar",
				"kind": "permission-denied",
				"value": 42
			}
		}]
	}`

	err := cs.cli.Push(&client.PushOptions{
		Path:   "/file.dat",
		Source: strings.NewReader("Hello, world!"),
	})
	clientErr, ok := err.(*client.Error)
	c.Assert(ok, tc.Equals, true)
	c.Assert(clientErr.Message, tc.Equals, "could not bar")
	c.Assert(clientErr.Kind, tc.Equals, "permission-denied")
}

func (cs *clientSuite) TestPushFailsWithMultipleAPIResults(c *tc.C) {
	cs.rsp = `{
		"type": "sync",
		"result": [{
			"path": "/file.dat",
			"error": {
				"message": "could not bar",
				"kind": "permission-denied",
				"value": 42
			}
		}, {
			"path": "/file.dat",
			"error": {
				"message": "could not baz",
				"kind": "generic-file-error",
				"value": 41
			}
		}]
	}`

	err := cs.cli.Push(&client.PushOptions{
		Path:   "/file.dat",
		Source: strings.NewReader("Hello, world!"),
	})
	c.Assert(err, tc.ErrorMatches, "expected exactly one result from API, got 2")
}

func (cs *clientSuite) TestPull(c *tc.C) {
	// Craft multipart response
	var srcBuf bytes.Buffer
	mw := multipart.NewWriter(&srcBuf)

	cs.header = http.Header{}
	cs.header.Set("Content-Type", mw.FormDataContentType())
	cs.status = http.StatusOK

	fw, err := mw.CreateFormFile("files", "/foo/bar.dat")
	c.Assert(err, tc.ErrorIsNil)
	fw.Write([]byte("Hello, world!"))

	mh := textproto.MIMEHeader{}
	mh.Set("Content-Type", "application/json")
	mh.Set("Content-Disposition", `form-data; name="response"`)

	part, err := mw.CreatePart(mh)
	c.Assert(err, tc.ErrorIsNil)
	fmt.Fprintf(part, `{
		"type": "sync",
		"status-code": 200,
		"status": "OK",
		"result": [{"path": "/foo/bar.dat"}]
	}`)

	mw.Close()
	cs.rsp = srcBuf.String()

	// Check response
	var targetBuf bytes.Buffer
	err = cs.cli.Pull(&client.PullOptions{
		Path:   "/foo/bar.dat",
		Target: &targetBuf,
	})
	c.Assert(err, tc.ErrorIsNil)
	c.Check(targetBuf.String(), tc.Equals, "Hello, world!")
}

func (cs *clientSuite) TestPullFailsWithNoContentType(c *tc.C) {
	// Check response
	var targetBuf bytes.Buffer
	err := cs.cli.Pull(&client.PullOptions{
		Path:   "/foo/bar.dat",
		Target: &targetBuf,
	})
	c.Assert(err, tc.ErrorMatches, "cannot parse Content-Type: .*")
}

func (cs *clientSuite) TestPullFailsWithNonMultipartResponse(c *tc.C) {
	cs.header = http.Header{}
	cs.header.Set("Content-Type", "text/plain; charset=utf-8")
	cs.rsp = "Hello, world!"

	// Check response
	var targetBuf bytes.Buffer
	err := cs.cli.Pull(&client.PullOptions{
		Path:   "/foo/bar.dat",
		Target: &targetBuf,
	})
	c.Assert(err, tc.ErrorMatches, `expected a multipart response, got "text/plain"`)
}

func (cs *clientSuite) TestPullFailsWithErrorResponse(c *tc.C) {
	cs.header = http.Header{}
	cs.header.Set("Content-Type", "application/json")
	cs.rsp = `{"type":"error","status-code":401,"status":"Unauthorized","result":{"message":"access denied","kind":"login-required"}}`

	// Check response
	var targetBuf bytes.Buffer
	err := cs.cli.Pull(&client.PullOptions{
		Path:   "/foo/bar.dat",
		Target: &targetBuf,
	})
	c.Assert(err, tc.ErrorMatches, "access denied")
}

func (cs *clientSuite) TestPullFailsWithMultipartErrorResponse(c *tc.C) {
	// Create multipart response with "response" field containing error
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	// Write the error response as first part
	responsePart, err := mw.CreateFormField("response")
	c.Assert(err, tc.ErrorIsNil)
	errorResp := `{"type":"sync","status-code":404,"status":"tc.Not Found","result":[{"error":{"message":"file not found","kind":"not-found"}}]}`
	_, err = responsePart.Write([]byte(errorResp))
	c.Assert(err, tc.ErrorIsNil)

	err = mw.Close()
	c.Assert(err, tc.ErrorIsNil)

	// Set up mock response
	cs.header = http.Header{}
	cs.header.Set("Content-Type", mw.FormDataContentType())
	cs.rsp = buf.String()

	// Check that Pull returns the error from the response part
	var targetBuf bytes.Buffer
	err = cs.cli.Pull(&client.PullOptions{
		Path:   "/foo/bar.dat",
		Target: &targetBuf,
	})
	c.Assert(err, tc.ErrorMatches, "file not found")
}

func (cs *clientSuite) TestPullFailsWithInvalidMultipartResponse(c *tc.C) {
	cs.header = http.Header{}
	cs.header.Set("Content-Type", "multipart/form-data")
	cs.rsp = "Definitely not a multipart payload"

	// Check response
	var targetBuf bytes.Buffer
	err := cs.cli.Pull(&client.PullOptions{
		Path:   "/foo/bar.dat",
		Target: &targetBuf,
	})
	c.Assert(err, tc.ErrorMatches, "cannot decode multipart payload: .*")
}

type errWriter struct{}

func (dw *errWriter) Write(p []byte) (n int, err error) {
	return 0, errors.New("I always fail!")
}

func (cs *clientSuite) TestPullFailsOnWrite(c *tc.C) {
	// Craft multipart response
	var srcBuf bytes.Buffer
	mw := multipart.NewWriter(&srcBuf)

	cs.header = http.Header{}
	cs.header.Set("Content-Type", mw.FormDataContentType())
	cs.status = http.StatusOK

	fw, err := mw.CreateFormFile("files", "/foo/bar.dat")
	c.Assert(err, tc.ErrorIsNil)
	fw.Write([]byte("Hello, world!"))

	mh := textproto.MIMEHeader{}
	mh.Set("Content-Type", "application/json")
	mh.Set("Content-Disposition", `form-data; name="response"`)

	part, err := mw.CreatePart(mh)
	c.Assert(err, tc.ErrorIsNil)
	fmt.Fprintf(part, `{
		"type": "sync",
		"status-code": 200,
		"status": "OK",
		"result": [{"path": "/foo/bar.dat"}]
	}`)

	mw.Close()
	cs.rsp = srcBuf.String()

	// Check response
	var dest errWriter
	err = cs.cli.Pull(&client.PullOptions{
		Path:   "/foo/bar.dat",
		Target: &dest,
	})
	c.Assert(err, tc.ErrorMatches, "cannot write to target: I always fail!")
}

func (cs *clientSuite) TestPullFailsWithInvalidJSON(c *tc.C) {
	// Craft multipart response
	var srcBuf bytes.Buffer
	mw := multipart.NewWriter(&srcBuf)

	cs.header = http.Header{}
	cs.header.Set("Content-Type", mw.FormDataContentType())
	cs.status = http.StatusOK

	// Encode file part
	filesHeader := textproto.MIMEHeader{}
	filesHeader.Set("Content-Type", "application/octet-stream")
	filesHeader.Set("Content-Disposition", `form-data; name="files"`)

	_, err := mw.CreatePart(filesHeader)
	c.Assert(err, tc.ErrorIsNil)

	// Encode response part
	responseHeader := textproto.MIMEHeader{}
	responseHeader.Set("Content-Type", "application/json")
	responseHeader.Set("Content-Disposition", `form-data; name="response"`)

	responsePart, err := mw.CreatePart(responseHeader)
	c.Assert(err, tc.ErrorIsNil)
	fmt.Fprintf(responsePart, `NJSON stands for tc.Not-JSON`)

	mw.Close()
	cs.rsp = srcBuf.String()

	// Check response
	var targetBuf bytes.Buffer
	err = cs.cli.Pull(&client.PullOptions{
		Path:   "/foo/bar.dat",
		Target: &targetBuf,
	})
	c.Assert(err, tc.ErrorMatches, "cannot decode .*: .*")
}

func (cs *clientSuite) TestPullFailsWithMetadataError(c *tc.C) {
	// Craft multipart response
	var srcBuf bytes.Buffer
	mw := multipart.NewWriter(&srcBuf)

	cs.header = http.Header{}
	cs.header.Set("Content-Type", mw.FormDataContentType())
	cs.status = http.StatusOK

	// Encode file part
	filesHeader := textproto.MIMEHeader{}
	filesHeader.Set("Content-Type", "application/octet-stream")
	filesHeader.Set("Content-Disposition", `form-data; name="files"`)

	_, err := mw.CreatePart(filesHeader)
	c.Assert(err, tc.ErrorIsNil)

	// Encode response part
	responseHeader := textproto.MIMEHeader{}
	responseHeader.Set("Content-Type", "application/json")
	responseHeader.Set("Content-Disposition", `form-data; name="response"`)

	responsePart, err := mw.CreatePart(responseHeader)
	c.Assert(err, tc.ErrorIsNil)
	fmt.Fprintf(responsePart, `{"type": "error", "result": {"message": "could not foo"}}`)

	mw.Close()
	cs.rsp = srcBuf.String()

	// Check response
	err = cs.cli.Pull(&client.PullOptions{
		Path: "/foo/bar.dat",
	})
	c.Assert(err, tc.ErrorMatches, "could not foo")
}

func (cs *clientSuite) TestPullFailsWithNonSyncResponse(c *tc.C) {
	// Craft multipart response
	var srcBuf bytes.Buffer
	mw := multipart.NewWriter(&srcBuf)

	cs.header = http.Header{}
	cs.header.Set("Content-Type", mw.FormDataContentType())
	cs.status = http.StatusOK

	// Encode file part
	filesHeader := textproto.MIMEHeader{}
	filesHeader.Set("Content-Type", "application/octet-stream")
	filesHeader.Set("Content-Disposition", `form-data; name="files"`)

	_, err := mw.CreatePart(filesHeader)
	c.Assert(err, tc.ErrorIsNil)

	// Encode response part
	responseHeader := textproto.MIMEHeader{}
	responseHeader.Set("Content-Type", "application/json")
	responseHeader.Set("Content-Disposition", `form-data; name="response"`)

	responsePart, err := mw.CreatePart(responseHeader)
	c.Assert(err, tc.ErrorIsNil)
	fmt.Fprintf(responsePart, `{"type": "sfdeljknesv"}`)

	mw.Close()
	cs.rsp = srcBuf.String()

	// Check response
	err = cs.cli.Pull(&client.PullOptions{
		Path: "/foo/bar.dat",
	})
	c.Assert(err, tc.ErrorMatches, `expected sync response, got "sfdeljknesv"`)
}

func (cs *clientSuite) TestPullFailsWithInvalidResult(c *tc.C) {
	// Craft multipart response
	var srcBuf bytes.Buffer
	mw := multipart.NewWriter(&srcBuf)

	cs.header = http.Header{}
	cs.header.Set("Content-Type", mw.FormDataContentType())
	cs.status = http.StatusOK

	// Encode file part
	filesHeader := textproto.MIMEHeader{}
	filesHeader.Set("Content-Type", "application/octet-stream")
	filesHeader.Set("Content-Disposition", `form-data; name="files"`)

	_, err := mw.CreatePart(filesHeader)
	c.Assert(err, tc.ErrorIsNil)

	// Encode response part
	responseHeader := textproto.MIMEHeader{}
	responseHeader.Set("Content-Type", "application/json")
	responseHeader.Set("Content-Disposition", `form-data; name="response"`)

	responsePart, err := mw.CreatePart(responseHeader)
	c.Assert(err, tc.ErrorIsNil)
	fmt.Fprintf(responsePart, `{"type": "sync", "result": "not what you expected"}`)

	mw.Close()
	cs.rsp = srcBuf.String()

	// Check response
	err = cs.cli.Pull(&client.PullOptions{
		Path: "/foo/bar.dat",
	})
	c.Assert(err, tc.ErrorMatches, "cannot unmarshal result: .*")
}

func (cs *clientSuite) TestPullFailsWithMultipleResults(c *tc.C) {
	// Craft multipart response
	var srcBuf bytes.Buffer
	mw := multipart.NewWriter(&srcBuf)

	cs.header = http.Header{}
	cs.header.Set("Content-Type", mw.FormDataContentType())
	cs.status = http.StatusOK

	// Encode file part
	filesHeader := textproto.MIMEHeader{}
	filesHeader.Set("Content-Type", "application/octet-stream")
	filesHeader.Set("Content-Disposition", `form-data; name="files"`)

	_, err := mw.CreatePart(filesHeader)
	c.Assert(err, tc.ErrorIsNil)

	// Encode response part
	responseHeader := textproto.MIMEHeader{}
	responseHeader.Set("Content-Type", "application/json")
	responseHeader.Set("Content-Disposition", `form-data; name="response"`)

	responsePart, err := mw.CreatePart(responseHeader)
	c.Assert(err, tc.ErrorIsNil)
	fmt.Fprintf(responsePart, `{"type": "sync", "result": [{"path": ""},{"path": ""}]}`)

	mw.Close()
	cs.rsp = srcBuf.String()

	// Check response
	err = cs.cli.Pull(&client.PullOptions{
		Path: "/foo/bar.dat",
	})
	c.Assert(err, tc.ErrorMatches, "expected exactly one result from API, got 2")
}

func (cs *clientSuite) TestPullFailsWithClientError(c *tc.C) {
	// Craft multipart response
	var srcBuf bytes.Buffer
	mw := multipart.NewWriter(&srcBuf)

	cs.header = http.Header{}
	cs.header.Set("Content-Type", mw.FormDataContentType())
	cs.status = http.StatusOK

	// Encode file part
	filesHeader := textproto.MIMEHeader{}
	filesHeader.Set("Content-Type", "application/octet-stream")
	filesHeader.Set("Content-Disposition", `form-data; name="files"`)

	_, err := mw.CreatePart(filesHeader)
	c.Assert(err, tc.ErrorIsNil)

	// Encode response part
	responseHeader := textproto.MIMEHeader{}
	responseHeader.Set("Content-Type", "application/json")
	responseHeader.Set("Content-Disposition", `form-data; name="response"`)

	responsePart, err := mw.CreatePart(responseHeader)
	c.Assert(err, tc.ErrorIsNil)
	fmt.Fprintf(responsePart, `{
		"type": "sync",
		"result": [{
			"path": "/foo/bar.dat",
			"error": {
				"message": "could not do something",
				"kind": "generic-file-error",
				"value": 1337
			}
		}]
	}`)

	mw.Close()
	cs.rsp = srcBuf.String()

	// Check response
	err = cs.cli.Pull(&client.PullOptions{
		Path: "/foo/bar.dat",
	})

	clientErr, ok := err.(*client.Error)
	c.Assert(ok, tc.Equals, true)
	c.Assert(clientErr.Message, tc.Equals, "could not do something")
	c.Assert(clientErr.Kind, tc.Equals, "generic-file-error")
}
