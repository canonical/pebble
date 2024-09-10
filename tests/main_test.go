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

package tests_test

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	. "github.com/canonical/pebble/tests"
)

func TestMain(m *testing.M) {
	goBuild := exec.Command("go", "build", "-o", "../pebble", "../cmd/pebble")
	if err := goBuild.Run(); err != nil {
		fmt.Println("Setup failed with error:", err)
		os.Exit(1)
	}

	exitVal := m.Run()
	os.Exit(exitVal)
}

func TestPebbleRunNormal(t *testing.T) {
	pebbleDir := t.TempDir()

	layerYAML := `
services:
    demo-service:
        override: replace
        command: sleep 1000
        startup: enabled
    demo-service2:
        override: replace
        command: sleep 1000
        startup: enabled
`[1:]

	CreateLayer(t, pebbleDir, "001-simple-layer.yaml", layerYAML)

	logsCh := PebbleRun(t, pebbleDir)
	expected := []string{
		"Started daemon",
		"Service \"demo-service\" starting",
		"Service \"demo-service2\" starting",
		"Started default services with change",
	}
	if err := WaitForLogs(logsCh, expected, time.Second*3); err != nil {
		t.Errorf("Error waiting for logs: %v", err)
	}
}
