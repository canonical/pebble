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
	"os"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/canonical/pebble/internals/logger"
	"github.com/canonical/pebble/internals/overlord/logstate/loki"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/servicelog"
)

const (
	parserSize         = 4 * 1024
	bufferTimeout      = 1 * time.Second
	maxBufferedEntries = 100

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
	*logGathererOptions

	target *plan.LogTarget
	// tomb for the main loop
	tomb tomb.Tomb

	client logClient
	// Context to pass to client methods
	clientCtx context.Context
	// cancel func for clientCtx - can be used during teardown if required, to
	// ensure the client is not blocking subsequent teardown steps.
	clientCancel context.CancelFunc

	// Channels used to notify the main loop to update the client
	setLabels chan svcWithLabels

	pullers *pullerGroup
	// All pullers send logs on this channel, received by main loop
	entryCh chan servicelog.Entry
}

// logGathererOptions allows overriding the newLogClient method and time values
// in testing.
type logGathererOptions struct {
	bufferTimeout       time.Duration
	maxBufferedEntries  int
	timeoutCurrentFlush time.Duration
	timeoutFinalFlush   time.Duration
	// method to get a new client
	newClient func(*plan.LogTarget) (logClient, error)
}

func newLogGatherer(target *plan.LogTarget) (*logGatherer, error) {
	return newLogGathererInternal(target, &logGathererOptions{})
}

// newLogGathererInternal contains the actual creation code for a logGatherer.
// This function is used in the real implementation, but also allows overriding
// certain configuration values for testing.
func newLogGathererInternal(target *plan.LogTarget, options *logGathererOptions) (*logGatherer, error) {
	options = fillDefaultOptions(options)
	client, err := options.newClient(target)
	if err != nil {
		return nil, fmt.Errorf("cannot create log client: %w", err)
	}

	g := &logGatherer{
		logGathererOptions: options,

		target:    target,
		client:    client,
		setLabels: make(chan svcWithLabels),
		entryCh:   make(chan servicelog.Entry),
		pullers:   newPullerGroup(target.Name),
	}
	g.clientCtx, g.clientCancel = context.WithCancel(context.Background())
	g.tomb.Go(g.loop)
	g.tomb.Go(g.pullers.tomb.Wait)

	return g, nil
}

func fillDefaultOptions(options *logGathererOptions) *logGathererOptions {
	if options.bufferTimeout == 0 {
		options.bufferTimeout = bufferTimeout
	}
	if options.maxBufferedEntries == 0 {
		options.maxBufferedEntries = maxBufferedEntries
	}
	if options.timeoutCurrentFlush == 0 {
		options.timeoutCurrentFlush = timeoutCurrentFlush
	}
	if options.timeoutFinalFlush == 0 {
		options.timeoutFinalFlush = timeoutFinalFlush
	}
	if options.newClient == nil {
		options.newClient = newLogClient
	}
	return options
}

// PlanChanged is called by the LogManager when the plan is changed, if this
// gatherer's target exists in the new plan.
// The labels map should not be modified while this method is running.
func (g *logGatherer) PlanChanged(pl *plan.Plan, serviceData map[string]*ServiceData) {
	// Remove old pullers
	for _, svcName := range g.pullers.Services() {
		svc, svcExists := pl.Services[svcName]
		if svcExists {
			tgt := pl.LogTargets[g.target.Name]
			if svc.LogsTo(tgt) {
				// We're still collecting logs from this service, so don't remove it.
				continue
			}
		}

		// Service no longer forwarding to this log target (or it was removed from
		// the plan). Remove it from the gatherer.
		g.pullers.Remove(svcName)
		select {
		case g.setLabels <- svcWithLabels{svcName, nil}:
		case <-g.tomb.Dying():
			return
		}
	}

	// Add new pullers
	for _, service := range pl.Services {
		target := pl.LogTargets[g.target.Name]
		if !service.LogsTo(target) {
			continue
		}

		svcData, svcStarted := serviceData[service.Name]
		if !svcStarted {
			// We don't yet have a reference to the service's ring buffer
			// Need to wait until ServiceStarted
			continue
		}

		labels := evaluateLabels(g.target.Labels, serviceData[service.Name].Env)
		select {
		case g.setLabels <- svcWithLabels{service.Name, labels}:
		case <-g.tomb.Dying():
			return
		}

		g.pullers.Add(service.Name, svcData.Buffer, g.entryCh)
	}
}

