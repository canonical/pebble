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

package planstate_test

import (
	"context"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/planstate"
	"github.com/canonical/pebble/internals/plan"
	"github.com/canonical/pebble/internals/tracing"
	"github.com/canonical/pebble/internals/tracing/tracingtest"
)

type tracingSuite struct {
	// ps provides the layers directory and layer helpers. It isn't embedded
	// so that its tests aren't run again as part of this suite.
	ps       planSuite
	planMgr  *planstate.PlanManager
	recorder *tracingtest.Recorder

	// listenerCtxs holds the contexts the plan change listener was called
	// with, and listenerPlans the plans.
	listenerCtxs  []context.Context
	listenerPlans []*plan.Plan
}

var _ = Suite(&tracingSuite{})

func (s *tracingSuite) SetUpTest(c *C) {
	s.ps.SetUpTest(c)
	s.recorder = tracingtest.NewRecorder()
	s.listenerCtxs = nil
	s.listenerPlans = nil

	var err error
	s.planMgr, err = planstate.NewManager(s.ps.layersDir)
	c.Assert(err, IsNil)
	s.planMgr.AddChangeListener(func(ctx context.Context, p *plan.Plan) {
		s.listenerCtxs = append(s.listenerCtxs, ctx)
		s.listenerPlans = append(s.listenerPlans, p)
	})
}

func (s *tracingSuite) TearDownTest(c *C) {
	s.recorder.Restore()
}

// startSpan starts a span to act as the span of the operation (such as the
// daemon's startup or an API request) that changes the plan.
func (s *tracingSuite) startSpan(name string) (context.Context, tracing.Span) {
	return tracing.Tracer().Start(context.Background(), name)
}

// endedSpan returns the ended span with the given name, of which there must
// be exactly one.
func (s *tracingSuite) endedSpan(c *C, name string) tracingtest.ReadOnlySpan {
	var found tracingtest.ReadOnlySpan
	for _, span := range s.recorder.Ended() {
		if span.Name() == name {
			c.Assert(found, IsNil, Commentf("more than one ended span named %q", name))
			found = span
		}
	}
	c.Assert(found, NotNil, Commentf("no ended span named %q", name))
	return found
}

func spanAttrs(span tracingtest.ReadOnlySpan) map[string]tracing.AttributeValue {
	attrs := make(map[string]tracing.AttributeValue)
	for _, kv := range span.Attributes() {
		attrs[string(kv.Key)] = kv.Value
	}
	return attrs
}

// checkListenerCalled checks that the plan change listener was called once
// since the last check, with the given plan and a context carrying span.
func (s *tracingSuite) checkListenerCalled(c *C, span tracingtest.ReadOnlySpan, p *plan.Plan) {
	c.Assert(s.listenerCtxs, HasLen, 1)
	c.Check(tracing.SpanContextFromContext(s.listenerCtxs[0]).SpanID(), Equals, span.SpanContext().SpanID())
	c.Check(tracing.SpanContextFromContext(s.listenerCtxs[0]).TraceID(), Equals, span.SpanContext().TraceID())
	c.Check(s.listenerPlans[0], Equals, p)
	s.listenerCtxs = nil
	s.listenerPlans = nil
}

func (s *tracingSuite) TestLoadSpan(c *C) {
	s.ps.writeLayer(c, string(reindent(`
		services:
			svc1:
				override: replace
				command: echo svc1
			svc2:
				override: replace
				command: echo svc2
		checks:
			chk1:
				override: replace
				exec:
					command: true
		log-targets:
			tgt1:
				override: replace
				type: loki
				location: http://localhost:3100
	`)))
	s.ps.writeLayer(c, string(reindent(`
		services:
			svc3:
				override: replace
				command: echo svc3
	`)))

	startupCtx, startupSpan := s.startSpan("startup")
	defer startupSpan.End()
	err := s.planMgr.Load(startupCtx, nil)
	c.Assert(err, IsNil)

	span := s.endedSpan(c, "plan load")
	c.Check(span.Parent().SpanID(), Equals, startupSpan.SpanContext().SpanID())
	c.Check(span.SpanContext().TraceID(), Equals, startupSpan.SpanContext().TraceID())
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)
	attrs := spanAttrs(span)
	c.Check(attrs["pebble.plan.layers"].AsInt64(), Equals, int64(2))
	c.Check(attrs["pebble.plan.services"].AsInt64(), Equals, int64(3))
	c.Check(attrs["pebble.plan.checks"].AsInt64(), Equals, int64(1))
	c.Check(attrs["pebble.plan.log-targets"].AsInt64(), Equals, int64(1))
	s.checkListenerCalled(c, span, s.planMgr.Plan())

	// Loading again has no effect, and isn't traced.
	s.recorder.Reset()
	err = s.planMgr.Load(startupCtx, nil)
	c.Assert(err, IsNil)
	c.Check(s.recorder.Ended(), HasLen, 0)
	c.Check(s.listenerCtxs, HasLen, 0)
}

