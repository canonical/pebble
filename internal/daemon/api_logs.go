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
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/canonical/pebble/internal/overlord/servstate"
	"github.com/canonical/pebble/internal/servicelog"
)

const (
	defaultNumLogs  = 10
	bufioReaderSize = 4 * 1024
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

	// Output format is JSON Lines, which doesn't have an official mime type,
	// but "application/x-ndjson" is what most people seem to use:
	// https://github.com/wardi/jsonlines/issues/9
	w.Header().Set("Content-Type", "application/x-ndjson")

	// First write n buffered logs
	if numLogs != 0 {
		if len(itsByName) == 1 {
			// Handle single-service case more efficiently
			for _, it := range itsByName {
				err = outputLogsSingle(w, it)
			}
		} else {
			err = outputLogsMulti(w, itsByName, numLogs)
		}
		if err != nil {
			fmt.Fprintf(w, "\nerror writing logs: %v", err)
			return
		}
	}

	// If requested, follow and output logs real time until request closed
	if follow {
		flushWriter(w)
		followLogs(w, itsByName, req.Context().Done())
	}
}

// Output a single iterator's logs without reading them all into memory.
func outputLogsSingle(w io.Writer, it servicelog.Iterator) error {
	reader := bufio.NewReaderSize(it, bufioReaderSize)
	now := time.Now().UTC()
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	err := iterateLogs(it, reader, nil, now, func(log logEntry) error {
		return encoder.Encode(log)
	})
	return err
}

// Output last numLogs logs from multiple iterators (not particularly
// efficient, as it writes them all to a slice first, then sorts by time).
func outputLogsMulti(w io.Writer, itsByName map[string]servicelog.Iterator, numLogs int) error {
	// Write all entries to slice.
	var entries []logEntry
	reader := bufio.NewReaderSize(nil, bufioReaderSize)
	now := time.Now().UTC()
	for _, it := range itsByName {
		reader.Reset(it)
		err := iterateLogs(it, reader, nil, now, func(log logEntry) error {
			entries = append(entries, log)
			return nil
		})
		if err != nil {
			return err
		}
	}

	// Order by timestamp (or service name if timestamps equal).
	sort.Slice(entries, func(i, j int) bool {
		ei, ej := entries[i], entries[j]
		if ei.Time.Equal(ej.Time) {
			return ei.Service < ei.Service
		}
		return ei.Time.Before(ej.Time)
	})

	// Output (up to) the last numLogs logs from the sorted slice.
	index := 0
	if numLogs >= 0 && numLogs < len(entries) {
		index = len(entries) - numLogs
	}
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	for _, log := range entries[index:] {
		err := encoder.Encode(log)
		if err != nil {
			return err
		}
	}
	return nil
}

// Follow iterators and output logs as they're written.
func followLogs(w io.Writer, itsByName map[string]servicelog.Iterator, done <-chan struct{}) {
	// Start one goroutine per service to listen for and output new logs.
	writeMutex := &sync.Mutex{}
	var wg sync.WaitGroup
	for service, it := range itsByName {
		wg.Add(1)
		go func(service string, it servicelog.Iterator) {
			defer wg.Done()
			reader := bufio.NewReaderSize(it, bufioReaderSize)
			fw := &followWriter{w, writeMutex}
			encoder := json.NewEncoder(fw)
			encoder.SetEscapeHTML(false)
			err := iterateLogs(it, reader, done, time.Time{}, func(log logEntry) error {
				return encoder.Encode(log)
			})
			if err != nil {
				fmt.Fprintf(w, "\nerror writing logs: %v", err)
				return
			}
		}(service, it)
	}

	// Don't return till client connection is closed and all iterateLogs have
	// finished. We can't just wait for the done channel here, because the
	// client connection will close and iterateLogs may still be calling
	// it.Next() when the caller calls it.Close(), causing a data race.
	wg.Wait()
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

type writeLogFunc func(log logEntry) error

// Iterate through all logs in the given iterator, calling writeLog for each
// until the iterator is exhausted.
func iterateLogs(it servicelog.Iterator, br *bufio.Reader, cancel <-chan struct{}, now time.Time, writeLog writeLogFunc) error {
	var prevTime time.Time
	var prevService string
	for it.Next(cancel) {
		br.Reset(it)
		for {
			line, err := br.ReadSlice('\n')
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			}
			if len(line) == 0 {
				// No bytes read, we're done with this iteration
				break
			}

			log, ok := parseLog(line)
			if ok {
				// Output normal log
				err := writeLog(log)
				if err != nil {
					return err
				}
				if !now.IsZero() && log.Time.After(now) {
					// Stop if we're past current time
					return nil
				}
				prevTime = log.Time
				prevService = log.Service
			} else if !prevTime.IsZero() {
				// Output partial log with previous timestamp and service name
				err := writeLog(logEntry{prevTime, prevService, string(line)})
				if err != nil {
					return err
				}
			}

			if errors.Is(err, io.EOF) {
				// EOF reached, break after processing line
				break
			}
		}
	}
	return nil
}

// Parse a log entry of the form "2021-05-20T15:39:12.345Z [service] log message"
func parseLog(line []byte) (logEntry, bool) {
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

// Writer that serializes concurrent writes to the underlying writer, and
// calls Flush on the underlying writer after each write.
type followWriter struct {
	w     io.Writer
	mutex *sync.Mutex
}

func (w *followWriter) Write(p []byte) (int, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	n, err := w.w.Write(p)
	flushWriter(w.w) // Flush HTTP response after each line in follow mode
	return n, err
}

func flushWriter(w io.Writer) {
	flusher, ok := w.(interface{ Flush() })
	if ok {
		flusher.Flush()
	}
}
