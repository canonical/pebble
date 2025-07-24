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
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEnterExec(t *testing.T) {
	pebbleDir := t.TempDir()
	stdoutCh, _ := pebbleDaemon(t, pebbleDir, "enter", "exec", "/bin/sh", "-c", "echo 'hello world'; sleep 1.1")
	waitForText(t, stdoutCh, "hello world", 3*time.Second)
}

func TestEnterExecVerboseEnabledByEnvVar(t *testing.T) {
	t.Setenv("PEBBLE_VERBOSE", "1")
	pebbleDir := t.TempDir()

	stdoutCh, stderrCh := pebbleDaemon(t, pebbleDir, "enter", "exec", "/bin/sh", "-c", "echo 'hello world'; sleep 1.1")
	waitForText(t, stderrCh, "Started daemon", 3*time.Second)
	waitForText(t, stderrCh, "POST /v1/exec", 3*time.Second)
	waitForText(t, stdoutCh, "hello world", 3*time.Second)
}

// TestEnterPersistNever tests that when the environment variable `PEBBLE_PERSIST` is set to "never",
// Pebble does not persist its state to the disk.
func TestEnterPersistNever(t *testing.T) {
	t.Setenv("PEBBLE_PERSIST", "never")
	pebbleDir := t.TempDir()

	stdoutCh, _ := pebbleDaemon(t, pebbleDir, "enter", "exec", "/bin/sh", "-c", "echo 'hello world'; sleep 0.1")
	waitForText(t, stdoutCh, "hello world", 3*time.Second)

	_, err := os.Stat(filepath.Join(pebbleDir, ".pebble.state"))
	if err == nil {
		t.Fatalf("pebble enter with PEBBLE_PERSIST set to 'never' still created the state file")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pebble enter with PEBBLE_PERSIST set to 'never' got error other than ErrNotExist: %v", err)
	}
}
