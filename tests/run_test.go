//go:build integration

// Copyright (c) 2024 Canonical Ltd
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
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStartupEnabledServices(t *testing.T) {
	pebbleDir := t.TempDir()

	layerYAML := fmt.Sprintf(`
services:
    svc1:
        override: replace
        command: /bin/sh -c "touch %s; sleep 1000"
        startup: enabled
    svc2:
        override: replace
        command: /bin/sh -c "touch %s; sleep 1000"
        startup: enabled
`,
		filepath.Join(pebbleDir, "svc1"),
		filepath.Join(pebbleDir, "svc2"),
	)

	createLayer(t, pebbleDir, "001-simple-layer.yaml", layerYAML)

	_, _ = pebbleRun(t, pebbleDir)

	waitForFile(t, filepath.Join(pebbleDir, "svc1"), 3*time.Second)
	waitForFile(t, filepath.Join(pebbleDir, "svc2"), 3*time.Second)
}

func TestCreateDirs(t *testing.T) {
	tmpDir := t.TempDir()
	pebbleDir := filepath.Join(tmpDir, "pebble")

	_, stderrCh := pebbleRun(t, pebbleDir, "--create-dirs")
	waitForLog(t, stderrCh, "pebble", "Started daemon", 3*time.Second)

	st, err := os.Stat(pebbleDir)
	if err != nil {
		t.Fatalf("pebble run --create-dirs didn't create Pebble directory: %v", err)
	}
	if !st.IsDir() {
		t.Fatalf("pebble dir %s is not a directory: %v", pebbleDir, err)
	}
}

func TestHold(t *testing.T) {
	pebbleDir := t.TempDir()

	layerYAML := fmt.Sprintf(`
services:
    svc1:
        override: replace
        command: /bin/sh -c "touch %s; sleep 1000"
        startup: enabled
`,
		filepath.Join(pebbleDir, "svc1"),
	)
	createLayer(t, pebbleDir, "001-simple-layer.yaml", layerYAML)

	_, _ = pebbleRun(t, pebbleDir, "--hold")

	// Sleep 100 millisecond before checking services because immediate check
	// can't guarantee that svc1 is not started shortly after the log "Started daemon".
	time.Sleep(100 * time.Millisecond)

	_, err := os.Stat(filepath.Join(pebbleDir, "svc1"))
	if err == nil {
		t.Fatal("pebble run --hold failed, services are still started")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Stat returned error other than ErrNotExist: %v", err)
	}
}

func TestHTTPPort(t *testing.T) {
	pebbleDir := t.TempDir()

	port := "61382"
	_, stderrCh := pebbleRun(t, pebbleDir, "--http=:"+port)
	waitForLog(t, stderrCh, "pebble", "Started daemon", 3*time.Second)

	resp, err := http.Get(fmt.Sprintf("http://localhost:%s/v1/health", port))
	if err != nil {
		t.Fatalf("port %s is not being listened by : %v", port, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("error checking pebble /v1/health on port %s: %v", port, err)
	}
}

func TestVerbose(t *testing.T) {
	pebbleDir := t.TempDir()

	layersFileName := "001-simple-layer.yaml"
	layerYAML := `
services:
    svc1:
        override: replace
        command: /bin/sh -c "echo 'hello world'; sleep 1000"
        startup: enabled
`
	createLayer(t, pebbleDir, layersFileName, layerYAML)

	stdoutCh, stderrCh := pebbleRun(t, pebbleDir, "--verbose")
	waitForLog(t, stderrCh, "pebble", "Started daemon", 3*time.Second)
	waitForLog(t, stdoutCh, "svc1", "hello world", 3*time.Second)
}

func TestArgs(t *testing.T) {
	pebbleDir := t.TempDir()

	layerYAML := `
services:
    svc1:
        override: replace
        command: /bin/sh
        startup: enabled
`
	layersFileName := "001-simple-layer.yaml"
	createLayer(t, pebbleDir, layersFileName, layerYAML)

	stdoutCh, stderrCh := pebbleRun(t, pebbleDir, "--verbose",
		"--args",
		"svc1",
		"-c",
		"echo 'hello world'; sleep 1000",
	)
	waitForLog(t, stderrCh, "pebble", "Started daemon", 3*time.Second)
	waitForLog(t, stdoutCh, "svc1", "hello world", 3*time.Second)
}

func TestIdentities(t *testing.T) {
	pebbleDir := t.TempDir()

	identitiesYAML := `
identities:
    bob:
        access: admin
        local:
            user-id: 42
`[1:]
	identitiesFileName := "idents-add.yaml"
	if err := os.WriteFile(filepath.Join(pebbleDir, identitiesFileName), []byte(identitiesYAML), 0o755); err != nil {
		t.Fatalf("Cannot write identities file: %v", err)
	}

	_, stderrCh := pebbleRun(t, pebbleDir, "--identities="+filepath.Join(pebbleDir, identitiesFileName))
	waitForLog(t, stderrCh, "pebble", "Started daemon", 3*time.Second)

	output := runPebbleCommand(t, pebbleDir, "identity", "bob")
	expected := `
access: admin
local:
    user-id: 42
`[1:]
	if output != expected {
		t.Fatalf("error checking identities. expected: %s; got: %s", expected, output)
	}
}
