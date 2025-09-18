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
	"slices"
	"sync"
	"time"
)

var (
	appID = "pebble"
)

// A Logger is a fairly minimal logging tool.
type Logger interface {
	// Notice is for messages that the user should see
	Noticef(format string, v ...any)
	// Debug is for messages that the user should be able to find if they're debugging something
	Debugf(format string, v ...any)
}

type nullLogger struct{}

func (nullLogger) Noticef(format string, v ...any) {}
func (nullLogger) Debugf(format string, v ...any)  {}

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
	logger.Noticef("PANIC "+format, v...)
	panic(fmt.Sprintf(format, v...))
}

// Noticef notifies the user of something
func Noticef(format string, v ...any) {
	loggerLock.Lock()
	defer loggerLock.Unlock()
	logger.Noticef(format, v...)
}

// Debugf records something in the debug log
func Debugf(format string, v ...any) {
	loggerLock.Lock()
	defer loggerLock.Unlock()
	logger.Debugf(format, v...)
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
	logger.Noticef("%s", buf.Bytes())
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
func (l *defaultLogger) Debugf(format string, v ...any) {
	if os.Getenv("PEBBLE_DEBUG") == "1" {
		l.Noticef("DEBUG "+format, v...)
	}
}

// Noticef alerts the user about something, as well as putting it syslog
func (l *defaultLogger) Noticef(format string, v ...any) {
	l.buf = l.buf[:0]
	l.buf = AppendTimestamp(l.buf, time.Now())
	l.buf = append(l.buf, ' ')
	l.buf = append(l.buf, l.prefix...)
	l.buf = fmt.Appendf(l.buf, format, v...)
	if l.buf[len(l.buf)-1] != '\n' {
		l.buf = append(l.buf, '\n')
	}
	l.w.Write(l.buf)
}

// New creates a log.Logger using the given io.Writer and prefix (which is
// printed between the timestamp and the message).
func New(w io.Writer, prefix string) Logger {
	return &defaultLogger{
		w:      w,
		prefix: prefix,
		buf:    make([]byte, 0, 256),
	}
}

// AppendTimestamp appends a timestamp in format "YYYY-MM-DDTHH:mm:ss.sssZ" to
// the given byte slice and returns the extended slice.
//
// The timestamp is always in UTC and has exactly 3 fractional digits
// (millisecond precision). Makes no allocations if b has enough capacity.
func AppendTimestamp(b []byte, t time.Time) []byte {
	const capacity = 24

	utc := t.UTC()

	year := utc.Year()
	month := int(utc.Month())
	day := utc.Day()
	hour := utc.Hour()
	minute := utc.Minute()
	second := utc.Second()

	// Convert nanoseconds to milliseconds as we use millisecond precision.
	millisecond := utc.Nanosecond() / 1_000_000

	// Ensure slice has enough capacity, and extend length.
	b = slices.Grow(b, capacity)
	b = b[:capacity]

	// Write year (4 digits)
	b[0] = byte('0' + year/1000%10)
	b[1] = byte('0' + year/100%10)
	b[2] = byte('0' + year/10%10)
	b[3] = byte('0' + year%10)
	b[4] = '-'

	// Write month (2 digits)
	b[5] = byte('0' + month/10)
	b[6] = byte('0' + month%10)
	b[7] = '-'

	// Write day (2 digits)
	b[8] = byte('0' + day/10)
	b[9] = byte('0' + day%10)
	b[10] = 'T'

	// Write hour (2 digits)
	b[11] = byte('0' + hour/10)
	b[12] = byte('0' + hour%10)
	b[13] = ':'

	// Write minute (2 digits)
	b[14] = byte('0' + minute/10)
	b[15] = byte('0' + minute%10)
	b[16] = ':'

	// Write second (2 digits)
	b[17] = byte('0' + second/10)
	b[18] = byte('0' + second%10)
	b[19] = '.'

	// Write milliseconds (3 digits)
	b[20] = byte('0' + millisecond/100) // millisecond is at most 999, so no need for %10 here
	b[21] = byte('0' + millisecond/10%10)
	b[22] = byte('0' + millisecond%10)
	b[23] = 'Z'

	return b
}
