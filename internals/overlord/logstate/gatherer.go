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
	"io"
	"sync"
	"time"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/servicelog"
)

// logGatherer is responsible for collecting service logs from a forwarder,
// writing them to its internal logBuffer, and sending the request via its
// logClient.
// One logGatherer will run per log target. Its loop() method should be run
// in its own goroutine, while the addLog() method can be invoked in a
// separate goroutine by a logForwarder.
// The logGatherer will "flush" and send a request to the client:
//   - on a regular cadence (e.g. every 1 second)
//   - when the buffer reaches a certain size
//   - when it is told to shut down.
type logGatherer struct {
	target     *plan.LogTarget
	tickPeriod time.Duration
	writeCh    chan struct{}
	cancel     chan struct{}

	bufferLock sync.Mutex
	buffer     logBuffer
	client     logClient
}

func newLogGatherer(target *plan.LogTarget) *logGatherer {
	tickPeriod := 1 * time.Second

	return &logGatherer{
		target:     target,
		tickPeriod: tickPeriod,
		// writeCh should be buffered, so that addLog can send write notifications,
		// even when the control loop is not ready to receive.
		writeCh: make(chan struct{}, 1),
		cancel:  make(chan struct{}),
		buffer:  newLogBuffer(target),
		client:  newLogClient(target),
	}
}

func (g *logGatherer) loop() {
	ticker := time.NewTicker(g.tickPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Timeout - flush
			g.flush(true)

		case <-g.writeCh:
			// Got a write - check if buffer is full
			g.flush(false)

		case <-g.cancel:
			// Gatherer has been stopped - flush any remaining logs
			g.flush(true)
			return
		}
	}
}

func (g *logGatherer) addLog(entry servicelog.Entry) {
	g.bufferLock.Lock()
	g.buffer.Write(entry)
	g.bufferLock.Unlock()

	// Try to notify the control loop of a new write to the buffer.
	// If there is already a notification waiting, no need to notify again - just
	// drop it.
	select {
	case g.writeCh <- struct{}{}:
	default:
	}
}

// flush obtains a lock on the buffer, prepares the request, sends to the
// remote server, and empties the buffer.
// If force is false, flush will check first if the buffer is full, and only
// flush if it is full.
func (g *logGatherer) flush(force bool) {
	g.bufferLock.Lock()
	defer g.bufferLock.Unlock()

	if g.buffer.IsEmpty() {
		// No point doing anything
		return
	}
	if !force && !g.buffer.IsFull() {
		// Not ready to flush yet
		return
	}

	req, err := g.buffer.Request()
	if err != nil {
		logger.Noticef("couldn't generate request for target %q: %v", g.target.Name, err)
		return
	}

	err = g.client.Send(req)
	if err != nil {
		logger.Noticef("couldn't send logs to target %q: %v", g.target.Name, err)
		// TODO: early return here? should we reset buffer if send fails?
	}

	g.buffer.Reset()
}

// stop closes the cancel channel, thereby terminating the main loop.
func (g *logGatherer) stop() {
	close(g.cancel)
}

// logBuffer is an interface encapsulating format-specific buffering of log
// messages. E.g. a logBuffer for Loki would encode the log messages in the
// JSON format expected by Loki.
// A logBuffer's methods may not be concurrency-safe. Callers should protect
// the logBuffer using a sync.Mutex.
type logBuffer interface {
	IsEmpty() bool
	IsFull() bool

	// Write encodes the provided log message and adds it to the buffer.
	Write(servicelog.Entry) // TODO: return error?

	// Request returns an io.Reader which can be used as the body of a request
	// to the remote log target.
	Request() (io.Reader, error)

	// Reset empties the buffer.
	Reset()
}

func newLogBuffer(target *plan.LogTarget) logBuffer {
	// TODO: check target.Type and return the corresponding logBuffer
	return nil
}

// logClient is implemented by a client to a specific type of log target.
// It sends requests using the protocol preferred by that log target.
type logClient interface {
	Send(io.Reader) error
}

func newLogClient(target *plan.LogTarget) logClient {
	// TODO: check target.Type and return the corresponding logClient
	return nil
}
