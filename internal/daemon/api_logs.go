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
	"sync"
	"time"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/servicelog"
)

func v1GetLogs(cmd *Command, req *http.Request, _ *userState) Response {
	return logsResponse{cmd}
}

// Response implementation to serve the logs in a custom format.
type logsResponse struct {
	cmd *Command
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

	// If "services" parameter not specified, fetch logs for all services.
	if len(services) == 0 {
		infos, err := r.cmd.d.overlord.ServiceManager().Services(nil)
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
	itsByName, err := r.cmd.d.overlord.ServiceManager().ServiceLogs(services)
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

	// Output format is not exactly text/plain, but it's not pure JSON either.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if follow {
		followLogs(w, itsByName, req.Context().Done())
	} else {
		outputLogs(w, itsByName)
	}
}

func outputLogs(w io.Writer, itsByName map[string]servicelog.Iterator) {
	// Service names, ordered alphabetically for consistent output.
	names := make([]string, 0, len(itsByName))
	for name := range itsByName {
		names = append(names, name)
	}
	sort.Strings(names)

	// Slice of iterators (or nil) holding the next log from each service.
	its := make([]servicelog.Iterator, len(names))
	for {
		// Grab next log for iterators that need fetching (its[i] nil).
		for i, name := range names {
			if its[i] == nil {
				it := itsByName[name]
				if it.Next() {
					its[i] = it
				}
			}
		}

		// Find log with earliest timestamp. Linear search is okay here as there
		// will only be a very small number of services (likely 1, 2, or 3).
		earliest := -1
		for i, it := range its {
			if it == nil {
				continue
			}
			if earliest < 0 || it.Timestamp().Before(its[earliest].Timestamp()) {
				earliest = i
			}
		}
		// We're done when it.Next() returned false for all iterators.
		if earliest < 0 {
			break
		}

		// Write log with earliest timestamp.
		err := writeLog(w, names[earliest], its[earliest])
		if err != nil {
			fmt.Fprintf(w, "\ncannot write log: %v", err)
			return
		}

		// Set iterator to nil so we fetch next log for that service.
		its[earliest] = nil
	}
}

func followLogs(w io.Writer, itsByName map[string]servicelog.Iterator, done <-chan struct{}) {
	// Start one goroutine per service to listen for new logs.
	writeMutex := &sync.Mutex{}
	for name, it := range itsByName {
		go followLog(w, writeMutex, done, name, it)
	}

	// Don't return till client connection is closed.
	<-done
}

func followLog(w io.Writer, writeMutex *sync.Mutex, done <-chan struct{}, name string, it servicelog.Iterator) {
	for it.Next() {
		// Catch up to current log.
	}

	for {
		more := it.More()
		for it.Next() {
			// Ensure we don't miss any buffered logs.
			writeLogLocked(w, writeMutex, name, it)
		}

		// Wait for next log (or connection closed).
		select {
		case <-more:
			it.Next()
			writeLogLocked(w, writeMutex, name, it)
		case <-done:
			// Stop when client connection is closed.
			return
		}
	}
}

// Each log write is output as <metadata json> <newline> <message bytes>,
// for example (the "length" field excludes the first newline):
//
// {"time":"2021-04-23T01:28:52.660695091Z","service":"redis","stream":"stdout","length":10}
// message 9
// {"time":"2021-04-23T01:28:52.798839551Z","service":"thing","stream":"stdout","length":11}
// message 10
type logMeta struct {
	Time    time.Time `json:"time"`
	Service string    `json:"service"`
	Stream  string    `json:"stream"`
	Length  int       `json:"length"`
}

// Write a single log to w from the iterator.
func writeLog(w io.Writer, service string, it servicelog.Iterator) error {
	log := logMeta{
		Time:    it.Timestamp(),
		Service: service,
		Stream:  it.StreamID().String(),
		Length:  it.Length(),
	}
	encoder := json.NewEncoder(w)
	err := encoder.Encode(log)
	if err != nil {
		return err
	}
	_, err = it.WriteTo(w)
	if err != nil {
		return err
	}
	return nil
}

func writeLogLocked(w io.Writer, writeMutex *sync.Mutex, service string, it servicelog.Iterator) {
	writeMutex.Lock()
	defer writeMutex.Unlock()

	err := writeLog(w, service, it)
	if err != nil {
		logger.Noticef("cannot write log from %s: %v", service, err)
		return
	}
	flushWriter(w)
}

func flushWriter(w io.Writer) {
	flusher, ok := w.(interface{ Flush() })
	if ok {
		flusher.Flush()
	}
}