func (s *tracingSuite) TestLoadSpanEmptyPlan(c *C) {
	err := s.planMgr.Load(context.Background(), nil)
	c.Assert(err, IsNil)

	span := s.endedSpan(c, "plan load")
	c.Check(span.Parent().IsValid(), Equals, false)
	attrs := spanAttrs(span)
	c.Check(attrs["pebble.plan.layers"].AsInt64(), Equals, int64(0))
	c.Check(attrs["pebble.plan.services"].AsInt64(), Equals, int64(0))
	c.Check(attrs["pebble.plan.checks"].AsInt64(), Equals, int64(0))
	c.Check(attrs["pebble.plan.log-targets"].AsInt64(), Equals, int64(0))
	s.checkListenerCalled(c, span, s.planMgr.Plan())
}

func (s *tracingSuite) TestLoadSpanError(c *C) {
	s.ps.writeLayer(c, "services: [not a map]\n")

	err := s.planMgr.Load(context.Background(), nil)
	c.Assert(err, NotNil)

	span := s.endedSpan(c, "plan load")
	c.Check(span.Status().Code, Equals, tracing.StatusError)
	c.Check(span.Status().Description, Equals, err.Error())
	_, ok := spanAttrs(span)["pebble.plan.layers"]
	c.Check(ok, Equals, false)
	c.Check(s.listenerCtxs, HasLen, 0)
}

func (s *tracingSuite) TestAppendLayerSpan(c *C) {
	requestCtx, requestSpan := s.startSpan("request")
	defer requestSpan.End()

	layer := s.ps.parseLayer(c, 0, "label1", `
services:
    svc1:
        override: replace
        command: echo svc1
checks:
    chk1:
        override: replace
        exec:
            command: true
`)
	err := s.planMgr.AppendLayer(requestCtx, layer, false)
	c.Assert(err, IsNil)

	span := s.endedSpan(c, "plan add layer")
	c.Check(span.Parent().SpanID(), Equals, requestSpan.SpanContext().SpanID())
	c.Check(span.SpanContext().TraceID(), Equals, requestSpan.SpanContext().TraceID())
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)
	attrs := spanAttrs(span)
	c.Check(attrs["pebble.layer.label"].AsString(), Equals, "label1")
	c.Check(attrs["pebble.layer.combine"].AsBool(), Equals, false)
	c.Check(attrs["pebble.layer.inner"].AsBool(), Equals, false)
	c.Check(attrs["pebble.layer.order"].AsInt64(), Equals, int64(1000))
	c.Check(attrs["pebble.plan.layers"].AsInt64(), Equals, int64(1))
	c.Check(attrs["pebble.plan.services"].AsInt64(), Equals, int64(1))
	c.Check(attrs["pebble.plan.checks"].AsInt64(), Equals, int64(1))
	c.Check(attrs["pebble.plan.log-targets"].AsInt64(), Equals, int64(0))
	s.checkListenerCalled(c, span, s.planMgr.Plan())

	// Appending a layer with the same label fails, which the span records,
	// and the listener isn't called.
	s.recorder.Reset()
	layer = s.ps.parseLayer(c, 0, "label1", "")
	err = s.planMgr.AppendLayer(requestCtx, layer, false)
	c.Assert(err, FitsTypeOf, &planstate.LabelExists{})

	span = s.endedSpan(c, "plan add layer")
	c.Check(span.Parent().SpanID(), Equals, requestSpan.SpanContext().SpanID())
	c.Check(span.Status().Code, Equals, tracing.StatusError)
	c.Check(span.Status().Description, Equals, err.Error())
	attrs = spanAttrs(span)
	c.Check(attrs["pebble.layer.label"].AsString(), Equals, "label1")
	c.Check(attrs["pebble.layer.combine"].AsBool(), Equals, false)
	_, ok := attrs["pebble.layer.order"]
	c.Check(ok, Equals, false)
	_, ok = attrs["pebble.plan.layers"]
	c.Check(ok, Equals, false)
	c.Check(s.listenerCtxs, HasLen, 0)

	// The inner flag is recorded as given.
	s.recorder.Reset()
	layer = s.ps.parseLayer(c, 0, "label2", "")
	err = s.planMgr.AppendLayer(requestCtx, layer, true)
	c.Assert(err, IsNil)
	attrs = spanAttrs(s.endedSpan(c, "plan add layer"))
	c.Check(attrs["pebble.layer.label"].AsString(), Equals, "label2")
	c.Check(attrs["pebble.layer.inner"].AsBool(), Equals, true)
	c.Check(attrs["pebble.layer.order"].AsInt64(), Equals, int64(2000))
	c.Check(attrs["pebble.plan.layers"].AsInt64(), Equals, int64(2))
}

