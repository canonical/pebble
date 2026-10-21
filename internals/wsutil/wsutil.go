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

// MessageWriteCloser is an interface that wraps websocket message writing
// and closing.
type MessageWriteCloser interface {
	MessageWriter
	Close(code websocket.StatusCode, reason string) error
}

var endCommandJSON = []byte(`{"command":"end"}`)

func WebsocketSendStream(conn MessageWriter, r io.Reader, bufferSize int) chan bool {
	ch := make(chan bool)

	if r == nil {
		close(ch)
		return ch
	}

	go func(conn MessageWriter, r io.Reader) {
		stop := make(chan struct{})
		defer close(stop)

		in := ReaderToChannel(r, bufferSize, stop)
		for {
			buf, ok := <-in
			if !ok {
				break
			}

			err := conn.Write(context.Background(), websocket.MessageBinary, buf)
			if err != nil {
				logger.Debugf("Got err writing %s", err)
				break
			}
		}
		conn.Write(context.Background(), websocket.MessageText, endCommandJSON)
		close(ch) // NOTE(benhoyt): this was "ch <- true", but that can block
	}(conn, r)

	return ch
}

func WebsocketRecvStream(w io.Writer, conn MessageReader) chan bool {
	ch := make(chan bool)

	go func() {
		recvLoop(w, conn)
		close(ch)
	}()

	return ch
}

func recvLoop(w io.Writer, conn MessageReader) {
	buf := make([]byte, 32*1024) // only allocate once per websocket, not once per loop

	for {
		mt, r, err := conn.Reader(context.Background())
		if err != nil {
			if websocket.CloseStatus(err) != -1 {
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
