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

package servstate_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/canonical/pebble/internals/overlord/restart"
	"github.com/canonical/pebble/internals/overlord/servstate"
	"github.com/canonical/pebble/internals/overlord/state"
	"github.com/canonical/pebble/internals/tracing"
	"github.com/canonical/pebble/internals/tracing/tracingtest"
)

// The tracing tests run on the S suite so that they can use its service
// manager setup and helpers; each installs a span recorder for its duration.

// startRecorder installs a span recorder for the rest of the test.
func (s *S) startRecorder() *tracingtest.Recorder {
	recorder := tracingtest.NewRecorder()
	s.AddCleanup(recorder.Restore)
	return recorder
}

// waitForSpan waits for a span with the given name and service name
// attribute to end, and returns it. Spans end asynchronously with respect to
// the changes and service states the tests wait on, so this polls.
func waitForSpan(c *C, recorder *tracingtest.Recorder, name, serviceName string) tracingtest.ReadOnlySpan {
	for start := time.Now(); time.Since(start) < 5*time.Second; time.Sleep(time.Millisecond) {
		for _, span := range recorder.Ended() {
			if span.Name() == name && spanAttrs(span)[pebbleAttr("service.name")].AsString() == serviceName {
				return span
			}
		}
	}
	c.Fatalf("timed out waiting for span %q for service %q to end", name, serviceName)
	return nil
}

// pebbleAttr returns the full key of a Pebble-specific attribute.
func pebbleAttr(name string) string {
	return string(tracing.AttrKey(name))
}

func spanAttrs(span tracingtest.ReadOnlySpan) map[string]tracing.AttributeValue {
	attrs := make(map[string]tracing.AttributeValue)
	for _, kv := range span.Attributes() {
		attrs[string(kv.Key)] = kv.Value
	}
	return attrs
}

func eventNames(span tracingtest.ReadOnlySpan) []string {
	var names []string
	for _, event := range span.Events() {
		names = append(names, event.Name)
	}
	return names
}

// eventAttrs returns the attributes of the (first) event with the given name,
// failing the test if there is none.
func eventAttrs(c *C, span tracingtest.ReadOnlySpan, name string) map[string]tracing.AttributeValue {
	for _, event := range span.Events() {
		if event.Name != name {
			continue
		}
		attrs := make(map[string]tracing.AttributeValue)
		for _, kv := range event.Attributes {
			attrs[string(kv.Key)] = kv.Value
		}
		return attrs
	}
	c.Fatalf("span %q has no event %q (events: %v)", span.Name(), name, eventNames(span))
	return nil
}

func traceParent(span tracingtest.ReadOnlySpan) string {
	return tracing.FormatTraceParent(span.SpanContext())
}

// traceEnvLayer returns a layer with a service that appends the trace
// context variables in its environment to the given file each time it runs,
// then runs the rest of the given command.
func traceEnvLayer(name, envPath, rest string) string {
	return fmt.Sprintf(`
services:
    %s:
        override: replace
        command: /bin/sh -c "echo TRACEPARENT=$TRACEPARENT TRACESTATE=$TRACESTATE >> %s; {{.NotifyDoneCheck}}; %s"
`, name, envPath, rest)
}

// readTraceEnv returns the lines the service of traceEnvLayer wrote.
func readTraceEnv(c *C, envPath string) []string {
	data, err := os.ReadFile(envPath)
	c.Assert(err, IsNil)
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func (s *S) TestTracingStartTaskSpan(c *C) {
	recorder := s.startRecorder()
	s.newServiceManager(c)
	envPath := filepath.Join(c.MkDir(), "env")
	s.planAddLayer(c, traceEnvLayer("test1", envPath, "sleep 10"))
	s.planChanged(c)

	chg := s.startServices(c, [][]string{{"test1"}})
	s.st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("Error: %v", chg.Err()))
	s.st.Unlock()
	s.waitForDoneCheck(c, "test1")

	span := waitForSpan(c, recorder, "do start", "test1")
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)
	attrs := spanAttrs(span)
	c.Check(attrs["process.executable.name"].AsString(), Equals, "sh")
	c.Check(attrs["process.pid"].AsInt64(), Equals, int64(s.manager.RunningCmds()["test1"].Process.Pid))
	_, hasExitCode := attrs["process.exit.code"]
	c.Check(hasExitCode, Equals, false)
	c.Check(eventNames(span), DeepEquals, []string{"process started", "okay"})

	// The service continues the start task's trace.
	c.Check(readTraceEnv(c, envPath), DeepEquals, []string{
		"TRACEPARENT=" + traceParent(span) + " TRACESTATE=",
	})
}

