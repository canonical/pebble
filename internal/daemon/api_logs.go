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

package daemon

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/canonical/pebble/internal/overlord/servstate"
	"github.com/canonical/pebble/internal/servicelog"
)

const (
	defaultNumLogs = 10
	logReaderSize  = 4 * 1024
)

type serviceManager interface {
	Services(names []string) ([]*servstate.ServiceInfo, error)
	ServiceLogs(services []string, last int) (map[string]servicelog.Iterator, error)
}

func v1GetLogs(cmd *Command, _ *http.Request, _ *userState) Response {
	return logsResponse{
		svcMgr: cmd.d.overlord.ServiceManager(),
	}
}

// logsResponse is a Response implementation to serve the logs in a custom
// JSON Lines format.
type logsResponse struct {
	svcMgr serviceManager
}

func (r logsResponse) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	query := req.URL.Query()

	services := query["services"]

	followStr := query.Get("follow")
	if followStr != "" && followStr != "true" && followStr != "false" {
		response := statusBadRequest(`follow parameter must be "true" or "false"`)
		response.ServeHTTP(w, req)
		return
	}
	follow := followStr == "true"

	numLogs := defaultNumLogs
	nStr := query.Get("n")
	if nStr != "" {
		n, err := strconv.Atoi(nStr)
		if err != nil {
			response := statusBadRequest("n must be a valid integer")
			response.ServeHTTP(w, req)
			return
		}
		numLogs = n
	}

	// If "services" parameter not specified, fetch logs for all services.
	if len(services) == 0 {
		infos, err := r.svcMgr.Services(nil)
		if err != nil {
			response := statusInternalError("cannot fetch services: %v", err)
			response.ServeHTTP(w, req)
			return
		}
		services = make([]string, len(infos))
		for i, info := range infos {
			services[i] = info.Name
		}
	}

	// Get log iterators by service name (and close them when we're done).
	itsByName, err := r.svcMgr.ServiceLogs(services, numLogs)
	if err != nil {
		response := statusInternalError("cannot fetch log iterators: %v", err)
		response.ServeHTTP(w, req)
		return
	}
	defer func() {
		for _, it := range itsByName {
			_ = it.Close()
		}
	}()

	// Output format is JSON Lines, which doesn't have an official mime type,
	// but "application/x-ndjson" is what most people seem to use:
	// https://github.com/wardi/jsonlines/issues/9
	w.Header().Set("Content-Type", "application/x-ndjson")
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)

	// First write buffered logs
	if numLogs != 0 {
		if numLogs < 0 {
			// Client asked for all logs; write everything from the iterators
			// (which are TailIterators).
			err = writeLogs(itsByName, func(entry logEntry) error {
				return encoder.Encode(entry)
			})
		} else {
			// Client asked for n logs, collect from iterators (which are
			// HeadIterator(n)) to a FIFO queue of size n and drop oldest. This
			// avoids reading numServices*numLogs into memory and sorting.
			fifo := make(chan logEntry, numLogs)
			err = writeLogs(itsByName, func(entry logEntry) error {
				// If FIFO channel is full, discard oldest log entry before
				// writing new one so it doesn't block.
				if len(fifo) == cap(fifo) {
					<-fifo
				}
				fifo <- entry
				return nil
			})
			// Then write the logs from the FIFO to the response.
			for len(fifo) > 0 && err == nil {
				err = encoder.Encode(<-fifo)
			}
		}
		if err != nil {
			log.Printf("error writing logs: %v", err)
			return
		}
	}

	// If requested, follow and output logs real time until request closed.
	if follow {
		flushWriter(w) // first flush any logs written above

		// followLogs sends logs to output channel as they arrive. Do this
		// until request is cancelled.
		output := make(chan logEntry)
		go followLogs(itsByName, req.Context().Done(), output)
		for entry := range output {
			err = encoder.Encode(entry)
			if err != nil {
				log.Printf("error writing logs: %v", err)
				return
			}
			flushWriter(w)
		}
	}
}

// writeLogs reads logs from the given iterators, merging the log streams and
// calling writeLog for each log.
func writeLogs(itsByName map[string]servicelog.Iterator, writeLog writeLogFunc) error {
	// Stop when we get a log recorded after this time. This prevents
	// continually trying to catch up if the client is slow at reading them.
	stop := time.Now().UTC()

	// Service names, ordered alphabetically for consistent output.
	names := make([]string, 0, len(itsByName))
	for name := range itsByName {
		names = append(names, name)
	}
	sort.Strings(names)

	parsersByName := make(map[string]*logParser, len(itsByName))
	for name, it := range itsByName {
		parsersByName[name] = newLogParser(it, logReaderSize)
	}

	// Slice of parsers (or nil) holding the next log from each service.
	parsers := make([]*logParser, len(names))
	for {
		// Grab next log for iterators that need fetching (parsers[i] nil).
		for i, name := range names {
			if parsers[i] == nil {
				parser := parsersByName[name]
				if parser.Next() && parser.Entry().Time.Before(stop) {
					parsers[i] = parser
				}
			}
		}

		// Find log with earliest timestamp. Linear search is okay here as there
		// will only be a very small number of services (likely 1, 2, or 3).
		earliest := -1
		for i, parser := range parsers {
			if parser == nil {
				continue
			}
			if parser.Err() != nil {
				return parser.Err()
			}
			if earliest < 0 || parser.Entry().Time.Before(parsers[earliest].Entry().Time) {
				earliest = i
			}
		}
		// We're done when it.Next() returned false for all iterators.
		if earliest < 0 {
			break
		}

		// Write the log
		err := writeLog(parsers[earliest].Entry())
		if err != nil {
			return err
		}

		// Set iterator to nil so we fetch next log for that service.
		parsers[earliest] = nil
	}
	return nil
}

