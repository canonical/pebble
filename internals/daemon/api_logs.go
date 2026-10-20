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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/canonical/x-go/strutil"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/overlord/servstate"
	"github.com/canonical/pebble/internals/servicelog"
)

const (
	defaultNumLogs = 30
	logReaderSize  = 4 * 1024
)

type serviceManager interface {
	Services(names []string) ([]*servstate.ServiceInfo, error)
	ServiceLogs(services []string, last int) (map[string]servicelog.Iterator, error)
}

func v1GetLogs(c *Command, _ *http.Request, _ *UserState) Response {
	return logsResponse{
		svcMgr: overlordServiceManager(c.d.overlord),
	}
}

// logsResponse is a Response implementation to serve the logs in a custom
// JSON Lines format.
type logsResponse struct {
	svcMgr serviceManager
}

func (r logsResponse) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	query := req.URL.Query()

	services := strutil.MultiCommaSeparatedList(query["services"])

	followStr := query.Get("follow")
	if followStr != "" && followStr != "true" && followStr != "false" {
		response := BadRequest(`follow parameter must be "true" or "false"`)
		response.ServeHTTP(w, req)
		return
	}
	follow := followStr == "true"

	var numLogs int
	nStr := query.Get("n")
	if nStr != "" {
		n, err := strconv.Atoi(nStr)
		if err != nil || n < -1 {
			response := BadRequest("n must be -1, 0, or a positive integer")
			response.ServeHTTP(w, req)
			return
		}
		numLogs = n
	} else if follow {
		numLogs = 0
	} else {
		numLogs = defaultNumLogs
	}

	// If "services" parameter not specified, fetch logs for all services.
	if len(services) == 0 {
		infos, err := r.svcMgr.Services(nil)
		if err != nil {
			response := ServerError("cannot fetch services: %v", err)
			response.ServeHTTP(w, req)
			return
		}
		services = make([]string, len(infos))
		for i, info := range infos {
			services[i] = info.Name
		}
	}

	// Start at the head of each service's logs, the backscroll is read by
	// stepping back from there.
	itsByName, err := r.svcMgr.ServiceLogs(services, 0)
	if err != nil {
		response := ServerError("cannot fetch log iterators: %v", err)
		response.ServeHTTP(w, req)
		return
	}

	// Output format is JSON Lines, which doesn't have an official mime type,
	// but "application/x-ndjson" is what most people seem to use:
	// https://github.com/wardi/jsonlines/issues/9
	w.Header().Set("Content-Type", "application/x-ndjson")
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)

	err = streamLogs(req.Context(), w, encoder, itsByName, numLogs, follow)
	if err != nil {
		logger.Noticef("%s", err)
	}
}

// streamLogs writes the most recent numLogs logs (all logs if numLogs is
// negative) from the given services to the encoder, merging the log streams
// and ordering by timestamp. If follow is true, it then writes new logs as
// they arrive until ctx is done.
func streamLogs(
	ctx context.Context, w io.Writer, enc *json.Encoder,
	itsByName map[string]servicelog.Iterator,
	numLogs int, follow bool,
) error {
	defer func() {
		for _, it := range itsByName {
			_ = it.Close()
		}
	}()

	// Make a channel and register it with each of the iterators to be
	// notified when new data comes in. We don't strictly need this when not
	// following, but it doesn't hurt either.
	notification := make(chan bool, 1)
	for _, it := range itsByName {
		it.Notify(notification)
	}

	// Make sorted list of service names we have iterators for.
	services := slices.Sorted(maps.Keys(itsByName))

	// Create an iterator and log parser for each service.
	iterators := make([]servicelog.Iterator, len(services))
	parsers := make([]*servicelog.Parser, len(services))
	for i, name := range services {
		iterators[i] = itsByName[name]
		parsers[i] = servicelog.NewParser(iterators[i], logReaderSize)
	}

	stepBack(iterators, numLogs)

	// Slice of next entries for each service
	nexts := make([]servicelog.Entry, len(services))

	// Main loop: output earliest log per iteration. Stop when ctx is done or
	// there are no more logs (in non-follow mode).
	requestStarted := time.Now().UTC()
	for {
		// Try to fetch next log from each service (parser/iterator combo).
		for i, parser := range parsers {
			if !nexts[i].Time.IsZero() {
				continue
			}
			if parser.Next() {
				nexts[i] = parser.Entry()
			} else if parser.Err() != nil {
				return fmt.Errorf("error parsing logs: %w", parser.Err())
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

		// No more logs: if not following we're done, otherwise wait for more
		// logs or ctx done.
		if earliest < 0 {
			if !follow {
				return nil
			}
			select {
			case <-notification:
			case <-ctx.Done():
				return nil
			}
			continue
		}

		// Logs are coming faster than we can send them (probably a slow
		// client), so stop now.
		if !follow && nexts[earliest].Time.After(requestStarted) {
			return nil
		}

		err := enc.Encode(newJSONLog(nexts[earliest]))
		if err != nil {
			return fmt.Errorf("cannot write logs: %w", err)
		}
		if follow {
			flushWriter(w)
		}
		nexts[earliest].Time = time.Time{} // so corresponding iterator is fetched next loop

		select {
		case <-ctx.Done():
			return nil
		default:
		}
	}
}

// stepBack moves the iterators back line by line, so that when read forwards
// again they start at the most recent numLogs lines across all of them (all
// lines if numLogs is negative).
func stepBack(iterators []servicelog.Iterator, numLogs int) {
	if numLogs == 0 {
		return
	}

	// The current line read backwards from each iterator, or empty if there
	// are no more lines.
	lines := make([]bytes.Buffer, len(iterators))
	previousLine := func(i int) {
		lines[i].Reset()
		if iterators[i].Next(nil) {
			// Each read ends with io.EOF at the end of the line.
			_, _ = lines[i].ReadFrom(iterators[i])
		}
	}

	for i, it := range iterators {
		it.Reverse()
		previousLine(i)
	}

	for n := 0; numLogs < 0 || n < numLogs; n++ {
		// Lines start with a fixed width UTC timestamp, so the most recent
		// line is the lexicographically largest.
		latest := -1
		for i := range lines {
			if lines[i].Len() == 0 {
				continue
			}
			if latest < 0 || bytes.Compare(lines[i].Bytes(), lines[latest].Bytes()) > 0 {
				latest = i
			}
		}
		if latest < 0 {
			break
		}
		// Step back past the latest line, so it's included when reading
		// forwards.
		previousLine(latest)
	}

	for _, it := range iterators {
		it.Forward()
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

func newJSONLog(entry servicelog.Entry) *jsonLog {
	message := strings.TrimSuffix(entry.Message, "\n")
	return &jsonLog{
		Time:    entry.Time,
		Service: entry.Service,
		Message: message,
	}
}

func flushWriter(w io.Writer) {
	flusher, ok := w.(interface{ Flush() })
	if ok {
		flusher.Flush()
	}
}
