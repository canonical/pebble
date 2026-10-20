// Copyright (c) 2026 Canonical Ltd
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

//go:build linux

package wsutil_test

import (
	"bytes"
	"os"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/ptyutil"
	"github.com/canonical/pebble/internals/wsutil"
)

func (s *wsutilSuite) TestExecReaderToChannelHUPFlush(c *C) {
	ptx, pty, err := ptyutil.OpenPty(int64(os.Getuid()), int64(os.Getgid()))
	c.Assert(err, IsNil)
	defer ptx.Close()

	exited := make(chan struct{})
	ch := wsutil.ExecReaderToChannel(ptx, 128*1024, exited, int(ptx.Fd()))

	// Write data to the slave end of the PTY.
	expected := []byte("hello from pty")
	_, err = pty.Write(expected)
	c.Assert(err, IsNil)

	// Close the slave end, triggering POLLHUP / POLLRDHUP on ptx.
	err = pty.Close()
	c.Assert(err, IsNil)

	// Signal child process exit.
	close(exited)

	// Collect output received from channel until closed.
	var received bytes.Buffer
	for {
		select {
		case buf, ok := <-ch:
			if !ok {
				c.Assert(received.Bytes(), DeepEquals, expected)
				return
			}
			received.Write(buf)
		case <-time.After(5 * time.Second):
			c.Fatalf("timed out waiting for output on channel")
		}
	}
}

func (s *wsutilSuite) TestExecReaderToChannelExitSlowConsumer(c *C) {
	// Tests that when consumer is not reading from ch and exited is closed,
	// channel closure coordination does not panic with send on closed channel.
	for i := 0; i < 20; i++ {
		ptx, pty, err := ptyutil.OpenPty(int64(os.Getuid()), int64(os.Getgid()))
		c.Assert(err, IsNil)

		exited := make(chan struct{})
		ch := wsutil.ExecReaderToChannel(ptx, 128*1024, exited, int(ptx.Fd()))

		// Write data so reader loop will have bytes to send.
		_, err = pty.Write([]byte("some data to fill reader buffer"))
		c.Assert(err, IsNil)

		// Close exited concurrently while consumer does not read immediately.
		close(exited)
		_ = pty.Close()

		// Short sleep to allow exit watcher to run and trigger stop.
		time.Sleep(10 * time.Millisecond)

		// Drain channel until closed without panic.
		done := make(chan struct{})
		go func() {
			for range ch {
			}
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			c.Fatalf("timed out draining channel on iteration %d", i)
		}

		_ = ptx.Close()
	}
}
