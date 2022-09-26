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
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
)

func (cs *clientSuite) TestPull(c *C) {
	// Craft multipart response
	var srcBuf bytes.Buffer
	mw := multipart.NewWriter(&srcBuf)

	cs.header = http.Header{}
	cs.header.Set("Content-Type", mw.FormDataContentType())
	cs.status = http.StatusOK

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
	cs.rsp = srcBuf.String()

	// Check response
	var targetBuf bytes.Buffer
	err = cs.cli.Pull(&client.PullOptions{
		Path:   "/foo/bar.dat",
		Target: &targetBuf,
	})
	c.Assert(err, IsNil)
	c.Check(targetBuf.String(), Equals, "Hello, world!")
}

func (cs *clientSuite) TestPullFailsWithNoContentType(c *C) {
	// Check response
	var targetBuf bytes.Buffer
	err := cs.cli.Pull(&client.PullOptions{
		Path:   "/foo/bar.dat",
		Target: &targetBuf,
	})
	c.Assert(err, ErrorMatches, "invalid Content-Type: .*")
}

func (cs *clientSuite) TestPullFailsWithNonMultipartResponse(c *C) {
	cs.header = http.Header{}
	cs.header.Set("Content-Type", "text/plain; charset=utf-8")
	cs.rsp = "Hello, world!"

	// Check response
	var targetBuf bytes.Buffer
	err := cs.cli.Pull(&client.PullOptions{
		Path:   "/foo/bar.dat",
		Target: &targetBuf,
	})
	c.Assert(err, ErrorMatches, "expected a multipart response but didn't get one")
}

func (cs *clientSuite) TestPullFailsWithInvalidMultipartResponse(c *C) {
	cs.header = http.Header{}
	cs.header.Set("Content-Type", "multipart/form-data")
	cs.rsp = "Definitely not a multipart payload"

	// Check response
	var targetBuf bytes.Buffer
	err := cs.cli.Pull(&client.PullOptions{
		Path:   "/foo/bar.dat",
		Target: &targetBuf,
	})
	c.Assert(err, ErrorMatches, "cannot decode multipart payload: .*")
}

type defectiveWriter struct{}

func (dw *defectiveWriter) Write(p []byte) (n int, err error) {
	return 0, errors.New("I always fail!")
}

func (cs *clientSuite) TestPullFailsOnWrite(c *C) {
	// Craft multipart response
	var srcBuf bytes.Buffer
	mw := multipart.NewWriter(&srcBuf)

	cs.header = http.Header{}
	cs.header.Set("Content-Type", mw.FormDataContentType())
	cs.status = http.StatusOK

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
	cs.rsp = srcBuf.String()

	// Check response
	var dest defectiveWriter
	err = cs.cli.Pull(&client.PullOptions{
		Path:   "/foo/bar.dat",
		Target: &dest,
	})
	c.Assert(err, ErrorMatches, "cannot write: I always fail!")
}

func (cs *clientSuite) TestPullFailsWithInvalidJSON(c *C) {
	// Craft multipart response
	var srcBuf bytes.Buffer
	mw := multipart.NewWriter(&srcBuf)

	cs.header = http.Header{}
	cs.header.Set("Content-Type", mw.FormDataContentType())
	cs.status = http.StatusOK

	mh := textproto.MIMEHeader{}
	mh.Set("Content-Type", "application/json")
	mh.Set("Content-Disposition", `form-data; name="response"`)

	part, err := mw.CreatePart(mh)
	c.Assert(err, IsNil)
	fmt.Fprintf(part, `NJSON stands for Not-JSON`)

	mw.Close()
	cs.rsp = srcBuf.String()

	// Check response
	var targetBuf bytes.Buffer
	err = cs.cli.Pull(&client.PullOptions{
		Path:   "/foo/bar.dat",
		Target: &targetBuf,
	})
	c.Assert(err, ErrorMatches, "cannot decode response: .*")
}

