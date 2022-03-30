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
	"strings"
	"sync"
	"time"
)

type formatter struct {
	mu              sync.Mutex
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
	f.mu.Lock()
	defer f.mu.Unlock()
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

// DefaultLayouts provides a list of default timestamp layouts that the
// TrimWriter tries to auto-detect for on logged/written line prefixes.  This
// auto detection only occurs if no explicit layout string is provided to the
// TrimWriter.
var DefaultLayouts = []string{}

// TrimWriter removes a timestamp prefix from each line written (separated by '\n')
// and forwards the trimmed writes to a destination writer.  Internal
// buffering does occur so it is important to call Flush after all writes
// are completed.  If no timestamp layout is explicitly specified,
// auto-detection using a list of pre-defined default formats is attempted.
type TrimWriter struct {
	dest           io.Writer
	mu             sync.Mutex
	buf            []byte
	layout         string
	nfields        int
	detectFailures int
}

// NewTrimWriter creates a writer that strips timestamp prefixes specified using layout. layout
// identifies the timestamp format to trim using the reference time reference format from Go's
// stdlib time package. If layout is the empty string, timestamp format auto-detection using
// pre-selected formats from DefaultLayouts is attempted during initial writes when they take
// place.
func NewTrimWriter(dest io.Writer, layout string) *TrimWriter {
	// layouts need to end in a space because that is how we detect/split off the
	// log line prefix containing the timestamp
	if len(layout) > 0 && layout[len(layout)-1] != ' ' {
		layout += " "
	}
	w := &TrimWriter{dest: dest, layout: layout, nfields: timeLayoutFields(layout)}
	return w
}

// Flush causes all buffered data to be written to the underlying destination writer.  This should
// be called after all writes have been completed.
func (w *TrimWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.buf) == 0 {
		return nil
	}

	write := w.buf
	w.buf = w.buf[:0]
	return w.write(write)
}

func (w *TrimWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

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

func (w *TrimWriter) write(line []byte) error {
	const maxDetectFailures = 10
	const restartDetectionFailures = 2
	if w.layout == "" && w.detectFailures < maxDetectFailures {
		// try to auto-detect the layout
		w.layout = detectLayout(line)
		w.nfields = timeLayoutFields(w.layout)
		if w.layout == "" {
			w.detectFailures++
		} else {
			// detected layout; reset failure count
			w.detectFailures = 0
		}
	}

	length := 0
	if w.layout != "" {
		prefix := timeLayoutPrefix(w.nfields, line)
		length = timeLength(w.layout, prefix)
		if length == 0 {
			// failed to find timestamp
			w.detectFailures++
			if w.detectFailures >= restartDetectionFailures {
				// failed consecutively too much, switch back to layout detection mode
				w.layout = ""
				w.detectFailures = 0
			}
		} else {
			// we found a timestamp again, reset failure count
			w.detectFailures = 0
		}
	}

	line = line[length:]
	for len(line) > 0 {
		n, err := w.dest.Write(line)
		line = line[n:]
		if err != nil {
			return err
		}
	}
	return nil
}

// timeLength calculates and returns the number of leading bytes of prefix that are part of the
// timestamp format specified by layout (Go time package reference time format).  If prefix does
// not contain a timestamp conforming to the given layout, it returns zero.
func timeLength(layout, prefix string) (length int) {
	_, err := time.Parse(layout, prefix)
	if err == nil {
		length = len(prefix)
	} else if err != nil && strings.Contains(err.Error(), "extra text: ") {
		pe, ok := err.(*time.ParseError)
		if ok {
			var extraLength int
			if strings.Contains(pe.Message, "extra text: \"") {
				// go > 1.14
				extraLength = len(pe.Message) - 1 - len(": extra text: \"")
			} else {
				// go <= 1.14.  Oler versions don't put quotes around the extra text content
				extraLength = len(pe.Message) - len(": extra text: ")
			}
			length = len(prefix) - extraLength
		}
	}

	// also skip leading whitespace on the remaining line
	for _, c := range prefix[length:] {
		if c != ' ' {
			break
		}
		length++
	}
	return length
}

// timeLayoutFields returns the number of space-separated fields are in the time layout string
func timeLayoutFields(layout string) int {
	return len(strings.Fields(strings.Replace(layout, "_", " ", -1)))
}

// timeLayoutPrefix Returns a prefix string from line containing nfields whitespace separated
// fields worth of content.  This is used to generate a superset of timestamp prefix bytes.
func timeLayoutPrefix(nfields int, line []byte) string {
	if nfields == 0 {
		return ""
	}

	sep := byte(' ')
	fieldCount := 0
	prevChar := sep
	size := 0
	for i, c := range line {
		// this loop is similar to strings.Fields but it also tracks the total number of bytes - which is
		// lost info with strings.Fields when e.g. runs of whitespace are gobbled.
		if c == sep && prevChar != sep || i == len(line)-1 && c != sep {
			// transitioned from field to sep or we are on the last field
			fieldCount++
			if fieldCount >= nfields {
				size = i + 1
				break
			}
		}
		prevChar = c
	}

	// also skip leading whitespace on the remaining line
	for _, c := range line[size:] {
		if c != ' ' {
			break
		}
		size++
	}

	return string(line[:size])
}

// detectLayout loops over DefaultLayouts and returns the timestamp layout from it (if any) that
// matches the timestamp prefix (if any) in line.  If no layouts match it returns an empty string.
func detectLayout(line []byte) string {
	for _, candidate := range DefaultLayouts {
		// convert the layout to a test date (replace _ with " " padding) to parse against itself
		// and confirm that the layout we are checking is valid.
		testDate := strings.Replace(candidate, "_", " ", -1)
		_, err := time.Parse(candidate, testDate)
		if err != nil {
			panic(err)
		}

		nfields := len(strings.Fields(testDate))
		prefix := timeLayoutPrefix(nfields, line)
		_, err = time.Parse(candidate, prefix)
		if err == nil || (err != nil && strings.Contains(err.Error(), "extra text:")) {
			return candidate
		}
	}
	return ""
}
