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

package logger

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

const (
	timestampFormat = "2006-01-02T15:04:05.000Z07:00"
)

// A Logger is a fairly minimal logging tool.
type Logger interface {
	// Notice is for messages that the user should see
	Notice(msg string)
	// Debug is for messages that the user should be able to find if they're debugging something
	Debug(msg string)
}

type nullLogger struct{}

func (nullLogger) Notice(string) {}
func (nullLogger) Debug(string)  {}

// NullLogger is a logger that does nothing
var NullLogger = nullLogger{}

var (
	logger Logger = NullLogger
)

// Panicf notifies the user and then panics
func Panicf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	logger.Notice("PANIC " + msg)
	panic(msg)
}

// Noticef notifies the user of something
func Noticef(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	logger.Notice(msg)
}

// Debugf records something in the debug log
func Debugf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	logger.Debug(msg)
}

// MockLogger replaces the existing logger with a buffer and returns
// the log buffer and a restore function.
func MockLogger(prefix string) (buf *bytes.Buffer, restore func()) {
	buf = &bytes.Buffer{}
	oldLogger := logger
	SetLogger(New(buf, prefix))
	return buf, func() {
		SetLogger(oldLogger)
	}
}

// SetLogger sets the global logger to the given one. It must be called
// from a single goroutine before any logs are written.
func SetLogger(l Logger) {
	logger = l
}

type defaultLogger struct {
	w      io.Writer
	prefix string

	buf []byte
	mu  sync.Mutex
}

// Debug only prints if PEBBLE_DEBUG is set.
func (l *defaultLogger) Debug(msg string) {
	if os.Getenv("PEBBLE_DEBUG") == "1" {
		l.Notice("DEBUG " + msg)
	}
}

// Notice alerts the user about something, as well as putting it syslog
func (l *defaultLogger) Notice(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.buf = l.buf[:0]
	now := time.Now().UTC()
	l.buf = now.AppendFormat(l.buf, timestampFormat)
	l.buf = append(l.buf, ' ')
	l.buf = append(l.buf, l.prefix...)
	l.buf = append(l.buf, msg...)
	if len(msg) == 0 || msg[len(msg)-1] != '\n' {
		l.buf = append(l.buf, '\n')
	}
	l.w.Write(l.buf)
}

// New creates a log.Logger using the given io.Writer and prefix (which is
// printed between the timestamp and the message).
func New(w io.Writer, prefix string) Logger {
	return &defaultLogger{w: w, prefix: prefix}
}
