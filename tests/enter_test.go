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
	"path/filepath"
	"testing"
	"time"
)

func TestEnterExec(t *testing.T) {
	pebbleDir := t.TempDir()

	script := `
#!/bin/sh
echo "hello world"
sleep 1.1
	`
	path := filepath.Join(pebbleDir, "test.sh")
	err := os.WriteFile(path, []byte(script), 0755)
	if err != nil {
		t.Fatal(err)
	}

	stdoutCh, _ := pebbleDaemon(t, pebbleDir, "enter", "exec", "/bin/sh", "-c", path)
	waitForText(t, stdoutCh, "hello world", 3*time.Second)
}

func TestEnterExecVerboseEnabledByEnvVar(t *testing.T) {
	t.Setenv("PEBBLE_VERBOSE", "1")

	pebbleDir := t.TempDir()

	script := `
#!/bin/sh
echo "hello world"
sleep 1.1
	`
	path := filepath.Join(pebbleDir, "test.sh")
	err := os.WriteFile(path, []byte(script), 0755)
	if err != nil {
		t.Fatal(err)
	}

	stdoutCh, stderrCh := pebbleDaemon(t, pebbleDir, "enter", "exec", "/bin/sh", "-c", path)
	waitForText(t, stderrCh, "Started daemon", 3*time.Second)
	waitForText(t, stderrCh, "POST /v1/exec", 3*time.Second)
	waitForText(t, stdoutCh, "hello world", 3*time.Second)
}
