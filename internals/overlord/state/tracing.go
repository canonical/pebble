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

package state

import (
	"context"
	"slices"

	"github.com/canonical/pebble/internals/tracing"
)

// Span attribute names used for changes and tasks. The full attribute key is
// prefixed with the program name (see tracing.AttrKey).
const (
	attrChangeID      = "change.id"
	attrChangeKind    = "change.kind"
	attrChangeSummary = "change.summary"
	attrChangeStatus  = "change.status"
	attrChangeResumed = "change.resumed"
	attrTaskID        = "task.id"
	attrTaskKind      = "task.kind"
	attrTaskSummary   = "task.summary"
	attrTaskHandler   = "task.handler"
	attrTaskStatus    = "task.status"
	attrRetryAfter    = "task.retry.after"
	attrReason        = "task.reason"
)

// startSpan starts the span covering the lifetime of the change, from when
// it is spawned until it becomes ready. The span is a child of the span
// carried by ctx, if any.
func (c *Change) startSpan(ctx context.Context) {
	_, c.span = tracing.Tracer().Start(ctx, "change "+c.kind,
		tracing.WithTimestamp(c.spawnTime),
		tracing.WithAttributes(
			tracing.AttrKey(attrChangeID).String(c.id),
			tracing.AttrKey(attrChangeKind).String(c.kind),
			tracing.AttrKey(attrChangeSummary).String(c.summary),
		),
	)
	c.spanContext = c.span.SpanContext()
}

// resumeSpan starts a new span for a change that was loaded from the
// persisted state before it became ready (for example, after a restart). The
// new span is parented to the change's original span, so that the tasks run
// after resuming remain part of the same tracing.
func (c *Change) resumeSpan() {
	ctx := context.Background()
	if c.spanContext.IsValid() {
		ctx = tracing.ContextWithRemoteSpanContext(ctx, c.spanContext)
	}
	_, c.span = tracing.Tracer().Start(ctx, "change "+c.kind,
		tracing.WithAttributes(
			tracing.AttrKey(attrChangeID).String(c.id),
			tracing.AttrKey(attrChangeKind).String(c.kind),
			tracing.AttrKey(attrChangeSummary).String(c.summary),
			tracing.AttrKey(attrChangeResumed).Bool(true),
		),
	)
	if !c.spanContext.IsValid() {
		c.spanContext = c.span.SpanContext()
	}
}

// endSpan ends the change's span, recording its final status. It is a no-op
// if the span has already ended or was never started.
func (c *Change) endSpan() {
	if c.span == nil {
		return
	}
	span := c.span
	c.span = nil

	status := c.Status()
	span.SetAttributes(tracing.AttrKey(attrChangeStatus).String(status.String()))
	if status == ErrorStatus {
		if err := c.Err(); err != nil {
			span.RecordError(err)
			span.SetStatus(tracing.StatusError, err.Error())
		} else {
			span.SetStatus(tracing.StatusError, "")
		}
	}
	var opts []tracing.SpanEndOption
	if !c.readyTime.IsZero() {
		opts = append(opts, tracing.WithTimestamp(c.readyTime))
	}
	span.End(opts...)
}

// traceContext returns a context carrying the change's span, for use as the
// parent of task spans.
func (c *Change) traceContext() context.Context {
	ctx := context.Background()
	if c.span != nil {
		return tracing.ContextWithSpan(ctx, c.span)
	}
	if c.spanContext.IsValid() {
		return tracing.ContextWithRemoteSpanContext(ctx, c.spanContext)
	}
	return ctx
}

// startSpan starts a span for a single run of one of the task's handlers.
// The handler is one of "do", "undo" or "cleanup". Until the span is ended
// with endTaskSpan, modifications to the task are traced as caused by it.
//
// Do and undo spans are children of the change's span. Cleanups run after
// the change is ready, possibly much later (on the next ensure pass), so a
// cleanup span is instead a new root, linked to the change's span, to avoid
// extending the change's tracing.
func (t *Task) startSpan(handler string) (context.Context, tracing.Span) {
	ctx := context.Background()
	attrs := []tracing.Attribute{
		tracing.AttrKey(attrTaskID).String(t.id),
		tracing.AttrKey(attrTaskKind).String(t.kind),
		tracing.AttrKey(attrTaskSummary).String(t.summary),
		tracing.AttrKey(attrTaskHandler).String(handler),
	}
	var opts []tracing.SpanStartOption
	if chg := t.Change(); chg != nil {
		attrs = append(attrs, tracing.AttrKey(attrChangeID).String(chg.id), tracing.AttrKey(attrChangeKind).String(chg.kind))
		if handler == "cleanup" {
			if chg.spanContext.IsValid() {
				opts = append(opts, tracing.WithLinks(tracing.Link{SpanContext: chg.spanContext}))
			}
		} else {
			ctx = chg.traceContext()
		}
	}
	opts = append(opts, tracing.WithAttributes(attrs...))
	ctx, span := tracing.Tracer().Start(ctx, handler+" "+t.kind, opts...)
	t.runSpan = span.SpanContext()
	return ctx, span
}

