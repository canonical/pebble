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

package wsutil_test

import (
	"bytes"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/testutil"
	"github.com/canonical/pebble/internals/wsutil"
)

func Test(t *testing.T) {
	testutil.PrintGoroutineLeaks(t, TestingT)
}

type wsutilSuite struct{}

var _ = Suite(&wsutilSuite{})

func (s *wsutilSuite) TestReaderToChannelStop(c *C) {
	stop := make(chan struct{})
	reader := bytes.NewReader(bytes.Repeat([]byte("x"), 128*1024))
	ch := wsutil.ReaderToChannel(reader, -1, stop)

	close(stop)
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			c.Fatal("ReaderToChannel did not stop")
		}
	}
}

func (s *wsutilSuite) TestReaderToChannel(c *C) {
	stop := make(chan struct{})
	want := []byte("hello")
	ch := wsutil.ReaderToChannel(bytes.NewReader(want), -1, stop)

	select {
	case got, ok := <-ch:
		c.Assert(ok, Equals, true)
		c.Assert(got, DeepEquals, want)
	case <-time.After(time.Second):
		c.Fatal("ReaderToChannel did not return data")
	}
	_, ok := <-ch
	c.Assert(ok, Equals, false)
}

func (s *wsutilSuite) TestReaderToChannelStopWithAbandonedConsumer(c *C) {
	stop := make(chan struct{})
	reader := bytes.NewReader(bytes.Repeat([]byte("x"), 128*1024))
	ch := wsutil.ReaderToChannel(reader, -1, stop)

	// Do not consume ch: this models a websocket consumer that stopped after
	// its write failed.
	close(stop)
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			c.Fatal("ReaderToChannel producer remained blocked")
		}
	}
}
