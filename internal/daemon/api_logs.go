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
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

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

	// This is not exactly text/plain, but it's not pure JSON either.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

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

	//	followLogs(w, req.Context().Done(), itsByName)
}

// TODO(benhoyt) - not working yet
func followLogs(w io.Writer, done <-chan struct{}, itsByName map[string]servicelog.Iterator) {
	writeMutex := &sync.Mutex{}
	for name, it := range itsByName {
		go func(name string, it servicelog.Iterator) {
			for {
				select {
				case <-it.More():
					writeMutex.Lock()
					err := writeLog(w, name, it)
					writeMutex.Unlock()
					if err != nil {
						fmt.Fprintf(w, "\ncannot write log: %v", err)
						return
					}
				case <-done:
					log.Printf("TODO: connection closed (%q)", name)
					return
				}
			}
		}(name, it)
	}
	<-done
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
	return err
}
