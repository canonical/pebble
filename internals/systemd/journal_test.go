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
	"log/syslog"
	"net"
	"path"
	"testing"

	"github.com/canonical/tc"

	. "github.com/canonical/pebble/internals/systemd"
)

type journalTestSuite struct{}

func TestJournalTestSuite(t *testing.T) {
	tc.Run(t, &journalTestSuite{})
}

func (j *journalTestSuite) TestStreamFileErrorNoPath(c *tc.C) {
	restore := FakeJournalStdoutPath(path.Join(c.MkDir(), "fake-journal"))
	defer restore()

	jout, err := NewJournalStreamFile("foobar", syslog.LOG_INFO, false)
	c.Assert(err, tc.ErrorMatches, ".*no such file or directory")
	c.Assert(jout, tc.IsNil)
}

func (j *journalTestSuite) TestStreamFileHeader(c *tc.C) {
	fakePath := path.Join(c.MkDir(), "fake-journal")
	restore := FakeJournalStdoutPath(fakePath)
	defer restore()

	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: fakePath})
	c.Assert(err, tc.ErrorIsNil)
	defer listener.Close()

	doneCh := make(chan struct{}, 1)

	go func() {
		defer func() { close(doneCh) }()

		// see https://github.com/systemd/systemd/blob/97a33b126c845327a3a19d6e66f05684823868fb/src/journal/journal-send.c#L424
		conn, err := listener.AcceptUnix()
		c.Assert(err, tc.ErrorIsNil)
		defer conn.Close()

		expectedHdrLen := len("foobar") + 1 + 1 + 2 + 2 + 2 + 2 + 2
		hdrBuf := make([]byte, expectedHdrLen)
		hdrLen, err := conn.Read(hdrBuf)
		c.Assert(err, tc.ErrorIsNil)
		c.Assert(hdrLen, tc.Equals, expectedHdrLen)
		c.Check(hdrBuf, tc.DeepEquals, []byte("foobar\n\n6\n0\n0\n0\n0\n"))

		data := make([]byte, 4096)
		sz, err := conn.Read(data)
		c.Assert(err, tc.ErrorIsNil)
		c.Assert(sz > 0, tc.Equals, true)
		c.Check(data[0:sz], tc.DeepEquals, []byte("hello from unit tests"))

		doneCh <- struct{}{}
	}()

	jout, err := NewJournalStreamFile("foobar", syslog.LOG_INFO, false)
	c.Assert(err, tc.ErrorIsNil)
	c.Assert(jout, tc.NotNil)

	_, err = jout.WriteString("hello from unit tests")
	c.Assert(err, tc.ErrorIsNil)
	defer jout.Close()

	<-doneCh
}
