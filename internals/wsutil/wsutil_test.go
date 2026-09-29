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
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
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

// blockingConn blocks reads until it is closed.
type blockingConn struct {
	once   sync.Once
	closed chan struct{}
}

func newBlockingConn() *blockingConn {
	return &blockingConn{closed: make(chan struct{})}
}

func (b *blockingConn) Reader(ctx context.Context) (websocket.MessageType, io.Reader, error) {
	<-b.closed
	return 0, nil, net.ErrClosed
}

func (b *blockingConn) Write(ctx context.Context, typ websocket.MessageType, p []byte) error {
	select {
	case <-b.closed:
		return net.ErrClosed
	default:
		return nil
	}
}

func (b *blockingConn) CloseNow() error {
	b.once.Do(func() { close(b.closed) })
	return nil
}

func (s *wsutilSuite) TestWebsocketSendStreamCancel(c *C) {
	conn := newBlockingConn()
	r, w := io.Pipe()
	defer w.Close()
	ctx, cancel := context.WithCancel(context.Background())

	// The reader never produces any data, so only cancellation can stop
	// the stream.
	done := wsutil.WebsocketSendStream(ctx, conn, r, -1)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		c.Fatal("WebsocketSendStream did not stop after cancellation")
	}
	select {
	case <-conn.closed:
	default:
		c.Fatal("WebsocketSendStream did not close the websocket")
	}
}

func (s *wsutilSuite) TestWebsocketRecvStreamCancel(c *C) {
	conn := newBlockingConn()
	ctx, cancel := context.WithCancel(context.Background())

	done := wsutil.WebsocketRecvStream(ctx, io.Discard, conn)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		c.Fatal("WebsocketRecvStream did not stop after cancellation")
	}
	select {
	case <-conn.closed:
	default:
		c.Fatal("WebsocketRecvStream did not close the websocket")
	}
}
