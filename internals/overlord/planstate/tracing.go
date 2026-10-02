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

package planstate

import (
	"context"

	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/tracing"
)

// Span attribute names used for the plan. The full attribute key is prefixed
// with the program name (see tracing.AttrKey).
const (
	attrLayers       = "plan.layers"
	attrServices     = "plan.services"
	attrChecks       = "plan.checks"
	attrLogTargets   = "plan.log-targets"
	attrLayerLabel   = "layer.label"
	attrLayerCombine = "layer.combine"
	attrLayerInner   = "layer.inner"
	attrLayerOrder   = "layer.order"
)

// planAttrs returns attributes describing the size of the plan.
func planAttrs(p *plan.Plan) []tracing.Attribute {
	return []tracing.Attribute{
		tracing.AttrKey(attrLayers).Int(len(p.Layers)),
		tracing.AttrKey(attrServices).Int(len(p.Services)),
		tracing.AttrKey(attrChecks).Int(len(p.Checks)),
		tracing.AttrKey(attrLogTargets).Int(len(p.LogTargets)),
	}
}

// startLayerSpan starts the span for adding a layer to the plan, as a child
// of the span carried by ctx.
func startLayerSpan(ctx context.Context, layer *plan.Layer, combine, inner bool) (context.Context, tracing.Span) {
	return tracing.Tracer().Start(ctx, "plan add layer", tracing.WithAttributes(
		tracing.AttrKey(attrLayerLabel).String(layer.Label),
		tracing.AttrKey(attrLayerCombine).Bool(combine),
		tracing.AttrKey(attrLayerInner).Bool(inner),
	))
}

// endLayerSpan records the outcome of adding a layer, and ends the span.
func endLayerSpan(span tracing.Span, newPlan *plan.Plan, layer *plan.Layer, err error) {
	if newPlan != nil {
		span.SetAttributes(tracing.AttrKey(attrLayerOrder).Int(layer.Order))
		span.SetAttributes(planAttrs(newPlan)...)
	}
	tracing.EndSpan(span, err)
}
