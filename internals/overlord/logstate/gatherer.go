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
	"context"
	"fmt"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/servicelog"
)

const (
	parserSize    = 4 * 1024
	bufferTimeout = 1 * time.Second

	// These constants control the maximum time allowed for each teardown step.
	timeoutCurrentFlush = 1 * time.Second
	timeoutPullers      = 2 * time.Second
	timeoutMainLoop     = 3 * time.Second
	// timeoutFinalFlush is measured from when the gatherer's main loop finishes,
	// NOT from when Stop() is called like the other constants.
	timeoutFinalFlush = 2 * time.Second
)

// logGatherer is responsible for collecting service logs from a bunch of
// services, and sending them to its logClient.
// One logGatherer will run per log target. Its loop() method should be run
// in its own goroutine.
// A logGatherer will spawn a separate logPuller for each service it collects
// logs from. Each logPuller will run in a separate goroutine, and send logs to
// the logGatherer via a shared channel.
// The logGatherer will "flush" the client:
//   - after a timeout (1s) has passed since the first log was written;
//   - when it is told to shut down.
//
// The client may also flush itself when its internal buffer reaches a certain
// size.
// Calling the Stop() method will tear down the logGatherer and all of its
// associated logPullers. Stop() can be called from an outside goroutine.
type logGatherer struct {
	logGathererArgs

	targetName string
	// tomb for the main loop
	tomb tomb.Tomb

	client logClient
	// Context to pass to client methods
	clientCtx context.Context
	// cancel func for clientCtx - can be used during teardown if required, to
	// ensure the client is not blocking subsequent teardown steps.
	clientCancel context.CancelFunc

	pullers *pullerGroup
	// All pullers send logs on this channel, received by main loop
	entryCh chan servicelog.Entry
}

// logGathererArgs allows overriding the newLogClient method and time values
// in testing.
type logGathererArgs struct {
	bufferTimeout     time.Duration
	timeoutFinalFlush time.Duration
	// method to get a new client
	newClient func(*plan.LogTarget) (logClient, error)
}

func newLogGatherer(target *plan.LogTarget) (*logGatherer, error) {
	return newLogGathererInternal(target, logGathererArgs{})
}

// newLogGathererInternal contains the actual creation code for a logGatherer.
// This function is used in the real implementation, but also allows overriding
// certain configuration values for testing.
func newLogGathererInternal(target *plan.LogTarget, args logGathererArgs) (*logGatherer, error) {
	args = fillDefaultArgs(args)
	client, err := args.newClient(target)
	if err != nil {
		return nil, fmt.Errorf("cannot create log client: %w", err)
	}

	g := &logGatherer{
		logGathererArgs: args,

		targetName: target.Name,
		client:     client,
		entryCh:    make(chan servicelog.Entry),
		pullers:    newPullerGroup(target.Name),
	}
	g.clientCtx, g.clientCancel = context.WithCancel(context.Background())
	g.tomb.Go(g.loop)
	g.tomb.Go(g.pullers.tomb.Wait)

	return g, nil
}

func fillDefaultArgs(args logGathererArgs) logGathererArgs {
	if args.bufferTimeout == 0 {
		args.bufferTimeout = bufferTimeout
	}
	if args.timeoutFinalFlush == 0 {
		args.timeoutFinalFlush = timeoutFinalFlush
	}
	if args.newClient == nil {
		args.newClient = newLogClient
	}
	return args
}

// PlanChanged is called by the LogManager when the plan is changed, if this
// gatherer's target exists in the new plan.
func (g *logGatherer) PlanChanged(pl *plan.Plan, buffers map[string]*servicelog.RingBuffer) {
	// Remove old pullers
	for _, svcName := range g.pullers.Services() {
		svc, svcExists := pl.Services[svcName]
		if !svcExists {
			g.pullers.Remove(svcName)
			continue
		}

		tgt := pl.LogTargets[g.targetName]
		if !svc.LogsTo(tgt) {
			g.pullers.Remove(svcName)
		}
	}

	// Add new pullers
	for _, service := range pl.Services {
		target := pl.LogTargets[g.targetName]
		if !service.LogsTo(target) {
			continue
		}

		buffer, bufferExists := buffers[service.Name]
		if !bufferExists {
			// We don't yet have a reference to the service's ring buffer
			// Need to wait until ServiceStarted
			continue
		}

		g.pullers.Add(service.Name, buffer, g.entryCh)
	}
}

// ServiceStarted is called by the LogManager on the start of a service which
// logs to this gatherer's target.
func (g *logGatherer) ServiceStarted(service *plan.Service, buffer *servicelog.RingBuffer) {
	g.pullers.Add(service.Name, buffer, g.entryCh)
}

