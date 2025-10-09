//go:build integration

// Copyright (c) 2025 Canonical Ltd
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

package tests

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Ensure Pebble doesn't re-run "start" tasks when restarting after an
// "on-failure: shutdown" service has caused it to exit. Regression test for:
// https://github.com/canonical/pebble/issues/527
func TestServiceStartedNoRestart(t *testing.T) {
	pebbleDir := t.TempDir()

	layerYAML := `
services:
    svc1:
        override: replace
        command: /bin/sh -c "sleep 0.1; badcommand"
        on-failure: shutdown
`
	createLayer(t, pebbleDir, "001-layer.yaml", layerYAML)

	_, stderrCh := pebbleDaemon(t, pebbleDir, "run")
	waitForLog(t, stderrCh, "pebble", "Started daemon", 3*time.Second)

	// Run "pebble start" but don't wait for command to finish (it'll timeout and fail).
	cmd := exec.Command(*pebbleBin, "start", "svc1")
	cmd.Env = append(os.Environ(), "PEBBLE="+pebbleDir)
	err := cmd.Start()
	if err != nil {
		t.Fatalf("error starting command: %s", err)
	}

	// Starting the "on-failure: shutdown" service should make the daemon shut down.
	waitForLog(t, stderrCh, "pebble", "triggering failure shutdown", 3*time.Second)
	waitForTermination(t, stderrCh, 3*time.Second)

	// Run Pebble again and ensure it *doesn't* exit this time (within 1s).
	_, stderrCh = pebbleDaemon(t, pebbleDir, "run")
	waitForLog(t, stderrCh, "pebble", "Started daemon", 3*time.Second)
	timeout := time.After(time.Second)
waitExit:
	for {
		select {
		case line := <-stderrCh:
			if strings.Contains(line, "Server exiting!") {
				t.Fatalf("server exited unexpectedly")
			}
		case <-timeout:
			break waitExit
		}
	}

	output := runPebbleCommand(t, pebbleDir, "services")
	expected := `
Service  Startup   Current   Since
svc1     disabled  inactive  -
`[1:]
	if output != expected {
		t.Fatalf("unexpected services output\nWant:\n%s\nGot:\n%s", expected, output)
	}
}
