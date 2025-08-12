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
	"slices"
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

// appendTimestamp appends a timestamp in format "YYYY-MM-DDTHH:mm:ss.sssZ" to
// the given byte slice and returns the extended slice.
//
// The timestamp is always in UTC and has exactly 3 fractional digits
// (millisecond precision). Makes no allocations if b has enough capacity.
func appendTimestamp(b []byte, t time.Time) []byte {
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

func (f *formatter) Write(p []byte) (nn int, ee error) {
	f.mut.Lock()
	defer f.mut.Unlock()
	written := 0
	for len(p) > 0 {
		if f.writeTimestamp {
			f.writeTimestamp = false
			f.timestampBuffer = appendTimestamp(f.timestampBuffer[:0], time.Now())
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
