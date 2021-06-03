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
	fields := bytes.SplitN(line, []byte(" "), 3)
	if len(fields) != 3 {
		return Entry{}, errParseFields
	}
	timestamp, err := time.Parse(parseTimeFormat, string(fields[0]))
	if err != nil {
		return Entry{}, errParseTime
	}
	if len(fields[1]) < 3 || fields[1][0] != '[' || fields[1][len(fields[1])-1] != ']' {
		return Entry{}, errParseService
	}
	service := string(fields[1][1 : len(fields[1])-1]) // Trim [ and ] from "[service]"
	message := string(fields[2])
	return Entry{timestamp, service, message}, nil
}
