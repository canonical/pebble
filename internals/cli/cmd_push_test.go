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

	"github.com/canonical/tc"

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

func (s *PebbleSuite) TestPush(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.URL.Path, tc.Equals, "/v1/files")
		c.Assert(r.Method, tc.Equals, "POST")

		mr, err := r.MultipartReader()
		c.Assert(err, tc.ErrorIsNil)

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
				Path:        "/tmp/file.bin",
				Permissions: "755",
			}},
		})

		// Check file part
		file, err := mr.NextPart()
		c.Assert(err, tc.ErrorIsNil)
		c.Assert(file.Header.Get("Content-Type"), tc.Equals, "application/octet-stream")
		c.Assert(file.FormName(), tc.Equals, "files")
		c.Assert(path.Base(file.FileName()), tc.Equals, "file.bin")

		buf.Reset()
		_, err = buf.ReadFrom(file)
		c.Assert(err, tc.ErrorIsNil)
		c.Assert(buf.String(), tc.Equals, "Hello, world!")

		// Check end of multipart request
		_, err = mr.NextPart()
		c.Assert(err, tc.Equals, io.EOF)

		fmt.Fprintln(w, `{"type": "sync", "result": [{"path": "/tmp/file.bin"}]}`)
	})

	// Create temporary file
	tempDir := c.MkDir()
	filePath := filepath.Join(tempDir, "file.dat")
	err := os.WriteFile(filePath, []byte("Hello, world!"), 0755)
	c.Assert(err, tc.ErrorIsNil)

	args := []string{"push", filePath, "/tmp/file.bin"}
	rest, err := cli.ParserForTest().ParseArgs(args)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, "")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestPushExtraArgs(c *tc.C) {
	args := []string{"push", "file.dat", "/tmp/file.bin", "extra", "args"}
	_, err := cli.ParserForTest().ParseArgs(args)
	c.Assert(err, tc.Equals, cli.ErrExtraArgs)
	c.Check(s.Stdout(), tc.Equals, "")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestPushFailsToOpen(c *tc.C) {
	args := []string{"push", "/non/existing/path", "/tmp/file.bin"}
	_, err := cli.ParserForTest().ParseArgs(args)
	c.Assert(err, tc.Not(tc.IsNil))
	e, ok := err.(*os.PathError)
	c.Assert(ok, tc.Equals, true)
	c.Assert(e.Path, tc.Equals, "/non/existing/path")
	c.Assert(e.Err, tc.Equals, syscall.ENOENT)
	c.Check(s.Stdout(), tc.Equals, "")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestPushAPIFails(c *tc.C) {
	s.RedirectClientToTestServer(c, func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.URL.Path, tc.Equals, "/v1/files")
		c.Assert(r.Method, tc.Equals, "POST")

		mr, err := r.MultipartReader()
		c.Assert(err, tc.ErrorIsNil)

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
				Path:        "/tmp/file.bin",
				Permissions: "600",
			}},
		})

		// Check file part
		file, err := mr.NextPart()
		c.Assert(err, tc.ErrorIsNil)
		c.Assert(file.Header.Get("Content-Type"), tc.Equals, "application/octet-stream")
		c.Assert(file.FormName(), tc.Equals, "files")
		c.Assert(path.Base(file.FileName()), tc.Equals, "file.bin")

		buf.Reset()
		_, err = buf.ReadFrom(file)
		c.Assert(err, tc.ErrorIsNil)
		c.Assert(buf.String(), tc.Equals, "Hello, world!")

		// Check end of multipart request
		_, err = mr.NextPart()
		c.Assert(err, tc.Equals, io.EOF)

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
	c.Assert(err, tc.ErrorIsNil)

	args := []string{"push", "-m", "600", filePath, "/tmp/file.bin"}
	rest, err := cli.ParserForTest().ParseArgs(args)

	clientErr, ok := err.(*client.Error)
	c.Assert(ok, tc.Equals, true)
	c.Assert(clientErr.Message, tc.Equals, "could not bar")
	c.Assert(clientErr.Kind, tc.Equals, "permission-denied")

	c.Assert(rest, tc.HasLen, 1)
	c.Check(s.Stdout(), tc.Equals, "")
	c.Check(s.Stderr(), tc.Equals, "")
}
