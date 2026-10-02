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

package servstate

import (
	"context"

	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/tracing"
)

// Span attribute names used for services. The full attribute key is prefixed
// with the program name (see tracing.AttrKey).
const (
	attrServiceName  = "service.name"
	attrServiceOn    = "service.on"
	attrAction       = "service.action"
	attrBackoffNum   = "service.backoff.num"
	attrBackoffDelay = "service.backoff.delay"
	attrKillDelay    = "service.kill-delay"
	attrRestartType  = "restart.type"
)

// A service's lifecycle is traced by two kinds of span.
//
// While a start or stop task is running, the events of the service's state
// machine (the process starting, SIGTERM and SIGKILL being sent, the process
// exiting) are recorded on that task's span, which serviceData.taskSpan
// borrows for the duration of the task handler.
//
// Everything else happens outside any task: a running service exits and is
// restarted after a backoff, or a check failure causes it to be restarted or
// the daemon to shut down. Each of these is traced as its own short root
// span, the "lifecycle span", which is linked to the span that started the
// process (a start task, or the previous restart) and, for check failures, to
// the check run. The span is owned by the service and ends when the service
// has been started again, stopped, or the terminal action has been requested.
// A restart is bounded by the kill delay, fail delay and backoff limit, so
// the span is short; a span per service lifetime would be unbounded.

// setTaskSpan sets or clears (with nil) the span of the task that is
// currently starting or stopping the service.
func (s *serviceData) setTaskSpan(span tracing.Span) {
	s.manager.servicesLock.Lock()
	defer s.manager.servicesLock.Unlock()
	s.taskSpan = span
}

// span returns the span on which the service's lifecycle events are recorded:
// the lifecycle span if one is active, otherwise the span of the task
// currently starting or stopping the service, otherwise a no-op span. It must
// be called with servicesLock held.
func (s *serviceData) span() tracing.Span {
	if s.lifecycleSpan != nil {
		return s.lifecycleSpan
	}
	if s.taskSpan != nil {
		return s.taskSpan
	}
	return tracing.SpanFromContext(context.Background())
}

// startLifecycleSpan starts the lifecycle span for a restart or exit of the
// service (see the comment above). The span is a new root, linked to the span
// that started the process and to the span carried by cause (for example, the
// failing check run), if any. It must be called with servicesLock held.
func (s *serviceData) startLifecycleSpan(cause context.Context, name, onType string, action plan.ServiceAction, attrs ...tracing.Attribute) {
	if s.lifecycleSpan != nil {
		// Shouldn't happen, but don't leak the previous span if it does.
		s.lifecycleSpan.End()
	}
	attrs = append(attrs,
		tracing.AttrKey(attrServiceName).String(s.config.Name),
		tracing.AttrKey(attrServiceOn).String(onType),
		tracing.AttrKey(attrAction).String(string(action)),
	)
	opts := []tracing.SpanStartOption{tracing.WithNewRoot(), tracing.WithAttributes(attrs...)}
	var links []tracing.Link
	if s.startSpanContext.IsValid() {
		links = append(links, tracing.Link{SpanContext: s.startSpanContext})
	}
	if sc := tracing.SpanContextFromContext(cause); sc.IsValid() && !sc.Equal(s.startSpanContext) {
		links = append(links, tracing.Link{SpanContext: sc})
	}
	if len(links) > 0 {
		opts = append(opts, tracing.WithLinks(links...))
	}
	_, s.lifecycleSpan = tracing.Tracer().Start(context.Background(), name+" "+s.config.Name, opts...)
}

// lifecycleContext returns a context carrying the lifecycle span, or a
// background context if there is none. It must be called with servicesLock
// held.
func (s *serviceData) lifecycleContext() context.Context {
	ctx := context.Background()
	if s.lifecycleSpan != nil {
		ctx = tracing.ContextWithSpan(ctx, s.lifecycleSpan)
	}
	return ctx
}

// endLifecycleSpan ends the lifecycle span, if any, recording err on it. It
// must be called with servicesLock held.
func (s *serviceData) endLifecycleSpan(err error) {
	if s.lifecycleSpan == nil {
		return
	}
	tracing.EndSpan(s.lifecycleSpan, err)
	s.lifecycleSpan = nil
}