type writeLogFunc func(entry logEntry) error

// TODO: move this to servicelog?
// logParser parses and iterates over logs from a Reader until EOF (or another
// error occurs). Each log parser
type logParser struct {
	r     io.Reader
	br    *bufio.Reader
	entry logEntry
	err   error
}

// newLogParser creates a logParser with the given buffer size.
func newLogParser(r io.Reader, size int) *logParser {
	return &logParser{
		r:  r,
		br: bufio.NewReaderSize(r, size),
	}
}

// Reset resets the internal buffer (and clears any error).
func (lp *logParser) Reset() {
	lp.br.Reset(lp.r)
	lp.err = nil
}

// Next parses the next log from the reader and reports whether another log
// is available (false is returned on EOF or other read error).
func (lp *logParser) Next() bool {
	for lp.err == nil {
		line, err := lp.br.ReadSlice('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			// Non EOF error, stop now
			lp.err = err
			break
		}
		if len(line) == 0 {
			lp.err = io.EOF
			break
		}
		if errors.Is(err, io.EOF) {
			// EOF reached, stop iterating after processing line
			lp.err = err
		}
		entry, ok := parseLog(line)
		if ok {
			// Normal log line
			lp.entry = entry
			return true
		}
		if !lp.entry.Time.IsZero() {
			// Partial log line (long line or "output truncated"), use
			// timestamp and service from previous entry.
			lp.entry.Message = string(line)
			return true
		}
	}
	return false
}

// Entry returns the current log entry (should only be called after Next
// returns true).
func (lp *logParser) Entry() logEntry {
	return lp.entry
}

// Err returns the last error that occurred (EOF is not considered an error).
func (lp *logParser) Err() error {
	if errors.Is(lp.err, io.EOF) {
		return nil
	}
	return lp.err
}

// parseLog parses a log entry of the form
// "2021-05-20T15:39:12.345Z [service] log message". Ok is true if a valid log
// entry was parsed, false if the line is not a valid log.
func parseLog(line []byte) (entry logEntry, ok bool) {
	fields := bytes.SplitN(line, []byte(" "), 3)
	if len(fields) != 3 {
		return logEntry{}, false
	}
	// .999 allows any number of fractional seconds (including none at all)
	timestamp, err := time.Parse("2006-01-02T15:04:05.999Z07:00", string(fields[0]))
	if err != nil {
		return logEntry{}, false
	}
	if len(fields[1]) < 3 {
		return logEntry{}, false
	}
	service := string(fields[1][1 : len(fields[1])-1]) // Trim [ and ] from "[service]"
	message := string(fields[2])
	return logEntry{timestamp, service, message}, true
}

// followLogs follows ("tails") the log iterators and sends logs to the output
// channel as they're written. It stops when the done channel is closed, and
// closes the output channel when everything is finished.
func followLogs(itsByName map[string]servicelog.Iterator, done <-chan struct{}, output chan<- logEntry) {
	// Start one goroutine per service to listen for and output new logs.
	var wg sync.WaitGroup
	for _, it := range itsByName {
		wg.Add(1)
		go func(it servicelog.Iterator) {
			defer wg.Done()
			parser := newLogParser(it, logReaderSize)
			for it.Next(done) {
				parser.Reset()
				for parser.Next() {
					output <- parser.Entry()
				}
			}
			if parser.Err() != nil {
				log.Printf("error parsing logs: %v", parser.Err())
			}
		}(it)
	}

	// Don't close output channel till client connection is closed and all
	// iterators have finished. We can't just wait for the done channel here,
	// because the client connection will close and the goroutine may still be
	// calling it.Next() when the caller calls it.Close(), causing a data race.
	wg.Wait()
	close(output)
}

// Each log is written as a JSON object followed by a newline (JSON Lines):
//
// {"time":"2021-04-23T01:28:52.660Z","service":"redis","message":"redis started up"}
// {"time":"2021-04-23T01:28:52.798Z","service":"thing","message":"did something"}
type logEntry struct {
	Time    time.Time `json:"time"`
	Service string    `json:"service"`
	Message string    `json:"message"`
}

func flushWriter(w io.Writer) {
	flusher, ok := w.(interface{ Flush() })
	if ok {
		flusher.Flush()
	}
}
