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
	"fmt"
	"time"

	"github.com/canonical/pebble/internal/logger"
	"github.com/canonical/pebble/internal/plan"
	"github.com/canonical/pebble/internal/servicelog"
)

const (
	parserSize = 4 * 1024

	maxBufferedLogs = 100
)

var flushDelay = 1 * time.Second

// logForwarder is responsible for forwarding logs from a single service to
// a single target.
type logForwarder struct {
	service string
	target  *plan.LogTarget

	iterator   servicelog.Iterator
	parser     *servicelog.Parser
	iteratorCh chan struct{}
	stopCh     chan chan struct{}

	messages    []servicelog.Entry
	maxMessages int

	delay      time.Duration
	flushTimer *time.Timer
	timerSet   bool
	timeoutCh  chan struct{}

	client logClient
}

func newLogForwarder(
	service string, target *plan.LogTarget, iterator servicelog.Iterator,
) (*logForwarder, error) {

	var client logClient
	var err error

	switch target.Type {
	case plan.LokiTarget:
		client = newLokiClient(target.Location)

	case plan.SyslogTarget:
		client, err = newSyslogClient(target.Location)

	default:
		return nil, fmt.Errorf(
			"unknown log target type %q", target.Type)
	}
	if err != nil {
		return nil, err
	}

	return newLogForwarderInternal(service, target, iterator, client, maxBufferedLogs, flushDelay), nil
}

// newLogForwarderInternal is shared by the real and test code.
func newLogForwarderInternal(
	service string, target *plan.LogTarget, iterator servicelog.Iterator,
	client logClient, maxMessages int, delay time.Duration,
) *logForwarder {

	iteratorCh := make(chan struct{})
	timeoutCh := make(chan struct{}, 1)
	// Create timer. Here we set the timer duration to a large value and
	// immediately stop it, to ensure the timer starts off unset.
	flushTimer := time.AfterFunc(time.Hour, func() {
		timeoutCh <- struct{}{}
		// If we are waiting on iterator.Next, cancel this
		select {
		case iteratorCh <- struct{}{}:
		default:
		}
	})
	flushTimer.Stop()

	return &logForwarder{
		service:     service,
		target:      target,
		iterator:    iterator,
		parser:      servicelog.NewParser(iterator, parserSize),
		iteratorCh:  iteratorCh,
		stopCh:      make(chan chan struct{}, 1),
		maxMessages: maxMessages, // TODO: make this value configurable in plan
		delay:       delay,       // TODO: make this value configurable in plan
		flushTimer:  flushTimer,
		timerSet:    false,
		timeoutCh:   timeoutCh,
		client:      client,
	}
}

// forward continually pulls logs from the service buffer and forwards them
// to the remote log target.
// This method will block until the service is stopped, or stop() is called.
// Hence, it should be run in a separate goroutine.
func (f *logForwarder) forward() {
	for {
		select {
		// f.stop() called
		case release := <-f.stopCh:
			// Pull all remaining logs from parser/iterator
			for f.parser.Next() {
				if err := f.parser.Err(); err != nil {
					logger.Noticef(
						"Forwarding logs to target %q: error reading logs for service %q: %v",
						f.target.Name, f.service, err)
					continue
				}

				f.messages = append(f.messages, f.parser.Entry())
			}
			// Done with iterator - release call to stop()
			close(release)
			f.close()
			return

		// Timeout on buffered messages
		case <-f.timeoutCh:
			f.flushBuffer()

		default:
			// continue loop
		}

		if len(f.messages) >= f.maxMessages {
			f.flushBuffer()
		}

		// Check if parser has buffered logs
		if f.parser.Next() {
			if err := f.parser.Err(); err != nil {
				logger.Noticef(
					"Forwarding logs to target %q: error reading logs for service %q: %v",
					f.target.Name, f.service, err)
				continue
			}

			f.messages = append(f.messages, f.parser.Entry())

			// Set deadline
			if !f.timerSet {
				f.flushTimer.Reset(f.delay)
				f.timerSet = true
			}

			continue
		}

		// Parser has no buffered logs. Wait for iterator to receive more data
		f.iterator.Next(f.iteratorCh)
	}
}

// stop stops the forwarding of logs.
func (f *logForwarder) stop() {
	// Create a "release" channel - the other goroutine will close this to signal
	// it has finished, then we can exit the stop() call.
	release := make(chan struct{})
	f.stopCh <- release

	// Cancel iterator.Next call - wait for release channel to be closed
	for {
		select {
		case f.iteratorCh <- struct{}{}:
		case <-release:
			return
		}
	}
}

// close flushes the buffer, closes the client and cleans up.
func (f *logForwarder) close() {
	f.flushBuffer()

	err := f.client.Close()
	if err != nil {
		logger.Noticef("Internal error: closing log client %q -> %q: %s",
			f.service, f.target.Name, err)
	}
}

// flushBuffer sends the buffered messages to the client, then resets the buffer.
func (f *logForwarder) flushBuffer() {
	err := f.client.Send(f.messages)
	if err != nil {
		logger.Noticef(
			"Forwarding logs to target %q: cannot send logs for service %q: %v",
			f.target.Name, f.service, err)
	}

	f.messages = f.messages[:0]

	// Reset timer
	f.flushTimer.Stop()
	f.timerSet = false
	// Drain timer channel
	select {
	case <-f.flushTimer.C:
	default:
	}
}

// logClient represents a client that communicates with a log target.
type logClient interface {
	Send([]servicelog.Entry) error
	Close() error
}
