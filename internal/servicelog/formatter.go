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
	"sync"
	"time"
)

type syslogTCP struct {
	Host string
}

func (s *syslogTCP) Write(p []byte) (int, error) {
	panic("unimplemented")
}

type BranchWriter struct {
	dsts   []io.Writer
	buf    []byte
	ch     chan []byte
	errors []error
}

func NewBranchWriter(dst ...io.Writer) *BranchWriter {
	b := &BranchWriter{dsts: dst, ch: make(chan []byte)}
	go b.forwardWrites()
	return b
}

func (b *BranchWriter) forwardWrites() {
	// NOTE: do we really want to go async here at the branchling level - this means that the
	// slowest destination/sink dictates how slow we write to *all* destinations.  Or do we want to
	// async/buffer at the destination level?
	for data := range b.ch {
		for dst := range b.dsts {
			_, err := dst.Write(line)
			if err != nil {
				b.errors = append(b.errors, err)
			}
		}
	}
}

func (b *BranchWriter) Close() error {
	b.Flush()
	close(b.ch)
}

// Flush causes all buffered data to be written to the underlying destination writer.  This should
// be called after all writes have been completed.
func (b *BranchWriter) Flush() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.buf) == 0 {
		return nil
	}

	b.write()
	return nil
}

func (b *BranchWriter) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.buf = append(b.buf, p...)
	b.write()
	return len(p), nil
}

func (b *BranchWriter) write() {
	select {
	case w.ch <- w.buf:
		w.buf = w.buf[:0]
	default:
	}
}

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
