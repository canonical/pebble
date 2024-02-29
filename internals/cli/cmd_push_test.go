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

package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/internals/cli"
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

func (s *PebbleSuite) TestPush(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.URL.Path, Equals, "/v1/files")
		c.Assert(r.Method, Equals, "POST")

		mr, err := r.MultipartReader()
		c.Assert(err, IsNil)

		// Check metadata part
		metadata, err := mr.NextPart()
		c.Assert(err, IsNil)
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
				Path:        "/tmp/file.bin",
				Permissions: "755",
			}},
		})

		// Check file part
		file, err := mr.NextPart()
		c.Assert(err, IsNil)
		c.Assert(file.Header.Get("Content-Type"), Equals, "application/octet-stream")
		c.Assert(file.FormName(), Equals, "files")
		c.Assert(path.Base(file.FileName()), Equals, "file.bin")

		buf.Reset()
		_, err = buf.ReadFrom(file)
		c.Assert(err, IsNil)
		c.Assert(buf.String(), Equals, "Hello, world!")

		// Check end of multipart request
		_, err = mr.NextPart()
		c.Assert(err, Equals, io.EOF)

		fmt.Fprintln(w, `{"type": "sync", "result": [{"path": "/tmp/file.bin"}]}`)
	})

	// Create temporary file
	tempDir := c.MkDir()
	filePath := filepath.Join(tempDir, "file.dat")
	err := os.WriteFile(filePath, []byte("Hello, world!"), 0755)
	c.Assert(err, IsNil)

	args := []string{"push", filePath, "/tmp/file.bin"}
	rest, err := cli.Parser(cli.Client()).ParseArgs(args)
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestPushExtraArgs(c *C) {
	args := []string{"push", "file.dat", "/tmp/file.bin", "extra", "args"}
	_, err := cli.Parser(cli.Client()).ParseArgs(args)
	c.Assert(err, Equals, cli.ErrExtraArgs)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestPushFailsToOpen(c *C) {
	args := []string{"push", "/non/existing/path", "/tmp/file.bin"}
	_, err := cli.Parser(cli.Client()).ParseArgs(args)
	c.Assert(err, Not(IsNil))
	e, ok := err.(*os.PathError)
	c.Assert(ok, Equals, true)
	c.Assert(e.Path, Equals, "/non/existing/path")
	c.Assert(e.Err, Equals, syscall.ENOENT)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestPushAPIFails(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.URL.Path, Equals, "/v1/files")
		c.Assert(r.Method, Equals, "POST")

		mr, err := r.MultipartReader()
		c.Assert(err, IsNil)

		// Check metadata part
		metadata, err := mr.NextPart()
		c.Assert(err, IsNil)
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
				Path:        "/tmp/file.bin",
				Permissions: "600",
			}},
		})

		// Check file part
		file, err := mr.NextPart()
		c.Assert(err, IsNil)
		c.Assert(file.Header.Get("Content-Type"), Equals, "application/octet-stream")
		c.Assert(file.FormName(), Equals, "files")
		c.Assert(path.Base(file.FileName()), Equals, "file.bin")

		buf.Reset()
		_, err = buf.ReadFrom(file)
		c.Assert(err, IsNil)
		c.Assert(buf.String(), Equals, "Hello, world!")

		// Check end of multipart request
		_, err = mr.NextPart()
		c.Assert(err, Equals, io.EOF)

		fmt.Fprintln(w, `{
			"type": "sync",
			"result": [{
				"path": "/tmp/file.bin",
				"error": {
					"message": "could not bar",
					"kind": "permission-denied",
					"value": 42
				}
			}]
		}`)
	})

	// Create temporary file
	tempDir := c.MkDir()
	filePath := filepath.Join(tempDir, "file.dat")
	err := os.WriteFile(filePath, []byte("Hello, world!"), 0755)
	c.Assert(err, IsNil)

	args := []string{"push", "-m", "600", filePath, "/tmp/file.bin"}
	rest, err := cli.Parser(cli.Client()).ParseArgs(args)

	clientErr, ok := err.(*client.Error)
	c.Assert(ok, Equals, true)
	c.Assert(clientErr.Message, Equals, "could not bar")
	c.Assert(clientErr.Kind, Equals, "permission-denied")

	c.Assert(rest, HasLen, 1)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}
