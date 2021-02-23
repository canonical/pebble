// Copyright (c) 2014-2020 Canonical Ltd
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

package systemd_test

import (
	"net"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internal/systemd"
)

type sdNotifyTestSuite struct {
	env           map[string]string
	restoreGetenv func()
}

var _ = Suite(&sdNotifyTestSuite{})

func (sd *sdNotifyTestSuite) SetUpTest(c *C) {
	sd.env = map[string]string{}
	sd.restoreGetenv = systemd.FakeOsGetenv(func(k string) string {
		return sd.env[k]
	})
}

func (sd *sdNotifyTestSuite) TearDownTest(c *C) {
	sd.restoreGetenv()
}

func (sd *sdNotifyTestSuite) TestSocketAvailable(c *C) {
	socketPath := filepath.Join(c.MkDir(), "notify.socket")
	c.Assert(systemd.SocketAvailable(), Equals, false)
	sd.env["NOTIFY_SOCKET"] = socketPath
	c.Assert(systemd.SocketAvailable(), Equals, false)
	f, _ := os.Create(socketPath)
	f.Close()
	c.Assert(systemd.SocketAvailable(), Equals, true)
}

func (sd *sdNotifyTestSuite) TestSdNotifyMissingNotifyState(c *C) {
	c.Check(systemd.SdNotify(""), ErrorMatches, "cannot use empty notify state")
}

func (sd *sdNotifyTestSuite) TestSdNotifyWrongNotifySocket(c *C) {
	for _, t := range []struct {
		env    string
		errStr string
	}{
		{"", `\$NOTIFY_SOCKET not defined`},
		{"xxx", `cannot use \$NOTIFY_SOCKET value: "xxx"`},
	} {
		sd.env["NOTIFY_SOCKET"] = t.env
		c.Check(systemd.SdNotify("something"), ErrorMatches, t.errStr)
	}
}

func (sd *sdNotifyTestSuite) TestSdNotifyIntegration(c *C) {
	for _, sockPath := range []string{
		filepath.Join(c.MkDir(), "socket"),
		"@socket",
	} {
		sd.env["NOTIFY_SOCKET"] = sockPath

		conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{
			Name: sockPath,
			Net:  "unixgram",
		})
		c.Assert(err, IsNil)
		defer conn.Close()

		ch := make(chan string)
		go func() {
			var buf [128]byte
			n, err := conn.Read(buf[:])
			c.Assert(err, IsNil)
			ch <- string(buf[:n])
		}()

		err = systemd.SdNotify("something")
		c.Assert(err, IsNil)
		c.Check(<-ch, Equals, "something")
	}
}
