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

// Package tracingtest provides helpers for testing code instrumented with
// the tracing package.
//
// Instrumented code creates spans with tracing.Tracer, which uses the global
// tracer provider. Outside of a daemon with tracing enabled, that provider is
// a no-op: spans aren't recorded, and their span contexts are invalid, so
// trace propagation can't be observed either. A [Recorder] replaces the
// global provider with one that samples every span and records them, so that
// tests can check which spans were created, their attributes, status,
// parents and links, and the trace context propagated to other processes.
//
// A typical test installs a recorder, exercises the code under test, and
// then inspects the ended spans:
//
//	recorder := tracingtest.NewRecorder()
//	defer recorder.Restore()
//
//	// ... exercise the code under test ...
//
//	for _, span := range recorder.Ended() {
//		// ... check span.Name(), span.Attributes(), span.Parent() ...
//	}
//
// As the tracer provider is global, a test using a recorder must not run in
// parallel with other tests that create spans, and must restore the previous
// provider when it's done.
//
// This package exists, rather than these helpers being part of the tracing
// package, so that the OpenTelemetry SDK's test support is only built into
// tests, and so that tests, like the rest of Pebble, don't need to import
// OpenTelemetry themselves (see the tracing package documentation). It is
// only for use in tests.
package tracingtest
