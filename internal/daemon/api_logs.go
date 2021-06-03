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
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/canonical/pebble/internal/logger"
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

	// Get log iterators by service (and close them when we're done).
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

	// Use a buffered channel as a FIFO for keeping the latest numLogs logs if
	// request "n" is set (the default).
	var fifo chan servicelog.Entry
	if numLogs > 0 {
		fifo = make(chan servicelog.Entry, numLogs)
	}
	flushFifo := func() error { // helper to flush any logs in the FIFO
		if numLogs <= 0 || len(fifo) == 0 {
			return nil
		}
		var err error
		for len(fifo) > 0 && err == nil {
			err = encoder.Encode(jsonLog(<-fifo))
		}
		if err != nil {
			logger.Noticef("error writing logs: %v", err)
			return err
		}
		flushWriter(w)
		return nil
	}

	// Close the streamLogs done channel when either the request is cancelled
	// or we return from this handler.
	returning := make(chan struct{})
	defer close(returning)
	done := make(chan struct{}, 1)
	go func() {
		select {
		case <-req.Context().Done():
		case <-returning:
		}
		done <- struct{}{}
	}()

	// Background goroutine to stream ordered logs: it sends parsed logs on
	// logs channel, any error on errors channel.
	logs := make(chan servicelog.Entry)
	errors := make(chan error)
	go streamLogs(itsByName, logs, errors, done)

	// Main loop: output earliest log per iteration. Stop when request
	// cancelled or there are no more logs (in non-follow mode).
	requestStarted := time.Now().UTC()
	for {
		select {
		case log := <-logs:
			if log.Time.IsZero() {
				// Zero-time log means we've consumed all buffered logs
				if flushFifo() != nil {
					return
				}
				if follow {
					// Following, wait for more
					numLogs = 0 // so we don't use the FIFO from here on
					continue
				}
				// Not following, we're done
				return
			}

			// Logs are coming faster than we can send them (probably a slow
			// client), so stop now.
			if !follow && log.Time.After(requestStarted) {
				flushFifo()
				return
			}

			if numLogs > 0 {
				// Push through FIFO so we only output the first "n" across
				// all services.
				if len(fifo) == cap(fifo) {
					// FIFO channel full, discard oldest log entry before
					// writing new one so it doesn't block.
					<-fifo
				}
				fifo <- log
				continue
			}

			// Otherwise encode and output log directly.
			err := encoder.Encode(jsonLog(log))
			if err != nil {
				logger.Noticef("error writing logs: %v", err)
				return
			}
			if follow {
				flushWriter(w)
			}

		case err := <-errors:
			logger.Noticef("%s", err)
			return

		case <-req.Context().Done():
			return
		}
	}
}

// streamLogs reads and parses logs from the given iterators, merging the
// log streams and ordering by timestamp. It sends the parsed logs to the
// logs channel, and any error to the errors channel. It returns when the
// done channel is closed.
func streamLogs(
	itsByName map[string]servicelog.Iterator,
	logs chan<- servicelog.Entry,
	errors chan<- error,
	done <-chan struct{},
) {
	// Make a channel and register it with each of the iterators to be
	// notified when new data comes in. We don't strictly need this when not
	// following, but it doesn't hurt either.
	notification := make(chan bool, 1)
	for _, it := range itsByName {
		it.Notify(notification)
	}

	// Make sorted list of service names we have iterators for.
	var services []string
	for name := range itsByName {
		services = append(services, name)
	}
	sort.Strings(services)

	// Create an iterator and log parser for each service.
	iterators := make([]servicelog.Iterator, len(services))
	parsers := make([]*servicelog.Parser, len(services))
	for i, name := range services {
		iterators[i] = itsByName[name]
		parsers[i] = servicelog.NewParser(iterators[i], logReaderSize)
	}

	// Slice of next entries for each service
	nexts := make([]servicelog.Entry, len(services))

	// Main loop: output earliest log per iteration. Stop when done is closed.
	for {
		// Try to fetch next log from each service (parser/iterator combo).
		for i, parser := range parsers {
			if !nexts[i].Time.IsZero() {
				continue
			}
			if parser.Next() {
				nexts[i] = parser.Entry()
			} else if parser.Err() != nil {
				errors <- fmt.Errorf("error parsing logs: %w", parser.Err())
				return
			} else if iterators[i].Next(nil) {
				// Parsed all in parser buffer, but iterator now has more.
				if parser.Next() {
					nexts[i] = parser.Entry()
				}
			}
		}

		// Find the log with the next earliest timestamp.
		earliest := -1
		for i, next := range nexts {
			if next.Time.IsZero() {
				continue
			}
			if earliest < 0 || next.Time.Before(nexts[earliest].Time) {
				earliest = i
			}
		}

		// No more logs: send empty log to caller, then wait for more logs
		// or done signal.
		if earliest < 0 {
			logs <- servicelog.Entry{}
			select {
			case <-notification:
			case <-done:
				return
			}
			continue
		}

		// Send log to caller.
		logs <- nexts[earliest]
		nexts[earliest].Time = time.Time{} // so corresponding iterator is fetched next loop
	}
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
