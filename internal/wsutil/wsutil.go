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

func DefaultWriter(conn MessageReader, w io.WriteCloser, writeDone chan<- bool) {
	recvLoop(w, conn)
	writeDone <- true
	w.Close()
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
		ch <- true
	}()

	return ch
}

func recvLoop(w io.Writer, conn MessageReader) {
	for {
		mt, r, err := conn.NextReader()
		if err != nil {
			logger.Debugf("Cannot get next reader: %v", err)
			break
		}

		if mt == websocket.CloseMessage {
			logger.Debugf("Got close message for reader")
			break
		}

		if mt == websocket.TextMessage {
			var command struct {
				Command string `json:"command"`
			}
			err := json.NewDecoder(r).Decode(&command)
			if err != nil {
				logger.Noticef("Cannot decode I/O command: %v", err)
				continue
			}
			if command.Command != "end" {
				logger.Noticef("Invalid I/O command %q", command.Command)
				continue
			}
			logger.Debugf("Got message barrier (%q command)", command.Command)
			break
		}

		buf, err := ioutil.ReadAll(r)
		if err != nil {
			logger.Debugf("Cannot read from message reader: %v", err)
			break
		}
		n, err := w.Write(buf)
		if n != len(buf) {
			logger.Debugf("Wrote %d bytes instead of %d", n, len(buf))
			break
		}
		if err != nil {
			logger.Debugf("Cannot write buf: %v", err)
			break
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
