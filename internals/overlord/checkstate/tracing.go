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

package checkstate

import (
	"context"
	"strings"

	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/tracing"
)

// startCheckSpan starts the span for a single run of a check.
//
// A check run on request, where parent carries a span (for example, from a
// "POST /v1/checks/refresh" request), is a child of that span. A periodic run
// is instead a new root span, so that each run is its own short trace rather
// than the check task's trace growing indefinitely. In both cases, the span
// is linked to the check task's span carried by ctx, if any.
func startCheckSpan(ctx, parent context.Context, config *plan.Check) (context.Context, tracing.Span) {
	attrs := []tracing.Attribute{
		tracing.AttrKey("check.name").String(config.Name),
		tracing.AttrKey("check.type").String(strings.ToLower(checkType(config))),
	}
	if config.Level != plan.UnsetLevel {
		attrs = append(attrs, tracing.AttrKey("check.level").String(string(config.Level)))
	}
	opts := []tracing.SpanStartOption{tracing.WithAttributes(attrs...)}

	taskSpan := tracing.SpanContextFromContext(ctx)
	var parentSpan tracing.SpanContext
	if parent != nil {
		parentSpan = tracing.SpanContextFromContext(parent)
	}
	if taskSpan.IsValid() && !taskSpan.Equal(parentSpan) {
		opts = append(opts, tracing.WithLinks(tracing.Link{SpanContext: taskSpan}))
	}
	if parentSpan.IsValid() {
		ctx = tracing.ContextWithSpanContext(ctx, parentSpan)
	} else {
		opts = append(opts, tracing.WithNewRoot())
	}
	return tracing.Tracer().Start(ctx, "check "+config.Name, opts...)
}
