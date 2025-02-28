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
sleep 2
	`
	path := filepath.Join(pebbleDir, "test.sh")
	err := os.WriteFile(path, []byte(script), 0755)
	if err != nil {
		t.Fatal(err)
	}

	stdoutCh, _ := pebbleEnter(t, pebbleDir, "exec", "/bin/sh", "-c", path)
	waitForText(t, stdoutCh, "hello world", 1*time.Second)
}

func TestEnterExecVerboseEnabledByEnvVar(t *testing.T) {
	os.Setenv("PEBBLE_VERBOSE", "1")
	defer os.Setenv("PEBBLE_VERBOSE", "")

	pebbleDir := t.TempDir()

	script := `
#!/bin/sh
echo "hello world"
sleep 2
	`
	path := filepath.Join(pebbleDir, "test.sh")
	err := os.WriteFile(path, []byte(script), 0755)
	if err != nil {
		t.Fatal(err)
	}

	stdoutCh, stderrCh := pebbleEnter(t, pebbleDir, "exec", "/bin/sh", "-c", path)
	waitForText(t, stderrCh, "Started daemon", 1*time.Second)
	waitForText(t, stderrCh, "POST /v1/exec", 1*time.Second)
	waitForText(t, stdoutCh, "hello world", 1*time.Second)
}
