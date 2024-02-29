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

package servicelog

import (
	"io"
	"regexp"
	"strings"
)

// Used to strip the Pebble log prefix, for example: "2006-01-02T15:04:05.000Z [service] "
// Timestamp must match format in logger.timestampFormat.
var timestampServiceRegexp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z \[[^]]+\] `)

// LastLines fetches the last n lines of output and, if stripPrefix is true,
// strips the timestamp and service name prefix from each line. If there are
// more than n lines, the result is prefixed with a "(...)" line.
func LastLines(logBuffer *RingBuffer, n int, indent string, stripPrefix bool) (string, error) {
	it := logBuffer.HeadIterator(n + 1)
	defer it.Close()
	logBytes, err := io.ReadAll(it)
	if err != nil {
		return "", err
	}

	// Indent lines
	trimmed := strings.TrimSpace(string(logBytes))
	lines := strings.Split(trimmed, "\n")
	if len(lines) > n {
		// Prefix with truncation marker if too many lines
		lines[0] = "(...)"
	}
	for i, line := range lines {
		if stripPrefix {
			// Strip Pebble timestamp and "[service]" prefix
			line = timestampServiceRegexp.ReplaceAllString(line, "")
		}
		lines[i] = indent + line
	}
	return strings.Join(lines, "\n"), nil
}
