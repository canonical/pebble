// Copyright (c) 2023 Canonical Ltd
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

package logstate

import (
	"sync"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/servicelog"
)

// logForwarder is responsible for pulling logs from a service's ringbuffer,
// and distributing each log message to its logGatherers. Its gatherers field
// holds a reference to the gatherer for each log target that the service is
// sending logs to.
// One logForwarder will run per service. Its forward() method should be run
// in its own goroutine.
type logForwarder struct {
	serviceName string

	mu        sync.Mutex // mutex for gatherers
	gatherers []*logGatherer

	cancel chan struct{}
}

func newLogForwarder(serviceName string) *logForwarder {
	f := &logForwarder{
		serviceName: serviceName,
		cancel:      make(chan struct{}),
	}

	return f
}

func (f *logForwarder) forward(buffer *servicelog.RingBuffer) {
	iterator := buffer.TailIterator()
	// TODO: don't use the parser, just pull/write bytes from iterator
	parser := servicelog.NewParser(iterator, 1024 /* TODO*/)

	for iterator.Next(f.cancel) {
		for parser.Next() {
			entry := parser.Entry()
			f.mu.Lock()
			gatherers := f.gatherers
			f.mu.Unlock()
			for _, c := range gatherers {
				c.addLog(entry)
			}
		}
		if err := parser.Err(); err != nil {
			logger.Noticef("Cannot read logs from service %q: %v", f.serviceName, err)
		}
	}
}

func (f *logForwarder) stop() {
	close(f.cancel)
}
