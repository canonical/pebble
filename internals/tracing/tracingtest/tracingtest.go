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

package tracingtest

import (
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/canonical/pebble/internals/tracing"
)

// ReadOnlySpan is a recorded span.
type ReadOnlySpan = sdktrace.ReadOnlySpan

// Recorder records the spans created with tracing.Tracer.
type Recorder struct {
	recorder *tracetest.SpanRecorder
	restore  tracing.TracerProvider
}

// NewRecorder installs a global tracer provider that samples and records
// all spans. Call Restore to reinstate the previous tracer provider.
func NewRecorder() *Recorder {
	r := &Recorder{
		recorder: tracetest.NewSpanRecorder(),
		restore:  otel.GetTracerProvider(),
	}
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(r.recorder)))
	return r
}

// Restore reinstates the tracer provider that was installed before the
// recorder.
func (r *Recorder) Restore() {
	otel.SetTracerProvider(r.restore)
}

// Ended returns the spans that have ended, in the order they ended.
func (r *Recorder) Ended() []ReadOnlySpan {
	return r.recorder.Ended()
}

// Reset forgets the spans recorded so far.
func (r *Recorder) Reset() {
	r.recorder.Reset()
}
