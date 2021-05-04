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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/canonical/pebble/internal/overlord/servstate"
	"github.com/canonical/pebble/internal/servicelog"
)

const (
	defaultNumLogs = 10
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

// Response implementation to serve the logs in a custom format.
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

	// Output format is not exactly text/plain, but it's not pure JSON either.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if numLogs != 0 {
		if len(itsByName) == 1 {
			// Handle single-service case efficiently
			for name, it := range itsByName {
				outputLogsSingle(w, name, it)
			}
		} else if numLogs < 0 {
			// n<0, output all logs from all services in time order
			outputLogsAll(w, itsByName)
		} else {
			outputLogsMulti(w, itsByName, numLogs)
		}
	}
	if follow {
		flushWriter(w)
		followLogs(w, itsByName, req.Context().Done())
	}
}

// Efficiently output a single iterator's logs.
func outputLogsSingle(w io.Writer, service string, it servicelog.Iterator) {
	now := time.Now().UTC() // stop if we iterate past current time
	for it.Next() && it.Timestamp().Before(now) {
		err := writeLog(w, service, it.Timestamp(), it.StreamID(), it.Length(), it)
		if err != nil {
			fmt.Fprintf(w, "\ncannot write log from %q: %v", service, err)
			return
		}
	}
}

// Output last numLogs logs from multiple iterators (less efficient as it
// writes them all to a slice first, and sorts by timestamp).
func outputLogsMulti(w io.Writer, itsByName map[string]servicelog.Iterator, numLogs int) {
	type entry struct {
		timestamp time.Time
		service   string
		stream    servicelog.StreamID
		message   string
	}

	// Write all entries to slice.
	var entries []entry
	var buf bytes.Buffer
	now := time.Now().UTC() // stop if we iterate past current time
	for name, it := range itsByName {
		for it.Next() && it.Timestamp().Before(now) {
			buf.Reset()
			_, err := io.Copy(&buf, it)
			if err != nil {
				fmt.Fprintf(w, "\ncannot write log from %q: %v", name, err)
				return
			}
			entries = append(entries, entry{
				timestamp: it.Timestamp(),
				service:   name,
				stream:    it.StreamID(),
				message:   buf.String(),
			})
		}
	}

	// Order by timestamp (or service name if timestamps equal).
	sort.Slice(entries, func(i, j int) bool {
		ei, ej := entries[i], entries[j]
		if ei.timestamp == ej.timestamp {
			return ei.service < ei.service
		}
		return ei.timestamp.Before(ej.timestamp)
	})

	// Output (up to) the last numLogs logs from the sorted slice.
	index := len(entries) - numLogs
	if index < 0 {
		index = 0
	}
	for _, e := range entries[index:] {
		message := strings.NewReader(e.message)
		err := writeLog(w, e.service, e.timestamp, e.stream, len(e.message), message)
		if err != nil {
			fmt.Fprintf(w, "\ncannot write log from %q: %v", e.service, err)
			return
		}
	}
}

// Output all logs from multiple iterators in timestamp order.
func outputLogsAll(w io.Writer, itsByName map[string]servicelog.Iterator) {
	// Service names, ordered alphabetically for consistent output.
	names := make([]string, 0, len(itsByName))
	for name := range itsByName {
		names = append(names, name)
	}
	sort.Strings(names)

	// Slice of iterators (or nil) holding the next log from each service.
	its := make([]servicelog.Iterator, len(names))
	now := time.Now().UTC() // stop if we iterate past current time
	for {
		// Grab next log for iterators that need fetching (its[i] nil).
		for i, name := range names {
			if its[i] == nil {
				it := itsByName[name]
				if it.Next() && it.Timestamp().Before(now) {
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
		it := its[earliest]
		err := writeLog(w, names[earliest], it.Timestamp(), it.StreamID(), it.Length(), it)
		if err != nil {
			fmt.Fprintf(w, "\ncannot write log from %q: %v", names[earliest], err)
			return
		}

		// Set iterator to nil so we fetch next log for that service.
		its[earliest] = nil
	}
}

// Follow iterators and output logs as they're written.
func followLogs(w io.Writer, itsByName map[string]servicelog.Iterator, done <-chan struct{}) {
	// Start one goroutine per service to listen for new logs.
	writeMutex := &sync.Mutex{}
	var wg sync.WaitGroup
	for name, it := range itsByName {
		out := &lockingLogWriter{w: w, mutex: writeMutex}
		wg.Add(1)
		go func(name string, it servicelog.Iterator) {
			defer wg.Done()
			servicelog.Sink(it, out, name, done)
		}(name, it)
	}

	// Don't return till client connection is closed and all the Sink()s have
	// finished. We can't just wait for the done channel here, because the
	// client connection will close and the Sink calls may still be accessing
	// it.Next() when the caller calls it.Close(), causing a data race.
	wg.Wait()
}

// Each log write is output as <metadata json> <newline> <message bytes>,
// for example (the "length" field excludes the first newline):
//
// {"time":"2021-04-23T01:28:52.660695091Z","service":"redis","stream":"stdout","length":10}
// message 9
// {"time":"2021-04-23T01:28:52.798839551Z","service":"thing","stream":"stdout","length":11}
// message 10
//
// The reason for this is so that it's possible to efficiently output the bytes
// of the message to a destination writer without additional copying.
type logMeta struct {
	Time    time.Time `json:"time"`
	Service string    `json:"service"`
	Stream  string    `json:"stream"`
	Length  int       `json:"length"`
}

// Write a single log to w from the iterator.
func writeLog(w io.Writer, service string, timestamp time.Time, stream servicelog.StreamID, length int, message io.Reader) error {
	log := logMeta{
		Time:    timestamp,
		Service: service,
		Stream:  stream.String(),
		Length:  length,
	}
	encoder := json.NewEncoder(w)
	err := encoder.Encode(log) // Encode writes the object followed by '\n'
	if err != nil {
		return err
	}
	_, err = io.Copy(w, message) // io.Copy uses message.WriteTo to avoid copy
	if err != nil {
		return err
	}
	return nil
}

type lockingLogWriter struct {
	w     io.Writer
	mutex *sync.Mutex
}

func (w *lockingLogWriter) WriteLog(timestamp time.Time, serviceName string, stream servicelog.StreamID, length int, message io.Reader) error {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	err := writeLog(w.w, serviceName, timestamp, stream, length, message)
	if err != nil {
		fmt.Fprintf(w.w, "\ncannot write log from %q: %v", serviceName, err)
		return err
	}
	flushWriter(w.w) // Flush HTTP response after each line in follow mode
	return nil
}

func flushWriter(w io.Writer) {
	flusher, ok := w.(interface{ Flush() })
	if ok {
		flusher.Flush()
	}
}
