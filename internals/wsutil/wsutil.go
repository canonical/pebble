// Copyright (c) 2021 Canonical Ltd
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

package wsutil

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"

	"github.com/coder/websocket"

	"github.com/canonical/pebble/internals/logger"
)

// MessageReader is an interface that wraps websocket message reading.
type MessageReader interface {
	Reader(ctx context.Context) (websocket.MessageType, io.Reader, error)
}

// MessageWriter is an interface that wraps websocket message writing.
type MessageWriter interface {
	Write(ctx context.Context, typ websocket.MessageType, p []byte) error
}

// MessageReadWriter is an interface that wraps websocket message reading and
// writing.
type MessageReadWriter interface {
	MessageReader
	MessageWriter
}

// MessageReadCloser is an interface that wraps websocket message reading and
// closing.
type MessageReadCloser interface {
	MessageReader
	CloseNow() error
}

// MessageWriteCloser is an interface that wraps websocket message writing and
// closing.
type MessageWriteCloser interface {
	MessageWriter
	CloseNow() error
}

// closeOnCancel closes conn if ctx is cancelled before stop returns. This
// unblocks the peer, which would otherwise wait for an "end" command.
func closeOnCancel(ctx context.Context, conn interface{ CloseNow() error }) (stop func()) {
	stopAfter := context.AfterFunc(ctx, func() {
		_ = conn.CloseNow()
	})
	return func() {
		stopAfter()
		// The AfterFunc runs asynchronously, so it may not have run (or
		// finished) yet. CloseNow is idempotent, and waits for any concurrent
		// close to finish.
		if ctx.Err() != nil {
			_ = conn.CloseNow()
		}
	}
}

var endCommandJSON = []byte(`{"command":"end"}`)

// WebsocketSendStream sends data read from r to the websocket as binary
// messages, followed by an "end" command. Cancelling ctx stops the stream and
// closes the websocket.
func WebsocketSendStream(ctx context.Context, conn MessageWriteCloser, r io.Reader, bufferSize int) chan bool {
	ch := make(chan bool)

	if r == nil {
		close(ch)
		return ch
	}

	go func(conn MessageWriteCloser, r io.Reader) {
		defer close(ch) // NOTE(benhoyt): this was "ch <- true", but that can block
		defer closeOnCancel(ctx, conn)()

		stop := make(chan struct{})
		defer close(stop)

		in := ReaderToChannel(r, bufferSize, stop)
	loop:
		for {
			var buf []byte
			var ok bool
			select {
			case buf, ok = <-in:
			case <-ctx.Done():
				break loop
			}
			if !ok {
				break
			}

			err := conn.Write(ctx, websocket.MessageBinary, buf)
			if err != nil {
				logger.Debugf("Got err writing %s", err)
				break
			}
		}
		conn.Write(ctx, websocket.MessageText, endCommandJSON)
	}(conn, r)

	return ch
}

// WebsocketRecvStream writes binary messages received from the websocket to
// w until an "end" command is received or the websocket is closed. Cancelling
// ctx stops the stream and closes the websocket.
func WebsocketRecvStream(ctx context.Context, w io.Writer, conn MessageReadCloser) chan bool {
	ch := make(chan bool)

	go func() {
		defer close(ch)
		defer closeOnCancel(ctx, conn)()
		recvLoop(ctx, w, conn)
	}()

	return ch
}

func recvLoop(ctx context.Context, w io.Writer, conn MessageReader) {
	buf := make([]byte, 32*1024) // only allocate once per websocket, not once per loop

	for {
		mt, r, err := conn.Reader(ctx)
		if err != nil {
			if ctx.Err() != nil {
				logger.Debugf("Reader cancelled: %v", err)
			} else if websocket.CloseStatus(err) != -1 {
				logger.Debugf("Got close message for reader")
			} else if !IsAbnormalClosure(err) {
				logger.Debugf("Cannot get next reader: %v", err)
			}
			return
		}

		switch mt {
		case websocket.MessageText:
			// A TEXT message is an out-of-band "command".
			payload, err := io.ReadAll(r)
			if err != nil {
				logger.Debugf("Cannot read from message reader: %v", err)
				return
			}
			var command struct {
				Command string `json:"command"`
			}
			err = json.Unmarshal(payload, &command)
			if err != nil {
				logger.Noticef("Cannot decode I/O command: %v", err)
				continue
			}
			switch command.Command {
			case "end":
				logger.Debugf(`Got message barrier ("end" command)`)
				return
			default:
				logger.Noticef("Invalid I/O command %q", command.Command)
			}

		case websocket.MessageBinary:
			// A BINARY message is actual I/O data.
			_, err := io.CopyBuffer(w, r, buf)
			if err != nil {
				logger.Debugf("Cannot copy message to writer: %v", err)
				return
			}

		default:
			logger.Noticef("Invalid message type %d", mt)
		}
	}
}

// IsAbnormalClosure reports whether err indicates that the peer dropped the
// connection without sending a close frame (close code 1006 in RFC 6455).
func IsAbnormalClosure(err error) bool {
	if err == nil || websocket.CloseStatus(err) != -1 || errors.Is(err, net.ErrClosed) {
		return false
	}
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

func ReaderToChannel(r io.Reader, bufferSize int, stop <-chan struct{}) <-chan []byte {
	if bufferSize <= 128*1024 {
		bufferSize = 128 * 1024
	}

	ch := make(chan ([]byte))

	go func() {
		defer close(ch)

		readSize := 128 * 1024
		offset := 0
		buf := make([]byte, bufferSize)

		for {
			read := buf[offset : offset+readSize]
			nr, err := r.Read(read)
			offset += nr
			if offset > 0 && (offset+readSize >= bufferSize || err != nil) {
				select {
				case ch <- buf[0:offset]:
				case <-stop:
					return
				}
				offset = 0
				buf = make([]byte, bufferSize)
			}

			if err != nil {
				break
			}
		}
	}()

	return ch
}
