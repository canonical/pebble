// Copyright (c) 2026 Canonical Ltd
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

package tracestate

import (
	"context"
	"fmt"
	"time"

	"gopkg.in/tomb.v2"

	"github.com/canonical/pebble/cmd"
	"github.com/canonical/pebble/internals/logger"
	tracepb "github.com/canonical/pebble/internals/otlp/trace/v1"
	"github.com/canonical/pebble/internals/overlord/tracestate/opentelemetry"
	"github.com/canonical/pebble/internals/plan"
)

const (
	bufferTimeout      = 1 * time.Second
	maxBufferedBatches = 100
	// maxQueuedBatches bounds the number of not-yet-processed batches the
	// gatherer will hold before it starts dropping incoming traces.
	maxQueuedBatches = 200

	// These constants control the maximum time allowed for each teardown step.
	timeoutCurrentFlush = 1 * time.Second
	timeoutMainLoop     = 3 * time.Second
	// timeoutFinalFlush is measured from when the gatherer's main loop finishes,
	// NOT from when Stop() is called like timeoutMainLoop.
	timeoutFinalFlush = 2 * time.Second
)

// traceGatherer is responsible for collecting OTLP traces received on behalf
// of a bunch of services, and sending them to its tracesClient.
// One traceGatherer runs per trace target. Its loop() method should be
// run in its own goroutine.
//
// Unlike logs (which are pulled from a per-service ring buffer), traces arrive
// already batched via the local OTLP receiver, so a traceGatherer is fed
// directly through a channel by the TraceManager.
//
// The traceGatherer will "flush" the client:
//   - after a timeout (1s) has passed since the first batch was buffered;
//   - when it is told to shut down.
//
// The client may also flush itself when its internal buffer reaches a
// certain size.
type traceGatherer struct {
	*traceGathererOptions

	targetName string
	// tomb for the main loop
	tomb tomb.Tomb

	client tracesClient
	// Context to pass to client methods
	clientCtx context.Context
	// cancel func for clientCtx - can be used during teardown if required, to
	// ensure the client is not blocking subsequent teardown steps.
	clientCancel context.CancelFunc

	// entryCh receives batches of traces to be added to the client's buffer.
	entryCh chan tracesBatch
}

// traceGathererOptions allows overriding the newClient method and time
// values in testing.
type traceGathererOptions struct {
	bufferTimeout      time.Duration
	maxBufferedBatches int
	timeoutFinalFlush  time.Duration
	queueSize          int
	// method to get a new client
	newClient func(*plan.TraceTarget) (tracesClient, error)
}

// tracesBatch represents a single OTLP export received on behalf of a
// service, queued up ready to be added to a tracesClient's buffer.
type tracesBatch struct {
	service       string
	instanceID    string
	labels        map[string]string
	resourceSpans []*tracepb.ResourceSpans
}

func newtraceGatherer(target *plan.TraceTarget) (*traceGatherer, error) {
	return newtraceGathererInternal(target, &traceGathererOptions{})
}

// newtraceGathererInternal contains the actual creation code for a
// traceGatherer. This function is used in the real implementation, but
// also allows overriding certain configuration values for testing.
func newtraceGathererInternal(target *plan.TraceTarget, options *traceGathererOptions) (*traceGatherer, error) {
	options = fillDefaultOptions(options)
	client, err := options.newClient(target)
	if err != nil {
		return nil, fmt.Errorf("cannot create trace client: %w", err)
	}

	g := &traceGatherer{
		traceGathererOptions: options,

		targetName: target.Name,
		client:     client,
		entryCh:    make(chan tracesBatch, options.queueSize),
	}
	g.clientCtx, g.clientCancel = context.WithCancel(context.Background())
	g.tomb.Go(g.loop)

	return g, nil
}

func fillDefaultOptions(options *traceGathererOptions) *traceGathererOptions {
	if options.bufferTimeout == 0 {
		options.bufferTimeout = bufferTimeout
	}
	if options.maxBufferedBatches == 0 {
		options.maxBufferedBatches = maxBufferedBatches
	}
	if options.timeoutFinalFlush == 0 {
		options.timeoutFinalFlush = timeoutFinalFlush
	}
	if options.queueSize == 0 {
		options.queueSize = maxQueuedBatches
	}
	if options.newClient == nil {
		options.newClient = newTracesClient
	}
	return options
}

