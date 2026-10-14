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
	"unicode"
)

// Used to strip the Pebble log prefix, for example: "2006-01-02T15:04:05.000Z [service] "
// Timestamp must match format in logger.timestampFormat.
var (
	// Matches the timestamp and any "[service]" prefix. This is ambiguous if
	// the service name contains a "]" character; see ServiceLastLines.
	timestampServiceRegexp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z \[[^]]+\] `)

	// Matches the timestamp only. Use if the service name is known.
	timestampRegexp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z`)
)

// LastLines fetches the last n lines of output and, if stripPrefix is true,
// strips the timestamp and service name prefix from each line. If there are
// more than n lines, the result is prefixed with a "(...)" line.
//
// Prefix stripping is unreliable for service names containing "]". Use
// ServiceLastLines instead if the service name is known.
func LastLines(logBuffer *RingBuffer, n int, indent string, stripPrefix bool) (string, error) {
	lines, err := lastLines(logBuffer, n)
	if err != nil {
		return "", err
	}

	for i, line := range lines {
		if stripPrefix {
			line = timestampServiceRegexp.ReplaceAllString(line, "")
		}
		lines[i] = indent + line
	}
	return strings.Join(lines, "\n"), nil
}

// ServiceLastLines fetches the last n lines of output for the named service,
// stripping the "<timestamp> [serviceName]" prefix written by the formatter
// writer for each line. Lines without that exact prefix are returned
// unchanged. If there are more than n lines, the result is prefixed with a
// "(...)" line.
func ServiceLastLines(logBuffer *RingBuffer, n int, indent string, serviceName string) (string, error) {
	lines, err := lastLines(logBuffer, n)
	if err != nil {
		return "", err
	}

	servicePrefix := " [" + serviceName + "] "
	for i, line := range lines {
		line = stripLogPrefix(line, servicePrefix)
		lines[i] = indent + line
	}
	return strings.Join(lines, "\n"), nil
}

// lastLines reads the last n lines from logBuffer with leading whitespace
// trimmed. If there are more than n lines, the first is replaced with "(...)".
func lastLines(logBuffer *RingBuffer, n int) ([]string, error) {
	it := logBuffer.HeadIterator(n + 1)
	defer it.Close()
	logBytes, err := io.ReadAll(it)
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimLeftFunc(string(logBytes), unicode.IsSpace)
	trimmed = strings.TrimSuffix(trimmed, "\n")
	lines := strings.Split(trimmed, "\n")
	if len(lines) > n {
		// Prefix with truncation marker if too many lines
		lines[0] = "(...)"
	}
	return lines, nil
}

// stripLogPrefix removes the timestamp followed by servicePrefix from `line`.
// If the line does not start with both, it is returned unchanged.
func stripLogPrefix(line, servicePrefix string) string {
	ts := timestampRegexp.FindString(line)
	if ts == "" {
		return line
	}
	msg, ok := strings.CutPrefix(line[len(ts):], servicePrefix)
	if !ok {
		return line
	}
	return msg
}
