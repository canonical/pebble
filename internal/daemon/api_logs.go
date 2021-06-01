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

	// Make sorted list of service names we actually have iterators for.
	services = services[:0]
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

	// Output format is JSON Lines, which doesn't have an official mime type,
	// but "application/x-ndjson" is what most people seem to use:
	// https://github.com/wardi/jsonlines/issues/9
	w.Header().Set("Content-Type", "application/x-ndjson")
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	requestDone := req.Context().Done()
	requestStarted := time.Now().UTC()

	// If we'll be following real-time, make a channel and register it with
	// each of the iterators to be notified when new data comes in.
	var notification chan bool
	if follow {
		notification = make(chan bool, 1)
		for _, it := range iterators {
			it.Notify(notification)
		}
	}

	// Use a buffered channel as a FIFO for keeping the latest numLogs logs if
	// request "n" is set (the default).
	var fifo chan servicelog.Entry
	if numLogs > 0 {
		fifo = make(chan servicelog.Entry, numLogs)
	}

	// Slice of next entries for each service
	nexts := make([]servicelog.Entry, len(services))

	// Changes to true once we start following
	following := false

	// Main loop: output earliest log per iteration. Stop when no more logs,
	// or when request is cancelled if in follow mode.
	for {
		// Try to fetch next log from each service (parser/iterator combo).
		for i, parser := range parsers {
			if !nexts[i].Time.IsZero() {
				continue
			}
			if parser.Next() {
				nexts[i] = parser.Entry()
			} else if parser.Err() != nil {
				log.Printf("error parsing logs: %v", parser.Err())
				return
			} else if iterators[i].Next(nil) {
				parser.Reset()
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

		// No more logs: flush and exit, or wait for another if in follow mode.
		if earliest < 0 {
			// If they requested "n" logs, flush logs in the FIFO first.
			if numLogs > 0 && len(fifo) > 0 {
				var err error
				for len(fifo) > 0 && err == nil {
					err = encoder.Encode(jsonLog(<-fifo))
				}
				if err != nil {
					log.Printf("error writing logs: %v", err)
					return
				}
				flushWriter(w)
			}
			if !follow {
				// Following not requested, all done.
				break
			}
			// Now following, wait for something from iterators.
			following = true
			select {
			case <-notification:
			case <-requestDone:
				return
			}
			continue
		}
		next := nexts[earliest]
		nexts[earliest].Time = time.Time{} // so corresponding iterator is fetched next loop

		// Stop if not in follow mode and we've moved passed request start
		// time (we're not keeping up with logs coming in).
		if !follow && next.Time.After(requestStarted) {
			break
		}

		// Output the log.
		if following || numLogs <= 0 {
			// If now following or client requested all logs, output immediately.
			err := encoder.Encode(jsonLog(next))
			if err != nil {
				log.Printf("error writing logs: %v", err)
				return
			}
			if following {
				flushWriter(w)
			}
		} else {
			// Not following and numLogs>0, push through FIFO so we only output
			// the first "n" across all services.
			if len(fifo) == cap(fifo) {
				// FIFO channel full, discard oldest log entry before writing
				// new one so it doesn't block.
				<-fifo
			}
			fifo <- next
		}

		// Check for request cancellation every iteration even if not following.
		select {
		case <-requestDone:
			return
		default:
		}
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