// endTaskSpan records the outcome of a task handler run on its span and ends
// the span. It must be called with the state lock held.
func endTaskSpan(span tracing.Span, t *Task, err error, opts ...tracing.SpanEndOption) {
	switch x := err.(type) {
	case nil:
	case *Retry:
		span.AddEvent("retry", tracing.WithAttributes(
			tracing.AttrKey(attrRetryAfter).String(x.After.String()),
			tracing.AttrKey(attrReason).String(x.Reason),
		))
	case *Wait:
		span.AddEvent("wait", tracing.WithAttributes(
			tracing.AttrKey(attrReason).String(x.Reason),
		))
	default:
		span.RecordError(err)
		span.SetStatus(tracing.StatusError, err.Error())
	}
	span.SetAttributes(tracing.AttrKey(attrTaskStatus).String(t.Status().String()))
	span.End(opts...)
	t.runSpan = tracing.SpanContext{}
}

// traceCauses holds the spans on whose behalf the state has been modified.
type traceCauses struct {
	// explicit causes were added with State.AddTraceContext.
	explicit []tracing.SpanContext
	// implicit causes were added by modifying tasks and changes.
	implicit []tracing.SpanContext
}

// maxRetainedTraceCauses is the capacity above which the traceCauses slices
// are dropped on reset, rather than kept for reuse, so that an unusually
// large locked section doesn't hold on to memory indefinitely.
const maxRetainedTraceCauses = 128

// reset empties the causes. The slices' backing arrays are kept for reuse
// by the next locked section, unless they've grown too large.
func (tc *traceCauses) reset() {
	tc.explicit = truncateTraceCauses(tc.explicit)
	tc.implicit = truncateTraceCauses(tc.implicit)
}

func truncateTraceCauses(causes []tracing.SpanContext) []tracing.SpanContext {
	if cap(causes) > maxRetainedTraceCauses {
		return nil
	}
	// Zero the old elements so the trace states they reference can be
	// garbage collected.
	clear(causes)
	return causes[:0]
}

func (tc *traceCauses) add(sc tracing.SpanContext, explicit bool) {
	if !sc.IsValid() {
		return
	}
	for i, existing := range tc.implicit {
		if existing.Equal(sc) {
			if !explicit {
				return
			}
			// Promote to explicit.
			tc.implicit = append(tc.implicit[:i], tc.implicit[i+1:]...)
			break
		}
	}
	for _, existing := range tc.explicit {
		if existing.Equal(sc) {
			return
		}
	}
	if explicit {
		tc.explicit = append(tc.explicit, sc)
	} else {
		tc.implicit = append(tc.implicit, sc)
	}
}

// AddTraceContext records that the state is being modified on behalf of the
// span carried by ctx, so that the resulting checkpoint is traced as part of
// that span's tracing. Spans added this way take precedence over those of the
// tasks and changes modified. It must be called with the state lock held.
func (s *State) AddTraceContext(ctx context.Context) {
	s.reading()
	s.traceCauses.add(tracing.SpanContextFromContext(ctx), true)
}

// startCheckpointSpan starts the span for checkpointing the state. The span
// is a child of the first span that caused the checkpoint, preferring those
// added with AddTraceContext, and is linked to the others.
func (s *State) startCheckpointSpan() tracing.Span {
	causes := append(slices.Clone(s.traceCauses.explicit), s.traceCauses.implicit...)
	ctx := context.Background()
	var links []tracing.Link
	for i, sc := range causes {
		if i == 0 {
			ctx = tracing.ContextWithSpanContext(ctx, sc)
		} else {
			links = append(links, tracing.Link{SpanContext: sc})
		}
	}
	_, span := tracing.Tracer().Start(ctx, "state checkpoint", tracing.WithLinks(links...))
	return span
}

// writing marks the state as modified on behalf of the task (see
// Task.traceSpanContext).
func (t *Task) writing() {
	t.state.writing()
	t.state.traceCauses.add(t.traceSpanContext(), false)
}

// traceSpanContext returns the span that modifications to the task are made
// on behalf of: its running handler's span, or otherwise its change's span.
func (t *Task) traceSpanContext() tracing.SpanContext {
	if t.runSpan.IsValid() {
		return t.runSpan
	}
	if chg := t.state.changes[t.change]; chg != nil {
		return chg.spanContext
	}
	return tracing.SpanContext{}
}

// writing marks the state as modified on behalf of the change's span.
func (c *Change) writing() {
	c.state.writing()
	c.state.traceCauses.add(c.spanContext, false)
}

// traceCleaned traces marking tasks clean in an ensure pass, when they have
// no cleanup handler. This can be long after their changes became ready, so
// it's traced in its own trace, linked to the changes, and the resulting
// checkpoint is part of that trace rather than the changes' traces. It must
// be called with the state lock held.
func traceCleaned(st *State, tasks []*Task) {
	var links []tracing.Link
	seen := make(map[string]bool)
	for _, t := range tasks {
		chg := st.changes[t.change]
		if chg == nil || seen[chg.id] || !chg.spanContext.IsValid() {
			continue
		}
		seen[chg.id] = true
		links = append(links, tracing.Link{SpanContext: chg.spanContext})
	}
	ctx, span := tracing.Tracer().Start(context.Background(), "clean tasks",
		tracing.WithLinks(links...),
		tracing.WithAttributes(tracing.AttrKey("task.cleaned").Int(len(tasks))),
	)
	st.AddTraceContext(ctx)
	span.End()
}
