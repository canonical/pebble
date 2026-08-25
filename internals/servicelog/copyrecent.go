// Copyright (c) 2026 Canonical Ltd
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

package servicelog

import (
	"io"
	"time"
)

// copyRecentParserSize is the bufio.Reader buffer size used by CopyRecent
// when parsing entries out of the source ring buffer.
const copyRecentParserSize = 4 * 1024

// CopyRecent copies log entries from src that were written at or after cutoff
// into dest, preserving each entry's original timestamp and service name (the
// entries older than cutoff are skipped). It's used, for example, to recover
// recently-buffered log output held in a "shadow" ring buffer once a service
// that is exporting its own logs via OTLP exits.
func CopyRecent(dest io.Writer, src *RingBuffer, cutoff time.Time) error {
	it := src.TailIterator()
	defer it.Close()

	parser := NewParser(it, copyRecentParserSize)
	for parser.Next() {
		entry := parser.Entry()
		if entry.Time.Before(cutoff) {
			continue
		}
		err := WriteEntry(dest, entry.Service, entry.Time, entry.Message)
		if err != nil {
			return err
		}
	}
	return parser.Err()
}
