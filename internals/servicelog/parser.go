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
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"time"
)

var (
	errParseFields  = errors.New("line has too few fields")
	errParseTime    = errors.New("invalid log timestamp")
	errParseService = errors.New("invalid log service name")
)

// Entry is a parsed log entry.
type Entry struct {
	Time    time.Time
	Service string
	Message string
}

// Parser parses and iterates over logs from a Reader until EOF (or another
// error occurs).
type Parser struct {
	r     io.Reader
	br    *bufio.Reader
	entry Entry
	err   error
}

// NewParser creates a Parser with the given buffer size.
func NewParser(r io.Reader, size int) *Parser {
	return &Parser{
		r:  r,
		br: bufio.NewReaderSize(r, size),
	}
}

// Next parses the next log from the reader and reports whether another log
// is available (false is returned on EOF or other read error).
func (p *Parser) Next() bool {
	eof := false
	for !eof && p.err == nil {
		line, err := p.br.ReadSlice('\n')
		if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
			// Non EOF (and non-buffer full) error, stop now
			p.err = err
			return false
		}
		if len(line) == 0 {
			return false
		}

		// If EOF reached, stop iterating after processing line.
		eof = errors.Is(err, io.EOF)

		entry, err := Parse(line)
		if err == nil {
			p.entry = entry
			if entry.Message == "" {
				// FormatWriter has only written "timestamp [service]" and not
				// yet written log message, break this iteration so caller can
				// wait for next write. When message comes through it'll use
				// this p.entry's timestamp and service name.
				return false
			}
			// Normal log line
			return true
		}
		if !p.entry.Time.IsZero() {
			// Partial log line due to long line or "(... output truncated ...)",
			// use timestamp and service from previous entry.
			p.entry.Message = string(line)
			return true
		}
	}
	return false
}

// Entry returns the current log entry (should only be called after Next
// returns true).
func (p *Parser) Entry() Entry {
	return p.entry
}

// Err returns the last error that occurred (EOF is not considered an error).
func (p *Parser) Err() error {
	return p.err
}

// Parse parses a log entry of the form
// "2021-05-20T15:39:12.345Z [service] log message".
func Parse(line []byte) (Entry, error) {
	// find first space, telling us where the timestamp ends
	i := bytes.IndexByte(line, ' ')
	if i < 1 {
		return Entry{}, errParseFields
	}
	timestampBytes := line[:i]
	// ensure there is at least one more byte since we're going to slice next,
	// which could panic otherwise
	if len(line) <= i+1 {
		return Entry{}, errParseFields
	}
	line = line[i+1:] // Skip the space
	i = bytes.IndexByte(line, ' ')
	if i < 1 {
		return Entry{}, errParseFields
	}
	serviceBytes := line[:i]

	if len(line) <= i+1 {
		return Entry{}, errParseFields
	}
	messageBytes := line[i+1:] // Skip the space after service

	timestamp, err := parseTime(timestampBytes)
	if err != nil {
		return Entry{}, errParseTime
	}

	if len(serviceBytes) < 3 || serviceBytes[0] != '[' || serviceBytes[len(serviceBytes)-1] != ']' {
		return Entry{}, errParseService
	}
	serviceBytes = serviceBytes[1 : len(serviceBytes)-1] // Trim [ and ] from "[service]"
	return Entry{
		timestamp,
		string(serviceBytes),
		string(messageBytes),
	}, nil
}