// Add enqueues a batch of resource spans received on behalf of the named
// service, to be forwarded to this gatherer's target. If the gatherer's
// internal buffer is full, the batch is dropped and a warning is logged.
func (g *traceGatherer) Add(service, instanceID string, labels map[string]string, resourceSpans []*tracepb.ResourceSpans) {
	batch := tracesBatch{
		service:       service,
		instanceID:    instanceID,
		labels:        labels,
		resourceSpans: resourceSpans,
	}
	select {
	case g.entryCh <- batch:
	case <-g.tomb.Dying():
	default:
		logger.Noticef("Target %q: trace buffer full, dropping batch for service %q", g.targetName, service)
	}
}

// The main control loop for the traceGatherer. loop receives batches on
// entryCh, and adds them to the client's buffer. It also flushes the client
// periodically, and exits when the gatherer's tomb is killed.
func (g *traceGatherer) loop() error {
	flushTimer := newTimer()
	defer flushTimer.Stop()
	// Keep track of number of batches added since last flush
	numAdded := 0

	flushClient := func(ctx context.Context) {
		// Mark timer as unset
		flushTimer.Stop()
		err := g.client.Flush(ctx)
		if err != nil {
			logger.Noticef("Cannot flush traces to target %q: %v", g.targetName, err)
		}
		numAdded = 0
	}

mainLoop:
	for {
		select {
		case <-g.tomb.Dying():
			break mainLoop

		case <-flushTimer.Expired():
			flushClient(g.clientCtx)

		case batch := <-g.entryCh:
			err := g.client.Add(batch.service, batch.instanceID, batch.labels, batch.resourceSpans)
			if err != nil {
				logger.Noticef("Cannot buffer traces for target %q: %v", g.targetName, err)
				continue
			}
			numAdded++
			// Check if buffer is full
			if numAdded >= g.maxBufferedBatches {
				flushClient(g.clientCtx)
				continue
			}
			// Otherwise, set the timeout
			flushTimer.EnsureSet(g.bufferTimeout)
		}
	}

	// Final flush to send any remaining traces buffered in the client.
	// We need to create a new context, as the previous one may have been cancelled.
	ctx, cancel := context.WithTimeout(context.Background(), g.timeoutFinalFlush)
	defer cancel()
	flushClient(ctx)
	return nil
}

// Stop tears down the gatherer and associated resources (client).
// This method will block until gatherer teardown is complete.
func (g *traceGatherer) Stop() {
	// If a flush is in progress when Stop is called, give it a bounded time
	// to complete before cancelling its context.
	time.AfterFunc(timeoutCurrentFlush, g.clientCancel)

	// Kill the main loop once either it finishes on its own, or
	// timeoutMainLoop has passed.
	g.tomb.Kill(nil)
	select {
	case <-g.tomb.Dead():
	case <-time.After(timeoutMainLoop):
		logger.Debugf("gatherer %q: force killing main loop", g.targetName)
	}

	// Wait for final flush in the main loop
	err := g.tomb.Wait()
	if err != nil {
		logger.Noticef("Cannot shut down trace gatherer: %v", err)
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

// tracesClient handles requests to a specific type of trace target. It
// encodes and enriches OTLP trace payloads, and sends them using the
// protocol required by that trace target.
type tracesClient interface {
	// Add adds the given batch of resource spans (received on behalf of
	// the named service) to the client's buffer.
	Add(service, instanceID string, labels map[string]string, resourceSpans []*tracepb.ResourceSpans) error

	// Flush sends buffered traces (if any) to the remote target.
	Flush(context.Context) error
}

func newTracesClient(target *plan.TraceTarget) (tracesClient, error) {
	switch target.Type {
	case plan.OpenTelemetryTraceTarget:
		return opentelemetry.NewClient(&opentelemetry.ClientOptions{
			TargetName: target.Name,
			Location:   target.Location,
			UserAgent:  fmt.Sprintf("%s/%s", cmd.ProgramName, cmd.Version),
		}), nil
	default:
		return nil, fmt.Errorf("unknown type %q for trace target %q", target.Type, target.Name)
	}
}
