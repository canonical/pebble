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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormal(t *testing.T) {
	pebbleDir := t.TempDir()

	layerYAML := `
services:
    svc1:
        override: replace
        command: /bin/sh -c "{{.svc1Cmd}}"
        startup: enabled
    svc2:
        override: replace
        command: /bin/sh -c "{{.svc2Cmd}}"
        startup: enabled
`
	svc1Cmd := fmt.Sprintf("touch %s; sleep 1000", filepath.Join(pebbleDir, "svc1"))
	svc2Cmd := fmt.Sprintf("touch %s; sleep 1000", filepath.Join(pebbleDir, "svc2"))
	layerYAML = strings.Replace(layerYAML, "{{.svc1Cmd}}", svc1Cmd, -1)
	layerYAML = strings.Replace(layerYAML, "{{.svc2Cmd}}", svc2Cmd, -1)

	createLayer(t, pebbleDir, "001-simple-layer.yaml", layerYAML)

	_ = pebbleRun(t, pebbleDir)

	expectedServices := []string{"svc1", "svc2"}
	waitForServices(t, pebbleDir, expectedServices, time.Second*3)
}

func TestCreateDirs(t *testing.T) {
	tmpDir := t.TempDir()
	pebbleDir := filepath.Join(tmpDir, "PEBBLE_HOME")

	logsCh := pebbleRun(t, pebbleDir, "--create-dirs")
	expectedLogs := []string{"Started daemon"}
	if err := waitForLogs(logsCh, expectedLogs, time.Second*3); err != nil {
		t.Errorf("Error waiting for logs: %v", err)
	}

	if _, err := os.Stat(pebbleDir); err != nil {
		t.Errorf("pebble run --create-dirs failed: %v", err)
	}
}

func TestHold(t *testing.T) {
	pebbleDir := t.TempDir()

	layerYAML := `
services:
    svc1:
        override: replace
        command: /bin/sh -c "{{.svc1Cmd}}"
        startup: enabled
`
	svc1Cmd := fmt.Sprintf("touch %s ; sleep 1000", filepath.Join(pebbleDir, "svc1"))
	layerYAML = strings.Replace(layerYAML, "{{.svc1Cmd}}", svc1Cmd, -1)

	createLayer(t, pebbleDir, "001-simple-layer.yaml", layerYAML)

	logsCh := pebbleRun(t, pebbleDir, "--hold")
	expectedLogs := []string{"Started daemon"}
	if err := waitForLogs(logsCh, expectedLogs, time.Second*3); err != nil {
		t.Errorf("Error waiting for logs: %v", err)
	}

	// Sleep a second before checking services because immediate check
	// can't guarantee that svc1 is not started shortly after the log "Started daemon".
	time.Sleep(time.Second)

	_, err := os.Stat(filepath.Join(pebbleDir, "svc1"))
	if err == nil {
		t.Error("pebble run --hold failed, services are still started")
	} else {
		if !os.IsNotExist(err) {
			t.Errorf("Error checking service %s: %v", "svc1", err)
			fmt.Printf("Error checking the file: %v\n", err)
		}
	}
}

func TestHttpPort(t *testing.T) {
	pebbleDir := t.TempDir()

	port := "4000"
	logsCh := pebbleRun(t, pebbleDir, "--http=:"+port)
	expectedLogs := []string{"Started daemon"}
	if err := waitForLogs(logsCh, expectedLogs, time.Second*3); err != nil {
		t.Errorf("Error waiting for logs: %v", err)
	}

	if !isPortUsedByProcess(t, port, "pebble") {
		t.Errorf("Pebble is not listening on port %s", port)
	}
}

func TestVerbose(t *testing.T) {
	pebbleDir := t.TempDir()

	layerYAML := `
services:
    svc1:
        override: replace
        command: /bin/sh -c "{{.svc1Cmd}}"
        startup: enabled
`
	layersFileName := "001-simple-layer.yaml"
	svc1Cmd := fmt.Sprintf("cat %s; sleep 1000", filepath.Join(pebbleDir, "layers", layersFileName))
	layerYAML = strings.Replace(layerYAML, "{{.svc1Cmd}}", svc1Cmd, -1)

	createLayer(t, pebbleDir, layersFileName, layerYAML)

	logsCh := pebbleRun(t, pebbleDir, "--verbose")
	expectedLogs := []string{
		"Started daemon",
		"services:",
		"svc1:",
		"override: replace",
		"startup: enabled",
		"command: /bin/sh -c",
	}
	if err := waitForLogs(logsCh, expectedLogs, time.Second*3); err != nil {
		t.Errorf("Error waiting for logs: %v", err)
	}
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
	svc1Cmd := fmt.Sprintf("cat %s; sleep 1000", filepath.Join(pebbleDir, "layers", layersFileName))
	layerYAML = strings.Replace(layerYAML, "{{.svc1Cmd}}", svc1Cmd, -1)

	createLayer(t, pebbleDir, layersFileName, layerYAML)

	logsCh := pebbleRun(t, pebbleDir, "--verbose",
		"--args",
		"svc1",
		"-c",
		fmt.Sprintf("cat %s; sleep 1000", filepath.Join(pebbleDir, "layers", layersFileName)),
	)
	expectedLogs := []string{
		"Started daemon",
		"services:",
		"svc1:",
		"override: replace",
		"startup: enabled",
		"command: /bin/sh",
	}
	if err := waitForLogs(logsCh, expectedLogs, time.Second*3); err != nil {
		t.Errorf("Error waiting for logs: %v", err)
	}
}

func TestIdentities(t *testing.T) {
	pebbleDir := t.TempDir()

	identitiesYAML := `
identities:
    bob:
        access: admin
        local:
            user-id: 42
    alice:
        access: read
        local:
            user-id: 2000
`
	identitiesFileName := "idents-add.yaml"
	createIdentitiesFile(t, pebbleDir, identitiesFileName, identitiesYAML)

	logsCh := pebbleRun(t, pebbleDir, "--identities="+filepath.Join(pebbleDir, identitiesFileName))
	expectedLogs := []string{
		"Started daemon",
		"POST /v1/services",
	}
	if err := waitForLogs(logsCh, expectedLogs, time.Second*3); err != nil {
		t.Errorf("Error waiting for logs: %v", err)
	}

	expectedOutput := []string{"access: admin", "local:", "user-id: 42"}
	runPebbleCmdAndCheckOutput(t, pebbleDir, expectedOutput, "identity", "bob")

	expectedOutput = []string{"access: read", "local:", "user-id: 2000"}
	runPebbleCmdAndCheckOutput(t, pebbleDir, expectedOutput, "identity", "alice")
}