func (s *S) TestTracingDaemonTraceEnvNotInherited(c *C) {
	recorder := s.startRecorder()
	// The daemon's own trace context doesn't describe the service's parent.
	s.Setenv("TRACEPARENT", "00-11111111111111111111111111111111-2222222222222222-01")
	s.Setenv("TRACESTATE", "vendor=daemon")

	s.newServiceManager(c)
	envPath := filepath.Join(c.MkDir(), "env")
	s.planAddLayer(c, traceEnvLayer("test1", envPath, "sleep 10"))
	s.planChanged(c)

	s.startServices(c, [][]string{{"test1"}})
	s.waitForDoneCheck(c, "test1")

	span := waitForSpan(c, recorder, "do start", "test1")
	c.Check(readTraceEnv(c, envPath), DeepEquals, []string{
		"TRACEPARENT=" + traceParent(span) + " TRACESTATE=",
	})
}

func (s *S) TestTracingConfiguredTraceEnvWins(c *C) {
	s.startRecorder()
	s.newServiceManager(c)
	envPath := filepath.Join(c.MkDir(), "env")
	configured := "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	s.planAddLayer(c, traceEnvLayer("test1", envPath, "sleep 10")+fmt.Sprintf(`
        environment:
            TRACEPARENT: %s
`, configured))
	s.planChanged(c)

	s.startServices(c, [][]string{{"test1"}})
	s.waitForDoneCheck(c, "test1")

	c.Check(readTraceEnv(c, envPath), DeepEquals, []string{
		"TRACEPARENT=" + configured + " TRACESTATE=",
	})
}

func (s *S) TestTracingStopTaskSpan(c *C) {
	recorder := s.startRecorder()
	s.newServiceManager(c)
	s.planAddLayer(c, `
services:
    test1:
        override: replace
        command: sleep 10
`)
	s.planChanged(c)

	s.startServices(c, [][]string{{"test1"}})
	s.waitUntilService(c, "test1", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusActive
	})
	chg := s.stopServices(c, [][]string{{"test1"}})
	s.st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("Error: %v", chg.Err()))
	s.st.Unlock()

	span := waitForSpan(c, recorder, "do stop", "test1")
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)
	c.Check(eventNames(span), DeepEquals, []string{"sigterm", "process exited"})
	c.Check(eventAttrs(c, span, "sigterm")[pebbleAttr("service.kill-delay")].AsString(), Equals, shortKillDelay.String())
	c.Check(eventAttrs(c, span, "process exited")["process.exit.code"].AsInt64(), Equals, int64(128+15)) // SIGTERM
}

func (s *S) TestTracingStopTaskSpanSigkill(c *C) {
	recorder := s.startRecorder()
	s.newServiceManager(c)
	// The process ignores SIGTERM, so it has to be killed.
	s.planAddLayer(c, `
services:
    test1:
        override: replace
        command: /bin/sh -c "trap '' TERM; exec sleep 10"
        kill-delay: 150ms
`)
	s.planChanged(c)

	s.startServices(c, [][]string{{"test1"}})
	s.waitUntilService(c, "test1", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusActive
	})
	chg := s.stopServices(c, [][]string{{"test1"}})
	s.st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("Error: %v", chg.Err()))
	s.st.Unlock()

	span := waitForSpan(c, recorder, "do stop", "test1")
	c.Check(eventNames(span), DeepEquals, []string{"sigterm", "sigkill", "process exited"})
	c.Check(eventAttrs(c, span, "sigterm")[pebbleAttr("service.kill-delay")].AsString(), Equals, "150ms")
	c.Check(eventAttrs(c, span, "process exited")["process.exit.code"].AsInt64(), Equals, int64(128+9)) // SIGKILL
}

func (s *S) TestTracingRestartOnFailure(c *C) {
	recorder := s.startRecorder()
	s.newServiceManager(c)
	envPath := filepath.Join(c.MkDir(), "env")
	// The service exits (after the okay delay) with failure, so it's
	// restarted after a short backoff.
	s.planAddLayer(c, traceEnvLayer("test1", envPath, "sleep 0.15; exit 7")+`
        backoff-delay: 50ms
        on-failure: restart
`)
	s.planChanged(c)

	s.startServices(c, [][]string{{"test1"}})
	s.waitForDoneCheck(c, "test1")
	// Wait for the restarted process to run.
	s.waitForDoneCheck(c, "test1")

	startSpan := waitForSpan(c, recorder, "do start", "test1")
	span := waitForSpan(c, recorder, "service restart test1", "test1")

	// The restart is a new trace, linked to the start task that started the
	// process that exited.
	c.Check(span.Parent().IsValid(), Equals, false)
	c.Check(span.SpanContext().TraceID(), Not(Equals), startSpan.SpanContext().TraceID())
	c.Assert(span.Links(), HasLen, 1)
	c.Check(span.Links()[0].SpanContext.SpanID(), Equals, startSpan.SpanContext().SpanID())
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)

	attrs := spanAttrs(span)
	c.Check(attrs[pebbleAttr("service.on")].AsString(), Equals, "on-failure")
	c.Check(attrs[pebbleAttr("service.action")].AsString(), Equals, "restart")
	c.Check(attrs["process.exit.code"].AsInt64(), Equals, int64(7))
	// The restarted process is recorded on the restart span.
	c.Check(attrs["process.executable.name"].AsString(), Equals, "sh")
	c.Check(attrs["process.pid"].AsInt64(), Not(Equals), int64(0))
	c.Check(attrs["process.pid"].AsInt64(), Not(Equals), spanAttrs(startSpan)["process.pid"].AsInt64())

	c.Check(eventNames(span), DeepEquals, []string{"backoff", "process started"})
	backoff := eventAttrs(c, span, "backoff")
	c.Check(backoff[pebbleAttr("service.backoff.num")].AsInt64(), Equals, int64(1))
	delay, err := time.ParseDuration(backoff[pebbleAttr("service.backoff.delay")].AsString())
	c.Assert(err, IsNil)
	c.Check(delay >= 50*time.Millisecond, Equals, true, Commentf("delay: %v", delay))

	// The first process continued the start task's trace, the restarted
	// process continues the restart's trace. (The service keeps exiting and
	// being restarted, so there may be further lines by now.)
	lines := readTraceEnv(c, envPath)
	c.Assert(len(lines) >= 2, Equals, true, Commentf("lines: %q", lines))
	c.Check(lines[:2], DeepEquals, []string{
		"TRACEPARENT=" + traceParent(startSpan) + " TRACESTATE=",
		"TRACEPARENT=" + traceParent(span) + " TRACESTATE=",
	})
}

func (s *S) TestTracingExitOnFailureShutdown(c *C) {
	recorder := s.startRecorder()
	s.newServiceManager(c)
	s.planAddLayer(c, `
services:
    test1:
        override: replace
        command: /bin/sh -c "sleep 0.15; exit 7"
        on-failure: shutdown
`)
	s.planChanged(c)

	s.startServices(c, [][]string{{"test1"}})
	select {
	case restartType := <-s.stopDaemon:
		c.Assert(restartType, Equals, restart.RestartServiceFailure)
	case <-time.After(5 * time.Second):
		c.Fatalf("timed out waiting for stop-daemon channel")
	}

	startSpan := waitForSpan(c, recorder, "do start", "test1")
	span := waitForSpan(c, recorder, "service exit test1", "test1")
	c.Check(span.Parent().IsValid(), Equals, false)
	c.Assert(span.Links(), HasLen, 1)
	c.Check(span.Links()[0].SpanContext.SpanID(), Equals, startSpan.SpanContext().SpanID())
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)

	attrs := spanAttrs(span)
	c.Check(attrs[pebbleAttr("service.on")].AsString(), Equals, "on-failure")
	c.Check(attrs[pebbleAttr("service.action")].AsString(), Equals, "shutdown")
	c.Check(attrs["process.exit.code"].AsInt64(), Equals, int64(7))
	c.Check(eventNames(span), DeepEquals, []string{"restart requested"})
	c.Check(eventAttrs(c, span, "restart requested")[pebbleAttr("restart.type")].AsString(), Equals, "service-failure")
}

func (s *S) TestTracingRestartOnCheckFailure(c *C) {
	recorder := s.startRecorder()
	s.newServiceManager(c)
	envPath := filepath.Join(c.MkDir(), "env")
	s.planAddLayer(c, traceEnvLayer("test1", envPath, "sleep 10")+`
        backoff-delay: 50ms
        on-check-failure:
            chk1: restart
`)
	s.planChanged(c)

	s.startServices(c, [][]string{{"test1"}})
	s.waitForDoneCheck(c, "test1")
	s.waitUntilService(c, "test1", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusActive
	})

	// Report a check failure, as the check manager would, from within the
	// check run's span.
	checkCtx, checkSpan := tracing.Tracer().Start(context.Background(), "check chk1")
	s.manager.CheckFailed(checkCtx, "chk1")
	checkSpan.End()

	// Wait for the restarted process to run.
	s.waitForDoneCheck(c, "test1")

	startSpan := waitForSpan(c, recorder, "do start", "test1")
	span := waitForSpan(c, recorder, "service restart test1", "test1")

	// The restart is linked to both the start task and the check run.
	c.Check(span.Parent().IsValid(), Equals, false)
	c.Assert(span.Links(), HasLen, 2)
	c.Check(span.Links()[0].SpanContext.SpanID(), Equals, startSpan.SpanContext().SpanID())
	c.Check(span.Links()[1].SpanContext.SpanID(), Equals, checkSpan.SpanContext().SpanID())
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)

	attrs := spanAttrs(span)
	c.Check(attrs[pebbleAttr("service.on")].AsString(), Equals, "on-check-failure")
	c.Check(attrs[pebbleAttr("service.action")].AsString(), Equals, "restart")
	_, hasExitCode := attrs["process.exit.code"]
	c.Check(hasExitCode, Equals, false) // the process was terminated, it didn't exit by itself

	c.Check(eventNames(span), DeepEquals, []string{"sigterm", "process exited", "backoff", "process started"})
	c.Check(eventAttrs(c, span, "sigterm")[pebbleAttr("service.kill-delay")].AsString(), Equals, shortKillDelay.String())
	c.Check(eventAttrs(c, span, "process exited")["process.exit.code"].AsInt64(), Equals, int64(128+15)) // SIGTERM
	c.Check(eventAttrs(c, span, "backoff")[pebbleAttr("service.backoff.num")].AsInt64(), Equals, int64(1))

	c.Check(readTraceEnv(c, envPath), DeepEquals, []string{
		"TRACEPARENT=" + traceParent(startSpan) + " TRACESTATE=",
		"TRACEPARENT=" + traceParent(span) + " TRACESTATE=",
	})
}

func (s *S) TestTracingExitOnCheckFailureShutdown(c *C) {
	recorder := s.startRecorder()
	s.newServiceManager(c)
	s.planAddLayer(c, `
services:
    test1:
        override: replace
        command: sleep 10
        on-check-failure:
            chk1: shutdown
`)
	s.planChanged(c)

	s.startServices(c, [][]string{{"test1"}})
	s.waitUntilService(c, "test1", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusActive
	})

	checkCtx, checkSpan := tracing.Tracer().Start(context.Background(), "check chk1")
	s.manager.CheckFailed(checkCtx, "chk1")
	checkSpan.End()

	select {
	case restartType := <-s.stopDaemon:
		c.Assert(restartType, Equals, restart.RestartCheckFailure)
	case <-time.After(5 * time.Second):
		c.Fatalf("timed out waiting for stop-daemon channel")
	}

	startSpan := waitForSpan(c, recorder, "do start", "test1")
	span := waitForSpan(c, recorder, "service exit test1", "test1")
	c.Check(span.Parent().IsValid(), Equals, false)
	c.Assert(span.Links(), HasLen, 2)
	c.Check(span.Links()[0].SpanContext.SpanID(), Equals, startSpan.SpanContext().SpanID())
	c.Check(span.Links()[1].SpanContext.SpanID(), Equals, checkSpan.SpanContext().SpanID())
	attrs := spanAttrs(span)
	c.Check(attrs[pebbleAttr("service.on")].AsString(), Equals, "on-check-failure")
	c.Check(attrs[pebbleAttr("service.action")].AsString(), Equals, "shutdown")
	c.Check(eventNames(span), DeepEquals, []string{"restart requested"})
	c.Check(eventAttrs(c, span, "restart requested")[pebbleAttr("restart.type")].AsString(), Equals, "check-failure")
}

// backoffLayer is a service that exits successfully soon after the okay
// delay, and then backs off for long enough that a test can act on it.
const backoffLayer = `
services:
    test1:
        override: replace
        command: sleep 0.1
        backoff-delay: 10s
`

func (s *S) TestTracingStopDuringBackoff(c *C) {
	recorder := s.startRecorder()
	s.newServiceManager(c)
	s.planAddLayer(c, backoffLayer)
	s.planChanged(c)

	s.startServices(c, [][]string{{"test1"}})
	s.waitUntilService(c, "test1", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusBackoff
	})

	chg := s.stopServices(c, [][]string{{"test1"}})
	s.st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("Error: %v", chg.Err()))
	s.st.Unlock()

	// The stop cancels the pending restart, ending its span.
	span := waitForSpan(c, recorder, "service restart test1", "test1")
	c.Check(span.Status().Code, Equals, tracing.StatusUnset)
	attrs := spanAttrs(span)
	c.Check(attrs[pebbleAttr("service.on")].AsString(), Equals, "on-success")
	c.Check(attrs[pebbleAttr("service.action")].AsString(), Equals, "restart")
	c.Check(attrs["process.exit.code"].AsInt64(), Equals, int64(0))
	c.Check(eventNames(span), DeepEquals, []string{"backoff", "stopped during backoff"})

	// Nothing was done to a process by the stop task.
	stopSpan := waitForSpan(c, recorder, "do stop", "test1")
	c.Check(eventNames(stopSpan), HasLen, 0)
}

func (s *S) TestTracingStartDuringBackoff(c *C) {
	recorder := s.startRecorder()
	s.newServiceManager(c)
	s.planAddLayer(c, backoffLayer)
	s.planChanged(c)

	s.startServices(c, [][]string{{"test1"}})
	s.waitUntilService(c, "test1", func(svc *servstate.ServiceInfo) bool {
		return svc.Current == servstate.StatusBackoff
	})

	// Starting the service explicitly supersedes the pending restart.
	chg := s.startServices(c, [][]string{{"test1"}})
	s.st.Lock()
	c.Check(chg.Status(), Equals, state.DoneStatus, Commentf("Error: %v", chg.Err()))
	s.st.Unlock()

	span := waitForSpan(c, recorder, "service restart test1", "test1")
	c.Check(eventNames(span), DeepEquals, []string{"backoff", "start requested"})

	// The process is started by (and its events recorded on) the new start
	// task, not the superseded restart.
	var startSpans []tracingtest.ReadOnlySpan
	for start := time.Now(); len(startSpans) < 2 && time.Since(start) < 5*time.Second; time.Sleep(time.Millisecond) {
		startSpans = startSpans[:0]
		for _, ended := range recorder.Ended() {
			if ended.Name() == "do start" {
				startSpans = append(startSpans, ended)
			}
		}
	}
	c.Assert(startSpans, HasLen, 2)
	c.Check(eventNames(startSpans[1]), DeepEquals, []string{"process started", "okay"})
	c.Check(spanAttrs(startSpans[1])["process.pid"].AsInt64(), Equals, int64(s.manager.RunningCmds()["test1"].Process.Pid))
}
