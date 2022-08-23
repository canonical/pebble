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
	"os"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/client"
)

func (cs *clientSuite) TestListFiles(c *C) {
	cs.rsp = `{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": [
    {
      "path": "/bin",
      "name": "bin",
      "type": "symlink",
      "permissions": "777",
      "last-modified": "2022-04-21T03:02:51Z",
      "user-id": 0,
      "user": "root",
      "group-id": 0,
      "group": "root"
    },
    {
      "path": "/boot",
      "name": "boot",
      "type": "directory",
      "permissions": "755",
      "last-modified": "2022-08-16T10:23:25Z",
      "user-id": 0,
      "user": "root",
      "group-id": 0,
      "group": "root"
    },
    {
      "path": "/proc",
      "name": "proc",
      "type": "directory",
      "permissions": "555",
      "last-modified": "2022-08-16T09:50:23Z",
      "user-id": 0,
      "user": "root",
      "group-id": 0,
      "group": "root"
    },
    {
      "path": "/root",
      "name": "root",
      "type": "directory",
      "permissions": "700",
      "last-modified": "2022-08-17T11:55:09Z",
      "user-id": 0,
      "user": "root",
      "group-id": 0,
      "group": "root"
    },
    {
      "path": "/swap.img",
      "name": "swap.img",
      "type": "file",
      "size": 4100980736,
      "permissions": "600",
      "last-modified": "2022-05-14T18:10:12Z",
      "user-id": 0,
      "user": "root",
      "group-id": 0,
      "group": "root"
    },
    {
      "path": "/tmp",
      "name": "tmp",
      "type": "directory",
      "permissions": "777",
      "last-modified": "2022-08-22T12:42:49Z",
      "user-id": 0,
      "user": "root",
      "group-id": 0,
      "group": "root"
    },
	{
      "path": "/pebble.sock",
      "name": "pebble.sock",
      "type": "socket",
      "permissions": "700",
      "last-modified": "2022-08-22T12:42:49Z",
      "user-id": 0,
      "user": "root",
      "group-id": 0,
      "group": "root"
	},
	{
      "path": "/dbus.sock",
      "name": "dbus.sock",
      "type": "named-pipe",
      "permissions": "700",
      "last-modified": "2022-08-22T12:42:49Z",
      "user-id": 0,
      "user": "root",
      "group-id": 0,
      "group": "root"
	},
	{
      "path": "/tty0",
      "name": "tty0",
      "type": "device",
      "permissions": "700",
      "last-modified": "2022-08-22T12:42:49Z",
      "user-id": 0,
      "user": "root",
      "group-id": 0,
      "group": "root"
	},
	{
      "path": "/irreg",
      "name": "irreg",
      "type": "sfdeljknesv",
      "permissions": "700",
      "last-modified": "2022-08-22T12:42:49Z",
      "user-id": 0,
      "user": "root",
      "group-id": 0,
      "group": "root"
	}
  ]
}`
	result, err := cs.cli.ListFiles(&client.ListFilesOptions{
		Path: "/",
	})
	c.Assert(err, IsNil)
	c.Assert(result, HasLen, 10)
}

func (cs *clientSuite) TestListDirectoryItself(c *C) {
	cs.rsp = `{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": [
    {
      "path": "/bin",
      "name": "bin",
      "type": "symlink",
      "permissions": "777",
      "last-modified": "2022-04-21T03:02:51Z",
      "user-id": 0,
      "user": "root",
      "group-id": 0,
      "group": "root"
    }
  ]
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
	c.Check(result[0].UserID(), Equals, 0)
	c.Check(result[0].GroupID(), Equals, 0)
	c.Check(result[0].User(), Equals, "root")
	c.Check(result[0].Group(), Equals, "root")
	c.Check(result[0].IsDir(), Equals, false)
	c.Check(result[0].Sys(), Equals, nil)
}

func (cs *clientSuite) TestListFilesFails(c *C) {
	cs.rsp = `{
  "type": "error",
  "result": {
    "message": "could not foo"
  }
}`
	_, err := cs.cli.ListFiles(&client.ListFilesOptions{
		Path:   "/",
		Itself: true,
	})
	c.Assert(err, ErrorMatches, "could not foo")
}

func (cs *clientSuite) TestCalculateFileModeFails(c *C) {
	cs.rsp = `{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": [
    {
      "path": "/irreg",
      "name": "irreg",
      "type": "sfdeljknesv",
      "permissions": "deadbeef",
      "last-modified": "2022-08-22T12:42:49Z",
      "user-id": 0,
      "user": "root",
      "group-id": 0,
      "group": "root"
	}
  ]
}`
	_, err := cs.cli.ListFiles(&client.ListFilesOptions{
		Path: "/irreg",
	})
	c.Assert(err, ErrorMatches, "*invalid syntax*")
}

func (cs *clientSuite) TestCalculateFileModeClearsModeBits(c *C) {
	cs.rsp = `{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": [
    {
      "path": "/irreg",
      "name": "irreg",
      "type": "file",
      "permissions": "1664",
      "last-modified": "2022-08-22T12:42:49Z",
      "user-id": 0,
      "user": "root",
      "group-id": 0,
      "group": "root"
	}
  ]
}`
	result, err := cs.cli.ListFiles(&client.ListFilesOptions{
		Path: "/irreg",
	})
	c.Assert(err, IsNil)
	c.Assert(result, HasLen, 1)
	c.Check(result[0].Mode(), Equals, os.FileMode(0o664))
}

func (cs *clientSuite) TestParseTimeFails(c *C) {
	cs.rsp = `{
  "type": "sync",
  "status-code": 200,
  "status": "OK",
  "result": [
    {
      "path": "/irreg",
      "name": "irreg",
      "type": "sfdeljknesv",
      "permissions": "777",
      "last-modified": "2022-08-32T12:42:49Z",
      "user-id": 0,
      "user": "root",
      "group-id": 0,
      "group": "root"
	}
  ]
}`
	_, err := cs.cli.ListFiles(&client.ListFilesOptions{
		Path: "/irreg",
	})
	c.Assert(err, ErrorMatches, "*day out of range*")
}
