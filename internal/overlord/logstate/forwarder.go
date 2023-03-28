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
// a single target. A forwarder f should be started in a new goroutine:
//
//	go f.forward()
//
// This starts the main control loop, which will spawn an additional iterate
// goroutine to pull logs from the iterator and send them through a channel.
// f can be stopped by calling f.stop(), which will gracefully terminate the
// forward and iterate goroutines, and flush out all remaining logs.
type logForwarder struct {
	service     string
	target      *plan.LogTarget
	iterator    servicelog.Iterator
	maxMessages int
	delay       time.Duration
	client      logClient

	// Channels for communication
	// stopCh is closed in stop() to tell the forward() method to stop.
	stopCh chan struct{}
	// doneWithIterator is closed in forward() to tell the stop() method that we
	// are done with the iterator, so it's safe to return from stop().
	doneWithIterator chan struct{}
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

	return &logForwarder{
		service:          service,
		target:           target,
		iterator:         iterator,
		maxMessages:      maxMessages,
		delay:            delay,
		client:           client,
		stopCh:           make(chan struct{}),
		doneWithIterator: make(chan struct{}),
	}
}

// forward continually receives logs from the iterator and buffers them. It
// flushes the buffer after a timeout, or when a certain number of messages
// have been written.
// This method will block until the service is stopped, or stop() is called.
// Hence, it should be run in a separate goroutine.
func (f *logForwarder) forward() {
	// Create a timer and immediately stop it, so it starts unset
	flushTimer := time.NewTimer(0)
	stopTimer(flushTimer)

	cancelIterator := make(chan struct{})
	messagesCh := make(chan servicelog.Entry)
	go f.iterate(cancelIterator, messagesCh)

	var buffer []servicelog.Entry
	for {
		select {
		case message := <-messagesCh:
			buffer = append(buffer, message)

			if len(buffer) >= f.maxMessages {
				// Buffer full - send logs to target
				stopTimer(flushTimer)
				f.sendLogs(buffer)
				buffer = buffer[:0]

			} else if len(buffer) == 1 {
				// First write - set the timeout
				stopTimer(flushTimer)
				flushTimer.Reset(f.delay)
			}

		case <-flushTimer.C:
			f.sendLogs(buffer)
			buffer = buffer[:0]

		case <-f.stopCh:
			// Cancel the iterator channel, so it will stop waiting for more data
			close(cancelIterator)

			// Drain remaining messages from parser
			for message := range messagesCh {
				buffer = append(buffer, message)
				if len(buffer) >= f.maxMessages {
					f.sendLogs(buffer)
					buffer = buffer[:0]
				}
			}

			// Once the above for loop returns, the iterate method has finished,
			// so we are done using the iterator.
			close(f.doneWithIterator)
			f.sendLogs(buffer)

			err := f.client.Close()
			if err != nil {
				logger.Noticef("Internal error: closing log client %q -> %q: %s",
					f.service, f.target.Name, err)
			}
			return
		}
	}
}

// iterate continually pulls logs from the iterator/parser, and sends these to
// the forward loop via the messages channel. When the provided cancel channel
// is closed, iterate will pull all remaining logs from the parser before
// exiting.
func (f *logForwarder) iterate(cancel <-chan struct{}, messages chan servicelog.Entry) {
	parser := servicelog.NewParser(f.iterator, parserSize)

	for f.iterator.Next(cancel) {
		for parser.Next() {
			// The forward loop is either selecting on this channel or draining it
			// in the cancel case. So no need for a select here.
			messages <- parser.Entry()
		}
		if err := parser.Err(); err != nil {
			logger.Noticef(
				"Cannot read logs from service %q (target %q): %v",
				f.service, f.target.Name, err)
		}
	}
	close(messages)
}

// stop interrupts the main forward loop, telling the forwarder to shut down.
// Once this method returns, it is safe to re-use the iterator.
func (f *logForwarder) stop() {
	close(f.stopCh)
	// Wait till we are done with the iterator to avoid race conditions
	<-f.doneWithIterator
}

// sendLogs passes the buffered logs to the client, to send to the remote log
// target.
func (f *logForwarder) sendLogs(messages []servicelog.Entry) {
	if len(messages) == 0 {
		return
	}

	err := f.client.Send(messages)
	if err != nil {
		logger.Noticef(
			"Cannot forward logs from service %q to target %q: %v",
			f.service, f.target.Name, err)
	}
}

// stopTimer is a utility method which correctly stops a time.Timer, and drains
// the channel if necessary.
func stopTimer(timer *time.Timer) {
	timer.Stop()
	// Drain timer channel if non-empty
	select {
	case <-timer.C:
	default:
	}
}

// logClient represents a client that communicates with a log target.
type logClient interface {
	Send([]servicelog.Entry) error
	Close() error
}