// parseTime parses ISO8601/RFC3339-style datetime with variable fractional seconds
// Supported formats:
//   - "2021-05-20T15:39:12Z" (no fractional seconds)
//   - "2021-05-20T15:39:12.3Z" (1 digit)
//   - "2021-05-20T15:39:12.34Z" (2 digits)
//   - "2021-05-20T15:39:12.345Z" (3 digits - milliseconds)
//   - "2021-05-20T15:39:12.345678Z" (6 digits - microseconds)
//   - "2021-05-20T15:39:12.345678901Z" (9 digits - nanoseconds)
//
// UTC timezone only (Z suffix), variable length 20-29 bytes
// Returns time.Time and error, but makes no heap allocations
func parseTime(b []byte) (time.Time, error) {
	n := len(b)
	if n < 20 || n > 29 {
		return time.Time{}, fmt.Errorf("invalid datetime length: expected 20-29, got %d", n)
	}

	// Must end with Z
	if b[n-1] != 'Z' {
		return time.Time{}, fmt.Errorf("datetime must end with Z")
	}

	// Validate base format structure (first 19 chars): YYYY-MM-DDTHH:mm:ss
	if b[4] != '-' || b[7] != '-' || b[10] != 'T' ||
		b[13] != ':' || b[16] != ':' {
		return time.Time{}, fmt.Errorf("invalid datetime format")
	}

	// Parse base components
	year, err := parseInt4Bytes(b[0:4])
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid year: %w", err)
	}

	month, err := parseInt2Bytes(b[5:7])
	if err != nil || month < 1 || month > 12 {
		return time.Time{}, fmt.Errorf("invalid month: %d", month)
	}

	day, err := parseInt2Bytes(b[8:10])
	if err != nil || day < 1 || day > 31 {
		return time.Time{}, fmt.Errorf("invalid day: %d", day)
	}

	hour, err := parseInt2Bytes(b[11:13])
	if err != nil || hour > 23 {
		return time.Time{}, fmt.Errorf("invalid hour: %d", hour)
	}

	minute, err := parseInt2Bytes(b[14:16])
	if err != nil || minute > 59 {
		return time.Time{}, fmt.Errorf("invalid minute: %d", minute)
	}

	second, err := parseInt2Bytes(b[17:19])
	if err != nil || second > 59 {
		return time.Time{}, fmt.Errorf("invalid second: %d", second)
	}

	// Parse fractional seconds if present
	var nanoseconds int
	if n == 20 {
		// Format: "YYYY-MM-DDTHH:mm:ssZ" - no fractional seconds
		nanoseconds = 0
	} else {
		// Format: "YYYY-MM-DDTHH:mm:ss.fffffZ" - has fractional seconds
		if b[19] != '.' {
			return time.Time{}, fmt.Errorf("expected '.' at position 19")
		}

		// Parse fractional part (position 20 to n-2, since last char is Z)
		fracLen := n - 21 // subtract 20 (base length) + 1 (Z)
		if fracLen < 1 || fracLen > 9 {
			return time.Time{}, fmt.Errorf("fractional seconds must be 1-9 digits, got %d", fracLen)
		}

		fracBytes := b[20 : n-1] // exclude the Z
		fracValue, err := parseNDigitsBytes(fracBytes)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid fractional seconds: %w", err)
		}

		// Convert fractional value to nanoseconds
		// fracLen=1: multiply by 100,000,000 (10^8)
		// fracLen=2: multiply by 10,000,000  (10^7)
		// fracLen=3: multiply by 1,000,000   (10^6)
		// fracLen=6: multiply by 1,000       (10^3)
		// fracLen=9: multiply by 1           (10^0)
		multiplier := 1
		for i := fracLen; i < 9; i++ {
			multiplier *= 10
		}
		nanoseconds = fracValue * multiplier
	}

	// Construct time.Time directly
	return time.Date(year, time.Month(month), day, hour, minute, second,
		nanoseconds, time.UTC), nil
}

// parseInt2Bytes parses exactly 2 digits from byte slice without allocations
func parseInt2Bytes(b []byte) (int, error) {
	if len(b) != 2 {
		return 0, fmt.Errorf("expected 2 digits")
	}

	if b[0] < '0' || b[0] > '9' || b[1] < '0' || b[1] > '9' {
		return 0, fmt.Errorf("non-digit character")
	}

	return int(b[0]-'0')*10 + int(b[1]-'0'), nil
}

// parseInt4Bytes parses exactly 4 digits from byte slice without allocations
func parseInt4Bytes(b []byte) (int, error) {
	if len(b) != 4 {
		return 0, fmt.Errorf("expected 4 digits")
	}

	for i := 0; i < 4; i++ {
		if b[i] < '0' || b[i] > '9' {
			return 0, fmt.Errorf("non-digit character at position %d", i)
		}
	}

	return int(b[0]-'0')*1000 + int(b[1]-'0')*100 +
		int(b[2]-'0')*10 + int(b[3]-'0'), nil
}

// parseNDigitsBytes parses 1-9 digits from byte slice without allocations
func parseNDigitsBytes(b []byte) (int, error) {
	n := len(b)
	if n < 1 || n > 9 {
		return 0, fmt.Errorf("expected 1-9 digits, got %d", n)
	}

	result := 0
	for i := 0; i < n; i++ {
		if b[i] < '0' || b[i] > '9' {
			return 0, fmt.Errorf("non-digit character at position %d", i)
		}
		result = result*10 + int(b[i]-'0')
	}

	return result, nil
}
