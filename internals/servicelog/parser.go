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
	"io"
	"time"
)

const (
	// .999 allows any number of fractional seconds (including none at all)
	// when parsing. This is different from outputTimeFormat.
	parseTimeFormat = "2006-01-02T15:04:05.999Z07:00"
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

	timestamp, err := time.Parse(parseTimeFormat, string(timestampBytes))
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