// The main control loop for the logGatherer. loop receives logs from the
// pullers on entryCh, and writes them to the client. It also flushes the
// client periodically, and exits when the gatherer's tomb is killed.
func (g *logGatherer) loop() error {
	timer := newTimer()
	defer timer.Stop()

mainLoop:
	for {
		select {
		case <-g.tomb.Dying():
			break mainLoop

		case <-timer.Expired():
			// Mark timer as unset
			timer.Stop()
			err := g.client.Flush(g.clientCtx)
			if err != nil {
				logger.Noticef("Cannot flush logs to target %q: %v", g.targetName, err)
			}

		case entry := <-g.entryCh:
			err := g.client.Write(g.clientCtx, entry)
			if err != nil {
				logger.Noticef("Cannot write logs to target %q: %v", g.targetName, err)
			}
			timer.EnsureSet(g.bufferTimeout)
		}
	}

	// Final flush to send any remaining logs buffered in the client
	// We need to create a new context, as the previous one may have been cancelled.
	ctx, cancel := context.WithTimeout(context.Background(), g.timeoutFinalFlush)
	defer cancel()
	err := g.client.Flush(ctx)
	if err != nil {
		logger.Noticef("Cannot flush logs to target %q: %v", g.targetName, err)
	}
	return nil
}

// Stop tears down the gatherer and associated resources (pullers, client).
// This method will block until gatherer teardown is complete.
//
// The teardown process has several steps:
//   - If the main loop is in the middle of a flush when we call Stop, this
//     will block the pullers from sending logs to the gatherer. Hence, wait
//     for the current flush to complete.
//   - Wait for the pullers to pull the final logs from the iterator.
//   - Kill the main loop.
//   - Flush out any final logs buffered in the client.
func (g *logGatherer) Stop() {
	// Wait up to timeoutCurrentFlush for the current flush to complete (if any)
	time.AfterFunc(timeoutCurrentFlush, g.clientCancel)

	// Wait up to timeoutPullers for the pullers to pull the final logs from the
	// iterator and send to the main loop.
	time.AfterFunc(timeoutPullers, func() {
		logger.Debugf("gatherer %q: force killing log pullers", g.targetName)
		g.pullers.KillAll()
	})

	// Kill the main loop once either:
	// - all the pullers are done
	// - timeoutMainLoop has passed
	g.pullers.tomb.Kill(nil)
	select {
	case <-g.pullers.Done():
		logger.Debugf("gatherer %q: pullers have finished", g.targetName)
	case <-time.After(timeoutMainLoop):
		logger.Debugf("gatherer %q: force killing main loop", g.targetName)
	}

	g.tomb.Kill(nil)
	// Wait for final flush in the main loop
	err := g.tomb.Wait()
	if err != nil {
		logger.Noticef("Cannot shut down gatherer: %v", err)
	}
}

// timer wraps time.Timer and provides a better API.
type timer struct {
	timer *time.Timer
	set   bool
}

func newTimer() timer {
	t := timer{
		timer: time.NewTimer(1 * time.Hour),
	}
	t.Stop()
	return t
}

func (t *timer) Expired() <-chan time.Time {
	return t.timer.C
}

func (t *timer) Stop() {
	t.timer.Stop()
	t.set = false
	// Drain timer channel
	select {
	case <-t.timer.C:
	default:
	}
}

func (t *timer) EnsureSet(timeout time.Duration) {
	if t.set {
		return
	}

	t.timer.Reset(timeout)
	t.set = true
}

// logClient handles requests to a specific type of log target. It encodes
// log messages in the required format, and sends the messages using the
// protocol required by that log target.
// For example, a logClient for Loki would encode the log messages in the
// JSON format expected by Loki, and send them over HTTP(S).
//
// logClient implementations have some freedom about the semantics of these
// methods. For a buffering client (e.g. HTTP):
//   - Write could add the log to the client's internal buffer, calling Flush
//     when this buffer reaches capacity.
//   - Flush would prepare and send a request with the buffered logs.
//
// For a non-buffering client (e.g. TCP), Write could serialise the log
// directly to the open connection, while Flush would be a no-op.
type logClient interface {
	// Write adds the given log entry to the client. Depending on the
	// implementation of the client, this may send the log to the remote target,
	// or simply add the log to an internal buffer, flushing that buffer when
	// required.
	Write(context.Context, servicelog.Entry) error

	// Flush sends buffered logs (if any) to the remote target. For clients which
	// don't buffer logs, Flush should be a no-op.
	Flush(context.Context) error
}

func newLogClient(target *plan.LogTarget) (logClient, error) {
	switch target.Type {
	//case plan.LokiTarget: TODO
	//case plan.SyslogTarget: TODO
	default:
		return nil, fmt.Errorf("unknown type %q for log target %q", target.Type, target.Name)
	}
}
