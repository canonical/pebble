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

package daemon

import (
	"errors"
	"net"
	"path/filepath"
	sys "syscall"
	"testing"

	"github.com/canonical/tc"
)

type ucrednetSuite struct {
	ucred *sys.Ucred
	err   error
}

func TestUcrednetSuite(t *testing.T) {
	tc.Run(t, &ucrednetSuite{})
}

func (s *ucrednetSuite) getUcred(fd, level, opt int) (*sys.Ucred, error) {
	return s.ucred, s.err
}

func (s *ucrednetSuite) SetUpSuite(c *tc.C) {
	getUcred = s.getUcred
}

func (s *ucrednetSuite) TearDownTest(c *tc.C) {
	s.ucred = nil
	s.err = nil
}
func (s *ucrednetSuite) TearDownSuite(c *tc.C) {
	getUcred = sys.GetsockoptUcred
}

func (s *ucrednetSuite) TestAcceptConnRemoteAddrString(c *tc.C) {
	s.ucred = &sys.Ucred{Pid: 100, Uid: 42}
	d := c.MkDir()
	sock := filepath.Join(d, "sock")

	l, err := net.Listen("unix", sock)
	c.Assert(err, tc.IsNil)
	wl := &ucrednetListener{Listener: l}

	defer wl.Close()

	go func() {
		cli, err := net.Dial("unix", sock)
		c.Assert(err, tc.IsNil)
		cli.Close()
	}()

	conn, err := wl.Accept()
	c.Assert(err, tc.IsNil)
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	c.Check(remoteAddr, tc.Matches, "pid=100;uid=42;.*")
	u, err := ucrednetGet(remoteAddr)
	c.Check(u.Pid, tc.Equals, int32(100))
	c.Check(u.Uid, tc.Equals, uint32(42))
	c.Check(err, tc.IsNil)
}

func (s *ucrednetSuite) TestNonUnix(c *tc.C) {
	l, err := net.Listen("tcp", "localhost:0")
	c.Assert(err, tc.IsNil)

	wl := &ucrednetListener{Listener: l}
	defer wl.Close()

	addr := l.Addr().String()

	go func() {
		cli, err := net.Dial("tcp", addr)
		c.Assert(err, tc.IsNil)
		cli.Close()
	}()

	conn, err := wl.Accept()
	c.Assert(err, tc.IsNil)
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	c.Check(remoteAddr, tc.Matches, "pid=;uid=;.*")
	u, err := ucrednetGet(remoteAddr)
	c.Check(err, tc.Equals, errNoID)
	c.Check(u, tc.IsNil)
}

func (s *ucrednetSuite) TestAcceptErrors(c *tc.C) {
	s.ucred = &sys.Ucred{Pid: 100, Uid: 42}
	d := c.MkDir()
	sock := filepath.Join(d, "sock")

	l, err := net.Listen("unix", sock)
	c.Assert(err, tc.IsNil)
	c.Assert(l.Close(), tc.IsNil)

	wl := &ucrednetListener{Listener: l}

	_, err = wl.Accept()
	c.Assert(err, tc.NotNil)
}

func (s *ucrednetSuite) TestUcredErrors(c *tc.C) {
	s.err = errors.New("oopsie")
	d := c.MkDir()
	sock := filepath.Join(d, "sock")

	l, err := net.Listen("unix", sock)
	c.Assert(err, tc.IsNil)

	wl := &ucrednetListener{Listener: l}
	defer wl.Close()

	go func() {
		cli, err := net.Dial("unix", sock)
		c.Assert(err, tc.IsNil)
		cli.Close()
	}()

	_, err = wl.Accept()
	c.Assert(err, tc.Equals, s.err)
}

func (s *ucrednetSuite) TestIdempotentClose(c *tc.C) {
	s.ucred = &sys.Ucred{Pid: 100, Uid: 42}
	d := c.MkDir()
	sock := filepath.Join(d, "sock")

	l, err := net.Listen("unix", sock)
	c.Assert(err, tc.IsNil)
	wl := &ucrednetListener{Listener: l}

	c.Assert(wl.Close(), tc.IsNil)
	c.Assert(wl.Close(), tc.IsNil)
}

func (s *ucrednetSuite) TestGetNoUid(c *tc.C) {
	u, err := ucrednetGet("pid=100;uid=;socket=;")
	c.Check(err, tc.Equals, errNoID)
	c.Check(u, tc.IsNil)
}

func (s *ucrednetSuite) TestGetBadUid(c *tc.C) {
	u, err := ucrednetGet("pid=100;uid=4294967296;socket=;")
	c.Check(err, tc.Equals, errNoID)
	c.Check(u, tc.IsNil)
}

func (s *ucrednetSuite) TestGetNonUcrednet(c *tc.C) {
	u, err := ucrednetGet("hello")
	c.Check(err, tc.Equals, errNoID)
	c.Check(u, tc.IsNil)
}

func (s *ucrednetSuite) TestGetNothing(c *tc.C) {
	u, err := ucrednetGet("")
	c.Check(err, tc.Equals, errNoID)
	c.Check(u, tc.IsNil)
}

func (s *ucrednetSuite) TestGet(c *tc.C) {
	u, err := ucrednetGet("pid=100;uid=42;socket=/run/.pebble.socket;")
	c.Check(err, tc.IsNil)
	c.Check(u.Pid, tc.Equals, int32(100))
	c.Check(u.Uid, tc.Equals, uint32(42))
	c.Check(u.Socket, tc.Equals, "/run/.pebble.socket")
}

func (s *ucrednetSuite) TestGetSneak(c *tc.C) {
	u, err := ucrednetGet("pid=100;uid=42;socket=/run/.pebble.socket;pid=0;uid=0;socket=/tmp/my.socket")
	c.Check(err, tc.Equals, errNoID)
	c.Check(u, tc.IsNil)
}

func (s *ucrednetSuite) TestGetWithZeroPid(c *tc.C) {
	u, err := ucrednetGet("pid=0;uid=42;socket=/run/.pebble.socket;")
	c.Check(err, tc.IsNil)
	c.Check(u.Pid, tc.Equals, int32(0))
	c.Check(u.Uid, tc.Equals, uint32(42))
	c.Check(u.Socket, tc.Equals, "/run/.pebble.socket")
}
