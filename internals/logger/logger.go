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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

const (
	timestampFormat = "2006-01-02T15:04:05.000Z07:00"
)

var (
	appID = "pebble"
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
	logger     Logger = NullLogger
	loggerLock sync.Mutex
)

// Panicf notifies the user and then panics
func Panicf(format string, v ...any) {
	loggerLock.Lock()
	defer loggerLock.Unlock()
	msg := fmt.Sprintf(format, v...)
	logger.Notice("PANIC " + msg)
	panic(msg)
}

// Noticef notifies the user of something
func Noticef(format string, v ...any) {
	loggerLock.Lock()
	defer loggerLock.Unlock()
	msg := fmt.Sprintf(format, v...)
	logger.Notice(msg)
}

// Debugf records something in the debug log
func Debugf(format string, v ...any) {
	loggerLock.Lock()
	defer loggerLock.Unlock()
	msg := fmt.Sprintf(format, v...)
	logger.Debug(msg)
}

// SecurityWarn logs a security WARN event with the given arguments.
func SecurityWarn(event SecurityEvent, arg, description string) {
	securityEvent("WARN", event, arg, description)
}

// SecurityCritical logs a security CRITICAL event with the given arguments.
func SecurityCritical(event SecurityEvent, arg, description string) {
	securityEvent("CRITICAL", event, arg, description)
}

func securityEvent(level string, event SecurityEvent, arg, description string) {
	loggerLock.Lock()
	defer loggerLock.Unlock()

	eventWithArg := string(event)
	if arg != "" {
		eventWithArg += ":" + arg
	}
	data := struct {
		Type        string `json:"type"`
		DateTime    string `json:"datetime"`
		Level       string `json:"level"`
		Event       string `json:"event"`
		Description string `json:"description,omitempty"`
		AppID       string `json:"appid"`
	}{
		Type:        "security",
		DateTime:    time.Now().UTC().Format(time.RFC3339),
		Level:       level,
		Event:       eventWithArg,
		Description: description,
		AppID:       appID,
	}

	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(data)
	if err != nil {
		// Should never happen, and not much more we can do here.
		return
	}
	logger.Notice(buf.String())
}

type SecurityEvent string

const (
	SecurityAuthzAdmin         SecurityEvent = "authz_admin"
	SecurityAuthzFail          SecurityEvent = "authz_fail"
	SecurityUserCreated        SecurityEvent = "user_created"
	SecurityUserDeleted        SecurityEvent = "user_deleted"
	SecurityUserUpdated        SecurityEvent = "user_updated"
	SecuritySysMonitorDisabled SecurityEvent = "sys_monitor_disabled"
	SecuritySysShutdown        SecurityEvent = "sys_shutdown"
	SecuritySysStartup         SecurityEvent = "sys_startup"
)

// SetAppID sets the "appid" field used for security logging. The default is "pebble".
func SetAppID(s string) {
	appID = s
}

type lockedBytesBuffer struct {
	buffer bytes.Buffer
	mutex  sync.Mutex
}

func (b *lockedBytesBuffer) Write(p []byte) (int, error) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.Write(p)
}

func (b *lockedBytesBuffer) String() string {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	return b.buffer.String()
}

// MockLogger replaces the existing logger with a buffer and returns
// a Stringer returning the log buffer content and a restore function.
func MockLogger(prefix string) (fmt.Stringer, func()) {
	buf := &lockedBytesBuffer{}
	oldLogger := SetLogger(New(buf, prefix))
	return buf, func() {
		SetLogger(oldLogger)
	}
}

// SetLogger sets the global logger to the given one. It must be called
// from a single goroutine before any logs are written.
func SetLogger(l Logger) (old Logger) {
	loggerLock.Lock()
	defer loggerLock.Unlock()
	old = logger
	logger = l
	return old
}

type defaultLogger struct {
	w      io.Writer
	prefix string

	buf []byte
}

// Debug only prints if PEBBLE_DEBUG is set.
func (l *defaultLogger) Debug(msg string) {
	if os.Getenv("PEBBLE_DEBUG") == "1" {
		l.Notice("DEBUG " + msg)
	}
}

// Notice alerts the user about something, as well as putting it syslog
func (l *defaultLogger) Notice(msg string) {
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
