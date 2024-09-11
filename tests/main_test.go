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
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/canonical/pebble/internals/servicelog"
)

// TestMain builds the pebble binary before running the integration tests.
func TestMain(m *testing.M) {
	goBuild := exec.Command("go", "build", "-o", "../pebble", "../cmd/pebble")
	if err := goBuild.Run(); err != nil {
		fmt.Println("Cannot build pebble binary:", err)
		os.Exit(1)
	}

	exitCode := m.Run()
	os.Exit(exitCode)
}

func createLayer(t *testing.T, pebbleDir string, layerFileName string, layerYAML string) {
	t.Helper()

	layersDir := filepath.Join(pebbleDir, "layers")
	err := os.MkdirAll(layersDir, 0o755)
	if err != nil {
		t.Fatalf("Cannot create layers directory: pipe: %v", err)
	}

	layerPath := filepath.Join(layersDir, layerFileName)
	err = os.WriteFile(layerPath, []byte(layerYAML), 0o755)
	if err != nil {
		t.Fatalf("Error creating layers file: %v", err)
	}
}

func pebbleRun(t *testing.T, pebbleDir string, args ...string) <-chan servicelog.Entry {
	t.Helper()

	logsCh := make(chan servicelog.Entry)

	cmd := exec.Command("../pebble", append([]string{"run"}, args...)...)
	cmd.Env = append(os.Environ(), "PEBBLE="+pebbleDir)

	t.Cleanup(func() {
		err := cmd.Process.Signal(os.Interrupt)
		if err != nil {
			t.Errorf("Error sending SIGINT/Ctrl+C to pebble: %v", err)
		}
		cmd.Wait()
	})

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("Error creating stderr pipe: %v", err)
	}

	err = cmd.Start()
	if err != nil {
		t.Fatalf("Error starting 'pebble run': %v", err)
	}

	go func() {
		defer close(logsCh)
		parser := servicelog.NewParser(stderrPipe, 4*1024)
		for parser.Next() {
			if err := parser.Err(); err != nil {
				t.Errorf("Cannot parse Pebble logs: %v", err)
			}
			logsCh <- parser.Entry()
		}
	}()

	return logsCh
}

func waitForServices(t *testing.T, pebbleDir string, expectedServices []string, timeout time.Duration) {
	for _, service := range expectedServices {
		waitForService(t, pebbleDir, service, timeout)
	}
}

func waitForService(t *testing.T, pebbleDir string, service string, timeout time.Duration) {
	serviceFilePath := filepath.Join(pebbleDir, service)
	timeoutCh := time.After(timeout)
	ticker := time.NewTicker(time.Millisecond)
	for {
		select {
		case <-timeoutCh:
			t.Errorf("timeout waiting for service %s", service)
			return

		case <-ticker.C:
			stat, err := os.Stat(serviceFilePath)
			if err == nil && stat.Mode().IsRegular() {
				os.Remove(serviceFilePath)
				return
			}
		}
	}
}
