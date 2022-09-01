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
	"io/ioutil"
	"os"
	"path"

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

func (cs *clientSuite) TestPushFile(c *C) {
	cs.rsp = `{"type": "sync", "result": [{"path": "/file.dat"}]}`

	// Create temporary file
	tempDir := c.MkDir()
	filePath := path.Join(tempDir, "file.bin")
	contents := []byte("hello, world!")
	err := ioutil.WriteFile(filePath, contents, os.FileMode(0644))
	c.Assert(err, IsNil)

	err = cs.cli.PushFile(&client.PushFileOptions{
		LocalPath:  filePath,
		RemotePath: "/file.dat",
	})
	c.Assert(err, IsNil)

	c.Assert(cs.req.URL.Path, Equals, "/v1/files")
	c.Assert(cs.req.Method, Equals, "POST")
}
