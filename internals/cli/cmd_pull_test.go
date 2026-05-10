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
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"

	"github.com/canonical/tc"

	"github.com/canonical/pebble/client"
	"github.com/canonical/pebble/internals/cli"
)

func (s *PebbleSuite) TestPull(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.URL.Path, tc.Equals, "/v1/files")
		c.Assert(r.Method, tc.Equals, "GET")
		c.Assert(r.URL.Query(), tc.DeepEquals, url.Values{
			"action": {"read"},
			"path":   {"/foo/bar.dat"},
		})
		c.Assert(r.Header.Get("Accept"), tc.Equals, "multipart/form-data")

		mw := multipart.NewWriter(w)
		header := w.Header()
		header.Set("Content-Type", mw.FormDataContentType())
		w.WriteHeader(http.StatusOK)

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
	})

	// Create temporary directory
	tempDir := c.MkDir()
	filePath := filepath.Join(tempDir, "file.dat")

	args := []string{"pull", "/foo/bar.dat", filePath}
	rest, err := cli.ParserForTest().ParseArgs(args)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(rest, tc.HasLen, 0)
	c.Check(s.Stdout(), tc.Equals, "")
	c.Check(s.Stderr(), tc.Equals, "")

	b, err := os.ReadFile(filePath)
	c.Assert(err, tc.ErrorIsNil)
	c.Check(b, tc.DeepEquals, []byte("Hello, world!"))
}

func (s *PebbleSuite) TestPullFailsExtraArgs(c *tc.C) {
	args := []string{"pull", "/foo/bar.dat", "extra", "args"}
	rest, err := cli.ParserForTest().ParseArgs(args)
	c.Assert(err, tc.Equals, cli.ErrExtraArgs)
	c.Assert(rest, tc.HasLen, 1)
	c.Check(s.Stdout(), tc.Equals, "")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestPullFailsAPI(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.URL.Path, tc.Equals, "/v1/files")
		c.Assert(r.Method, tc.Equals, "GET")
		c.Assert(r.URL.Query(), tc.DeepEquals, url.Values{
			"action": {"read"},
			"path":   {"/foo/bar.dat"},
		})
		c.Assert(r.Header.Get("Accept"), tc.Equals, "multipart/form-data")

		mw := multipart.NewWriter(w)
		header := w.Header()
		header.Set("Content-Type", mw.FormDataContentType())
		w.WriteHeader(http.StatusOK)

		fw, err := mw.CreateFormFile("files", "/foo/bar.dat")
		c.Assert(err, tc.ErrorIsNil)
		fw.Write([]byte("Hello, world!"))

		mh := textproto.MIMEHeader{}
		mh.Set("Content-Type", "application/json")
		mh.Set("Content-Disposition", `form-data; name="response"`)

		part, err := mw.CreatePart(mh)
		c.Assert(err, tc.ErrorIsNil)
		fmt.Fprintf(part, ` {
			"type": "sync",
			"result": [{
				"path": "/foo/bar.dat",
				"error": {
					"message": "could not foo",
					"kind": "permission-denied",
					"value": 42
				}
			}]
		}`)

		mw.Close()
	})

	// Create temporary directory
	tempDir := c.MkDir()
	filePath := filepath.Join(tempDir, "file.dat")

	args := []string{"pull", "/foo/bar.dat", filePath}
	rest, err := cli.ParserForTest().ParseArgs(args)

	clientErr, ok := err.(*client.Error)
	c.Assert(ok, tc.Equals, true)
	c.Assert(clientErr.Message, tc.Equals, "could not foo")
	c.Assert(clientErr.Kind, tc.Equals, "permission-denied")

	// File should have been discarded
	_, err = os.Stat(filePath)
	isErrNotExist := errors.Is(err, os.ErrNotExist)
	c.Assert(isErrNotExist, tc.Equals, true)

	c.Assert(rest, tc.HasLen, 1)
	c.Check(s.Stdout(), tc.Equals, "")
	c.Check(s.Stderr(), tc.Equals, "")
}

func (s *PebbleSuite) TestPullFailsCreateFile(c *tc.C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.URL.Path, tc.Equals, "/v1/files")
		c.Assert(r.Method, tc.Equals, "GET")
		c.Assert(r.URL.Query(), tc.DeepEquals, url.Values{
			"action": {"read"},
			"path":   {"/foo/bar.dat"},
		})
		c.Assert(r.Header.Get("Accept"), tc.Equals, "multipart/form-data")

		mw := multipart.NewWriter(w)
		header := w.Header()
		header.Set("Content-Type", mw.FormDataContentType())
		w.WriteHeader(http.StatusOK)

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
			"result": [{
				"path": "/foo/bar.dat",
				"error": {
					"message": "could not foo",
					"kind": "permission-denied",
					"value": 42
				}
			}]
		}`)

		mw.Close()
	})

	args := []string{"pull", "/foo/bar.dat", ""}
	rest, err := cli.ParserForTest().ParseArgs(args)
	isErrNotExist := errors.Is(err, os.ErrNotExist)
	c.Assert(isErrNotExist, tc.Equals, true)

	c.Assert(rest, tc.HasLen, 1)
	c.Check(s.Stdout(), tc.Equals, "")
	c.Check(s.Stderr(), tc.Equals, "")
}
