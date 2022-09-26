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

package main_test

import (
	"errors"
	"fmt"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
	pebble "github.com/canonical/pebble/cmd/pebble"
)

func (s *PebbleSuite) TestPull(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.URL.Path, Equals, "/v1/files")
		c.Assert(r.Method, Equals, "GET")
		c.Assert(r.URL.Query(), DeepEquals, url.Values{
			"action": {"read"},
			"path":   {"/foo/bar.dat"},
		})
		c.Assert(r.Header.Get("Accept"), Equals, "multipart/form-data")

		mw := multipart.NewWriter(w)
		header := w.Header()
		header.Set("Content-Type", mw.FormDataContentType())
		w.WriteHeader(http.StatusOK)

		fw, err := mw.CreateFormFile("files", "/foo/bar.dat")
		c.Assert(err, IsNil)
		fw.Write([]byte("Hello, world!"))

		mh := textproto.MIMEHeader{}
		mh.Set("Content-Type", "application/json")
		mh.Set("Content-Disposition", `form-data; name="response"`)

		part, err := mw.CreatePart(mh)
		c.Assert(err, IsNil)
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
	rest, err := pebble.Parser(pebble.Client()).ParseArgs(args)
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")

	f, err := os.Open(filePath)
	c.Assert(err, IsNil)
	b, err := ioutil.ReadAll(f)
	c.Assert(err, IsNil)
	c.Check(b, DeepEquals, []byte("Hello, world!"))
}

func (s *PebbleSuite) TestPullFailsExtraArgs(c *C) {
	args := []string{"pull", "/foo/bar.dat", "extra", "args"}
	rest, err := pebble.Parser(pebble.Client()).ParseArgs(args)
	c.Assert(err, Equals, pebble.ErrExtraArgs)
	c.Assert(rest, HasLen, 1)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestPullFailsAPI(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.URL.Path, Equals, "/v1/files")
		c.Assert(r.Method, Equals, "GET")
		c.Assert(r.URL.Query(), DeepEquals, url.Values{
			"action": {"read"},
			"path":   {"/foo/bar.dat"},
		})
		c.Assert(r.Header.Get("Accept"), Equals, "multipart/form-data")

		mw := multipart.NewWriter(w)
		header := w.Header()
		header.Set("Content-Type", mw.FormDataContentType())
		w.WriteHeader(http.StatusOK)

		fw, err := mw.CreateFormFile("files", "/foo/bar.dat")
		c.Assert(err, IsNil)
		fw.Write([]byte("Hello, world!"))

		mh := textproto.MIMEHeader{}
		mh.Set("Content-Type", "application/json")
		mh.Set("Content-Disposition", `form-data; name="response"`)

		part, err := mw.CreatePart(mh)
		c.Assert(err, IsNil)
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
	rest, err := pebble.Parser(pebble.Client()).ParseArgs(args)

	clientErr, ok := err.(*client.Error)
	c.Assert(ok, Equals, true)
	c.Assert(clientErr.Message, Equals, "could not foo")
	c.Assert(clientErr.Kind, Equals, "permission-denied")

	// File should have been discarded
	_, err = os.Stat(filePath)
	isErrNotExist := errors.Is(err, os.ErrNotExist)
	c.Assert(isErrNotExist, Equals, true)

	c.Assert(rest, HasLen, 1)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}

func (s *PebbleSuite) TestPullFailsCreateFile(c *C) {
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(r.URL.Path, Equals, "/v1/files")
		c.Assert(r.Method, Equals, "GET")
		c.Assert(r.URL.Query(), DeepEquals, url.Values{
			"action": {"read"},
			"path":   {"/foo/bar.dat"},
		})
		c.Assert(r.Header.Get("Accept"), Equals, "multipart/form-data")

		mw := multipart.NewWriter(w)
		header := w.Header()
		header.Set("Content-Type", mw.FormDataContentType())
		w.WriteHeader(http.StatusOK)

		fw, err := mw.CreateFormFile("files", "/foo/bar.dat")
		c.Assert(err, IsNil)
		fw.Write([]byte("Hello, world!"))

		mh := textproto.MIMEHeader{}
		mh.Set("Content-Type", "application/json")
		mh.Set("Content-Disposition", `form-data; name="response"`)

		part, err := mw.CreatePart(mh)
		c.Assert(err, IsNil)
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
	rest, err := pebble.Parser(pebble.Client()).ParseArgs(args)
	isErrNotExist := errors.Is(err, os.ErrNotExist)
	c.Assert(isErrNotExist, Equals, true)

	c.Assert(rest, HasLen, 1)
	c.Check(s.Stdout(), Equals, "")
	c.Check(s.Stderr(), Equals, "")
}