func (cs *clientSuite) TestPullFailsWithMetadataError(c *C) {
	// Craft multipart response
	var srcBuf bytes.Buffer
	mw := multipart.NewWriter(&srcBuf)

	cs.header = http.Header{}
	cs.header.Set("Content-Type", mw.FormDataContentType())
	cs.status = http.StatusOK

	mh := textproto.MIMEHeader{}
	mh.Set("Content-Type", "application/json")
	mh.Set("Content-Disposition", `form-data; name="response"`)

	part, err := mw.CreatePart(mh)
	c.Assert(err, IsNil)
	fmt.Fprintf(part, `{"type": "error", "result": {"message": "could not foo"}}`)

	mw.Close()
	cs.rsp = srcBuf.String()

	// Check response
	err = cs.cli.Pull(&client.PullOptions{
		Path: "/foo/bar.dat",
	})
	c.Assert(err, ErrorMatches, "could not foo")
}

func (cs *clientSuite) TestPullFailsWithNonSyncResponse(c *C) {
	// Craft multipart response
	var srcBuf bytes.Buffer
	mw := multipart.NewWriter(&srcBuf)

	cs.header = http.Header{}
	cs.header.Set("Content-Type", mw.FormDataContentType())
	cs.status = http.StatusOK

	mh := textproto.MIMEHeader{}
	mh.Set("Content-Type", "application/json")
	mh.Set("Content-Disposition", `form-data; name="response"`)

	part, err := mw.CreatePart(mh)
	c.Assert(err, IsNil)
	fmt.Fprintf(part, `{"type": "sfdeljknesv"}`)

	mw.Close()
	cs.rsp = srcBuf.String()

	// Check response
	err = cs.cli.Pull(&client.PullOptions{
		Path: "/foo/bar.dat",
	})
	c.Assert(err, ErrorMatches, `expected sync response, got "sfdeljknesv"`)
}

func (cs *clientSuite) TestPullFailsWithInvalidResult(c *C) {
	// Craft multipart response
	var srcBuf bytes.Buffer
	mw := multipart.NewWriter(&srcBuf)

	cs.header = http.Header{}
	cs.header.Set("Content-Type", mw.FormDataContentType())
	cs.status = http.StatusOK

	mh := textproto.MIMEHeader{}
	mh.Set("Content-Type", "application/json")
	mh.Set("Content-Disposition", `form-data; name="response"`)

	part, err := mw.CreatePart(mh)
	c.Assert(err, IsNil)
	fmt.Fprintf(part, `{"type": "sync", "result": "not what you expected"}`)

	mw.Close()
	cs.rsp = srcBuf.String()

	// Check response
	err = cs.cli.Pull(&client.PullOptions{
		Path: "/foo/bar.dat",
	})
	c.Assert(err, ErrorMatches, "cannot unmarshal result: .*")
}

func (cs *clientSuite) TestPullFailsWithMultipleResults(c *C) {
	// Craft multipart response
	var srcBuf bytes.Buffer
	mw := multipart.NewWriter(&srcBuf)

	cs.header = http.Header{}
	cs.header.Set("Content-Type", mw.FormDataContentType())
	cs.status = http.StatusOK

	mh := textproto.MIMEHeader{}
	mh.Set("Content-Type", "application/json")
	mh.Set("Content-Disposition", `form-data; name="response"`)

	part, err := mw.CreatePart(mh)
	c.Assert(err, IsNil)
	fmt.Fprintf(part, `{"type": "sync", "result": [{"path": ""},{"path": ""}]}`)

	mw.Close()
	cs.rsp = srcBuf.String()

	// Check response
	err = cs.cli.Pull(&client.PullOptions{
		Path: "/foo/bar.dat",
	})
	c.Assert(err, ErrorMatches, "expected exactly one result from API, got 2")
}

func (cs *clientSuite) TestPullFailsWithClientError(c *C) {
	// Craft multipart response
	var srcBuf bytes.Buffer
	mw := multipart.NewWriter(&srcBuf)

	cs.header = http.Header{}
	cs.header.Set("Content-Type", mw.FormDataContentType())
	cs.status = http.StatusOK

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
	c.Assert(ok, Equals, true)
	c.Assert(clientErr.Message, Equals, "could not do something")
	c.Assert(clientErr.Kind, Equals, "generic-file-error")
}
