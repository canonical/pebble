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
	"encoding/json"
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
	iterators, err := r.svcMgr.ServiceLogs(services, numLogs)
	if err != nil {
		response := statusInternalError("cannot fetch log iterators: %v", err)
		response.ServeHTTP(w, req)
		return
	}
	defer func() {
		for _, it := range iterators {
			_ = it.Close()
		}
	}()

	// Allocate parsers up-front for use in both writeLogs and followLogs.
	parsers := make(map[string]*servicelog.Parser)
	for name, it := range iterators {
		parsers[name] = servicelog.NewParser(it, logReaderSize)
	}

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
			err = writeLogs(iterators, parsers, func(entry servicelog.Entry) error {
				return encoder.Encode(jsonLog(entry))
			})
		} else {
			// Client asked for n logs, collect from iterators (which are
			// HeadIterator(n)) to a FIFO queue of size n and drop oldest. This
			// avoids reading numServices*numLogs into memory and sorting.
			fifo := make(chan servicelog.Entry, numLogs)
			err = writeLogs(iterators, parsers, func(entry servicelog.Entry) error {
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
				err = encoder.Encode(jsonLog(<-fifo))
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
		output := make(chan servicelog.Entry)
		go followLogs(iterators, parsers, req.Context().Done(), output)
		for entry := range output {
			err = encoder.Encode(jsonLog(entry))
			if err != nil {
				log.Printf("error writing logs: %v", err)
				return
			}
			flushWriter(w)
		}
	}
}

// writeLogs reads logs from the given iterators and parsers, merging the log
// streams and calling writeLog for each log.
func writeLogs(
	iterators map[string]servicelog.Iterator,
	parsers map[string]*servicelog.Parser,
	writeLog writeLogFunc,
) error {
	// Stop when we get a log recorded after this time. This prevents
	// continually trying to catch up if the client is slow at reading them.
	stop := time.Now().UTC()

	// Service names, ordered alphabetically for consistent output.
	names := make([]string, 0, len(iterators))
	for name := range iterators {
		names = append(names, name)
	}
	sort.Strings(names)

	// Slice of parsers (or nil) holding the next log from each service.
	nexts := make([]*servicelog.Parser, len(names))
	for {
		// Grab next log for iterators that need fetching (parsers[i] nil).
		for i, name := range names {
			if nexts[i] == nil {
				parser := parsers[name]
				if parser.Next() && parser.Entry().Time.Before(stop) {
					nexts[i] = parser
				}
			}
		}

		// Find log with earliest timestamp. Linear search is okay here as there
		// will only be a very small number of services.
		earliest := -1
		for i, parser := range nexts {
			if parser == nil {
				continue
			}
			if parser.Err() != nil {
				return parser.Err()
			}
			if earliest < 0 || parser.Entry().Time.Before(nexts[earliest].Entry().Time) {
				earliest = i
			}
		}
		// We're done when it.Next() returned false for all iterators.
		if earliest < 0 {
			break
		}

		// Write the log
		err := writeLog(nexts[earliest].Entry())
		if err != nil {
			return err
		}

		// Set iterator to nil so we fetch next log for that service.
		nexts[earliest] = nil
	}
	return nil
}

type writeLogFunc func(entry servicelog.Entry) error

// followLogs follows ("tails") the log iterators and sends logs to the output
// channel as they're written. It stops when the done channel is closed, and
// closes the output channel when everything is finished.
func followLogs(
	iterators map[string]servicelog.Iterator,
	parsers map[string]*servicelog.Parser,
	done <-chan struct{},
	output chan<- servicelog.Entry,
) {
	// Start one goroutine per service to listen for and output new logs.
	var wg sync.WaitGroup
	for name, it := range iterators {
		wg.Add(1)
		go func(it servicelog.Iterator, parser *servicelog.Parser) {
			defer wg.Done()
			for it.Next(done) {
				parser.Reset()
				for parser.Next() {
					select {
					case output <- parser.Entry():
					case <-done:
						return
					}
				}
			}
			if parser.Err() != nil {
				log.Printf("error parsing logs: %v", parser.Err())
			}
		}(it, parsers[name])
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
type jsonLog struct {
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
