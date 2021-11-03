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
	"encoding/json"
	"io"
	"io/ioutil"

	"github.com/gorilla/websocket"

	"github.com/canonical/pebble/internal/logger"
)

// MessageReader is an interface that wraps websocket message reading.
type MessageReader interface {
	NextReader() (messageType int, r io.Reader, err error)
}

// MessageWriter is an interface that wraps websocket message writing.
type MessageWriter interface {
	WriteMessage(messageType int, data []byte) error
}

// MessageReadWriter is an interface that wraps websocket message reading and
// writing.
type MessageReadWriter interface {
	MessageReader
	MessageWriter
}

var endCommandJSON = []byte(`{"command":"end"}`)

func WebsocketSendStream(conn MessageWriter, r io.Reader, bufferSize int) chan bool {
	ch := make(chan bool)

	if r == nil {
		close(ch)
		return ch
	}

	go func(conn MessageWriter, r io.Reader) {
		in := ReaderToChannel(r, bufferSize)
		for {
			buf, ok := <-in
			if !ok {
				break
			}

			err := conn.WriteMessage(websocket.BinaryMessage, buf)
			if err != nil {
				logger.Debugf("Got err writing %s", err)
				break
			}
		}
		conn.WriteMessage(websocket.TextMessage, endCommandJSON)
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
		mt, r, err := conn.NextReader()
		if err != nil {
			logger.Debugf("Cannot get next reader: %v", err)
			return
		}

		switch mt {
		case websocket.CloseMessage:
			logger.Debugf("Got close message for reader")
			return

		case websocket.TextMessage:
			// A TEXT message is an out-of-band "command".
			payload, err := ioutil.ReadAll(r)
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

		case websocket.BinaryMessage:
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

func ReaderToChannel(r io.Reader, bufferSize int) <-chan []byte {
	if bufferSize <= 128*1024 {
		bufferSize = 128 * 1024
	}

	ch := make(chan ([]byte))

	go func() {
		readSize := 128 * 1024
		offset := 0
		buf := make([]byte, bufferSize)

		for {
			read := buf[offset : offset+readSize]
			nr, err := r.Read(read)
			offset += nr
			if offset > 0 && (offset+readSize >= bufferSize || err != nil) {
				ch <- buf[0:offset]
				offset = 0
				buf = make([]byte, bufferSize)
			}

			if err != nil {
				close(ch)
				break
			}
		}
	}()

	return ch
}