// EnvChanged is called when a service is (re)started and its environment has
// changed.
func (g *logGatherer) EnvChanged(serviceName string, env map[string]string) {
	labels := evaluateLabels(g.target.Labels, env)
	select {
	case g.setLabels <- svcWithLabels{serviceName, labels}:
	case <-g.tomb.Dying():
		return
	}
}

// BufferChanged is called when a service is (re)started with a new ringbuffer.
func (g *logGatherer) BufferChanged(serviceName string, buffer *servicelog.RingBuffer) {
	g.pullers.Add(serviceName, buffer, g.entryCh)
}

// evaluateLabels interprets the labels defined in the plan, substituting any
// $env_vars with the corresponding value in the service's environment.
func evaluateLabels(rawLabels, env map[string]string) map[string]string {
	substitute := func(k string) string {
		if v, ok := env[k]; ok {
			return v
		}
		// Undefined variables default to "", just like Bash
		return ""
	}

	labels := make(map[string]string, len(rawLabels))
	for key, rawLabel := range rawLabels {
		labels[key] = os.Expand(rawLabel, substitute)
	}
	return labels
}

// The main control loop for the logGatherer. loop receives logs from the
// pullers on entryCh, and writes them to the client. It also flushes the
// client periodically, and exits when the gatherer's tomb is killed.
func (g *logGatherer) loop() error {
	flushTimer := newTimer()
	defer flushTimer.Stop()
	// Keep track of number of logs written since last flush
	numWritten := 0

	flushClient := func(ctx context.Context) {
		// Mark timer as unset
		flushTimer.Stop()
		err := g.client.Flush(ctx)
		if err != nil {
			logger.Noticef("Cannot flush logs to target %q: %v", g.target.Name, err)
		}
		numWritten = 0
	}

mainLoop:
	for {
		select {
		case <-g.tomb.Dying():
			break mainLoop

		case <-flushTimer.Expired():
			flushClient(g.clientCtx)

		case args := <-g.setLabels:
			flushClient(g.clientCtx)
			g.client.SetLabels(args.service, args.labels)

		case entry := <-g.entryCh:
			err := g.client.Add(entry)
			if err != nil {
				logger.Noticef("Cannot write logs to target %q: %v", g.target.Name, err)
				continue
			}
			numWritten++
			// Check if buffer is full
			if numWritten >= g.maxBufferedEntries {
				flushClient(g.clientCtx)
				continue
			}
			// Otherwise, set the timeout
			flushTimer.EnsureSet(g.bufferTimeout)
		}
	}

	// Final flush to send any remaining logs buffered in the client
	// We need to create a new context, as the previous one may have been cancelled.
	ctx, cancel := context.WithTimeout(context.Background(), g.timeoutFinalFlush)
	defer cancel()
	flushClient(ctx)
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
	time.AfterFunc(g.timeoutCurrentFlush, g.clientCancel)

	// Wait up to timeoutPullers for the pullers to pull the final logs from the
	// iterator and send to the main loop.
	time.AfterFunc(timeoutPullers, func() {
		logger.Debugf("gatherer %q: force killing log pullers", g.target.Name)
		g.pullers.KillAll()
	})

	// Kill the main loop once either:
	// - all the pullers are done
	// - timeoutMainLoop has passed
	g.pullers.tomb.Kill(nil)
	select {
	case <-g.pullers.Done():
		logger.Debugf("gatherer %q: pullers have finished", g.target.Name)
	case <-time.After(timeoutMainLoop):
		logger.Debugf("gatherer %q: force killing main loop", g.target.Name)
	}

	g.tomb.Kill(nil)
	// Wait for final flush in the main loop
	err := g.tomb.Wait()
	if err != nil {
		logger.Noticef("Cannot shut down gatherer: %v", err)
	}
}

type svcWithLabels struct {
	service string
	labels  map[string]string
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
type logClient interface {
	// Add adds the given log entry to the client's buffer.
	Add(servicelog.Entry) error

	// Flush sends buffered logs (if any) to the remote target.
	Flush(context.Context) error

	// SetLabels sets the log labels for the given services, or releases
	// previously allocated label resources if the labels parameter is nil.
	SetLabels(serviceName string, labels map[string]string)
}

func newLogClient(target *plan.LogTarget) (logClient, error) {
	switch target.Type {
	case plan.LokiTarget:
		return loki.NewClient(target), nil
	//case plan.SyslogTarget: TODO
	default:
		return nil, fmt.Errorf("unknown type %q for log target %q", target.Type, target.Name)
	}
}