func (s *tracingSuite) TestCombineLayerSpan(c *C) {
	requestCtx, requestSpan := s.startSpan("request")
	defer requestSpan.End()

	// Combining into a label that doesn't exist appends the layer.
	layer := s.ps.parseLayer(c, 0, "label1", `
services:
    svc1:
        override: replace
        command: echo svc1
`)
	err := s.planMgr.CombineLayer(requestCtx, layer, false)
	c.Assert(err, IsNil)

	span := s.endedSpan(c, "plan add layer")
	c.Check(span.Parent().SpanID(), Equals, requestSpan.SpanContext().SpanID())
	c.Check(span.SpanContext().TraceID(), Equals, requestSpan.SpanContext().TraceID())
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)
	attrs := spanAttrs(span)
	c.Check(attrs["pebble.layer.label"].AsString(), Equals, "label1")
	c.Check(attrs["pebble.layer.combine"].AsBool(), Equals, true)
	c.Check(attrs["pebble.layer.inner"].AsBool(), Equals, false)
	c.Check(attrs["pebble.layer.order"].AsInt64(), Equals, int64(1000))
	c.Check(attrs["pebble.plan.layers"].AsInt64(), Equals, int64(1))
	c.Check(attrs["pebble.plan.services"].AsInt64(), Equals, int64(1))
	s.checkListenerCalled(c, span, s.planMgr.Plan())

	// Combining into the existing layer keeps its order.
	s.recorder.Reset()
	layer = s.ps.parseLayer(c, 0, "label1", `
services:
    svc2:
        override: replace
        command: echo svc2
`)
	err = s.planMgr.CombineLayer(requestCtx, layer, false)
	c.Assert(err, IsNil)

	span = s.endedSpan(c, "plan add layer")
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)
	attrs = spanAttrs(span)
	c.Check(attrs["pebble.layer.label"].AsString(), Equals, "label1")
	c.Check(attrs["pebble.layer.combine"].AsBool(), Equals, true)
	c.Check(attrs["pebble.layer.order"].AsInt64(), Equals, int64(1000))
	c.Check(attrs["pebble.plan.layers"].AsInt64(), Equals, int64(1))
	c.Check(attrs["pebble.plan.services"].AsInt64(), Equals, int64(2))
	s.checkListenerCalled(c, span, s.planMgr.Plan())

	// A combined layer that makes the plan invalid fails, which the span
	// records, and the listener isn't called.
	s.recorder.Reset()
	layer = s.ps.parseLayer(c, 0, "label1", `
services:
    svc3:
        override: merge
`)
	err = s.planMgr.CombineLayer(requestCtx, layer, false)
	c.Assert(err, ErrorMatches, `plan must define "command" for service "svc3"`)

	span = s.endedSpan(c, "plan add layer")
	c.Check(span.Status().Code, Equals, tracing.StatusError)
	c.Check(span.Status().Description, Equals, err.Error())
	attrs = spanAttrs(span)
	c.Check(attrs["pebble.layer.combine"].AsBool(), Equals, true)
	_, ok := attrs["pebble.layer.order"]
	c.Check(ok, Equals, false)
	c.Check(s.listenerCtxs, HasLen, 0)
}

func (s *tracingSuite) TestSetServiceArgsSpan(c *C) {
	layer := s.ps.parseLayer(c, 0, "label1", `
services:
    svc1:
        override: replace
        command: foo
    svc2:
        override: replace
        command: bar
`)
	err := s.planMgr.AppendLayer(context.Background(), layer, false)
	c.Assert(err, IsNil)
	s.recorder.Reset()
	s.listenerCtxs = nil
	s.listenerPlans = nil

	requestCtx, requestSpan := s.startSpan("run")
	defer requestSpan.End()
	err = s.planMgr.SetServiceArgs(requestCtx, map[string][]string{
		"svc1": {"-abc", "--xyz"},
		"svc2": {"--bar"},
	})
	c.Assert(err, IsNil)

	span := s.endedSpan(c, "plan set service args")
	c.Check(span.Parent().SpanID(), Equals, requestSpan.SpanContext().SpanID())
	c.Check(span.SpanContext().TraceID(), Equals, requestSpan.SpanContext().TraceID())
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)
	c.Check(spanAttrs(span)["pebble.plan.services"].AsInt64(), Equals, int64(2))
	s.checkListenerCalled(c, span, s.planMgr.Plan())

	// Arguments for a service not in the plan fail, which the span records,
	// and the listener isn't called.
	s.recorder.Reset()
	err = s.planMgr.SetServiceArgs(requestCtx, map[string][]string{
		"svc3": {"--baz"},
	})
	c.Assert(err, ErrorMatches, `service "svc3" not found in plan`)

	span = s.endedSpan(c, "plan set service args")
	c.Check(span.Status().Code, Equals, tracing.StatusError)
	c.Check(span.Status().Description, Equals, err.Error())
	c.Check(spanAttrs(span)["pebble.plan.services"].AsInt64(), Equals, int64(1))
	c.Check(s.listenerCtxs, HasLen, 0)
}
