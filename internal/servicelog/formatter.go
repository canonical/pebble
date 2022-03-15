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
	"bytes"
	"io"
	"regexp"
	"sync"
	"time"
)

type formatter struct {
	mut             sync.Mutex
	serviceName     string
	dest            io.Writer
	writeTimestamp  bool
	timestampBuffer []byte
	timestamp       []byte
}

const (
	// outputTimeFormat is RFC3339 with millisecond precision.
	outputTimeFormat = "2006-01-02T15:04:05.000Z07:00"
)

// NewFormatWriter returns a io.Writer that inserts timestamp and service name for every
// line in the stream.
// For the input:
//   first\n
//   second\n
//   third\n
// The expected output is:
//   2021-05-13T03:16:51.001Z [test] first\n
//   2021-05-13T03:16:52.002Z [test] second\n
//   2021-05-13T03:16:53.003Z [test] third\n
func NewFormatWriter(dest io.Writer, serviceName string) *formatter {
	return &formatter{
		serviceName:    serviceName,
		dest:           dest,
		writeTimestamp: true,
	}
}

func (f *formatter) Write(p []byte) (int, error) {
	f.mut.Lock()
	defer f.mut.Unlock()
	written := 0
	for len(p) > 0 {
		if f.writeTimestamp {
			f.writeTimestamp = false
			f.timestampBuffer = time.Now().UTC().AppendFormat(f.timestampBuffer[:0], outputTimeFormat)
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

type TrimWriter struct {
	dest     io.Writer
	re       *regexp.Regexp
	buf      []byte
	postTrim bool
	mut      sync.Mutex
}

func NewTrimWriter(dest io.Writer, re *regexp.Regexp) *TrimWriter {
	return &TrimWriter{
		dest: dest,
		re:   re,
	}
}

func (w *TrimWriter) Flush() error {
	w.mut.Lock()
	defer w.mut.Unlock()

	if len(w.buf) == 0 {
		return nil
	}

	write := w.buf
	w.buf = w.buf[:0]
	return w.write(write)
}

func (w *TrimWriter) Write(p []byte) (int, error) {
	w.mut.Lock()
	defer w.mut.Unlock()

	// Buffered content has already been searched for newlines - track this.
	pos := len(w.buf)

	w.buf = append(w.buf, p...)
	written := len(p) // always report everything we've bufferred as written
	for {
		end := bytes.IndexByte(w.buf[pos:], '\n')
		if end == -1 {
			return written, nil // wait for rest of line
		}
		end += pos

		err := w.write(w.buf[:end+1])
		if err != nil {
			return written, err
		}
		w.buf = w.buf[end+1:]
		pos = 0
	}
}

func (w *TrimWriter) write(p []byte) error {
	start := 0
	loc := w.re.FindIndex(p)
	if loc != nil {
		start = loc[1]
	}

	p = p[start:]
	for len(p) > 0 {
		n, err := w.dest.Write(p)
		p = p[n:]
		if err != nil {
			return err
		}
	}
	return nil
}
