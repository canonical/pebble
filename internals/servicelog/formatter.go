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
// along with this program.  If not, see <http://www.gnu.org/licenses/>

package servicelog

import (
	"io"
	"strings"
	"sync"
	"time"

	"github.com/canonical/pebble/internals/logger"
)

type formatter struct {
	mut             sync.Mutex
	serviceName     string
	dest            io.Writer
	writeTimestamp  bool
	timestampBuffer []byte
	timestamp       []byte
}

// NewFormatWriter returns a io.Writer that inserts timestamp and service name for every
// line in the stream.
// For the input:
//
//	first\n
//	second\n
//	third\n
//
// The expected output is:
//
//	2021-05-13T03:16:51.001Z [test] first\n
//	2021-05-13T03:16:52.002Z [test] second\n
//	2021-05-13T03:16:53.003Z [test] third\n
func NewFormatWriter(dest io.Writer, serviceName string) io.Writer {
	return &formatter{
		serviceName:    serviceName,
		dest:           dest,
		writeTimestamp: true,
	}
}

func (f *formatter) Write(p []byte) (nn int, ee error) {
	f.mut.Lock()
	defer f.mut.Unlock()
	written := 0
	for len(p) > 0 {
		if f.writeTimestamp {
			f.writeTimestamp = false
			f.timestampBuffer = logger.AppendTimestamp(f.timestampBuffer[:0], time.Now())
			f.timestampBuffer = append(f.timestampBuffer, " ["...)
			f.timestampBuffer = append(f.timestampBuffer, f.serviceName...)
			f.timestampBuffer = append(f.timestampBuffer, "] "...)
			f.timestamp = f.timestampBuffer
		}

		for len(f.timestamp) > 0 {
			// Timestamp bytes don't count towards the returned count because they constitute the
			// encoding not the payload.
			n, err := f.dest.Write(f.timestamp)
			f.timestamp = f.timestamp[n:]
			if err != nil {
				return written, err
			}
		}

		length := 0
		for i := 0; i < len(p); i++ {
			length++
			if p[i] == '\n' {
				f.writeTimestamp = true
				break
			}
		}

		write := p[:length]
		n, err := f.dest.Write(write)
		p = p[n:]
		written += n
		if err != nil {
			return written, err
		}
	}
	return written, nil
}

// WriteEntry formats and writes a single log entry to dest, using the
// supplied timestamp rather than the time at which the write occurs.
//
// If message contains multiple lines, each line is written as a separate
// entry sharing the same timestamp and service name, matching the format
// produced by NewFormatWriter so entries can be parsed consistently by
// Parse.
func WriteEntry(dest io.Writer, serviceName string, t time.Time, message string) error {
	message = strings.TrimSuffix(message, "\n")
	lines := strings.Split(message, "\n")
	var buf []byte
	for _, line := range lines {
		buf = logger.AppendTimestamp(buf[:0], t)
		buf = append(buf, " ["...)
		buf = append(buf, serviceName...)
		buf = append(buf, "] "...)
		buf = append(buf, line...)
		buf = append(buf, '\n')
		if _, err := dest.Write(buf); err != nil {
			return err
		}
	}
	return nil
}
