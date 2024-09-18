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
	"strings"
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

func createLayer(t *testing.T, pebbleDir, layerFileName, layerYAML string) {
	t.Helper()

	layersDir := filepath.Join(pebbleDir, "layers")
	err := os.MkdirAll(layersDir, 0o755)
	if err != nil {
		t.Fatalf("Cannot create layers directory: %v", err)
	}

	layerPath := filepath.Join(layersDir, layerFileName)
	err = os.WriteFile(layerPath, []byte(layerYAML), 0o755)
	if err != nil {
		t.Fatalf("Cannot create layers file: %v", err)
	}
}

func pebbleRun(t *testing.T, pebbleDir string, args ...string) (<-chan servicelog.Entry, <-chan servicelog.Entry) {
	t.Helper()

	stdoutCh := make(chan servicelog.Entry)
	stderrCh := make(chan servicelog.Entry)

	cmd := exec.Command("../pebble", append([]string{"run"}, args...)...)
	cmd.Env = append(os.Environ(), "PEBBLE="+pebbleDir)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("Cannot create stdout pipe: %v", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("Cannot create stderr pipe: %v", err)
	}

	err = cmd.Start()
	if err != nil {
		t.Fatalf("Error starting 'pebble run': %v", err)
	}

	t.Cleanup(func() {
		err := cmd.Process.Signal(os.Interrupt)
		if err != nil {
			t.Errorf("Error sending SIGINT/Ctrl+C to pebble: %v", err)
		}
		cmd.Wait()
	})

	done := make(chan struct{})
	go func() {
		defer close(stdoutCh)
		defer close(stderrCh)

		readLogs := func(parser *servicelog.Parser, ch chan servicelog.Entry) {
			for parser.Next() {
				if err := parser.Err(); err != nil {
					t.Errorf("Cannot parse Pebble logs: %v", err)
				}
				select {
				case ch <- parser.Entry():
				case <-done:
					return
				}
			}
		}

		// Both stderr and stdout are needed, because pebble logs to stderr
		// while with "--verbose", services otuput to stdout.
		stderrParser := servicelog.NewParser(stderrPipe, 4*1024)
		stdoutParser := servicelog.NewParser(stdoutPipe, 4*1024)

		go readLogs(stdoutParser, stdoutCh)
		go readLogs(stderrParser, stderrCh)

		// Wait for both parsers to finish
		<-done
		<-done
	}()

	return stdoutCh, stderrCh
}

func waitForLog(t *testing.T, logsCh <-chan servicelog.Entry, expectedLog string, timeout time.Duration) {
	t.Helper()

	timeoutCh := time.After(timeout)
	for {
		select {
		case log, ok := <-logsCh:
			if !ok {
				t.Error("channel closed before all expected logs were received")
			}

			if strings.Contains(log.Message, expectedLog) {
				return
			}

		case <-timeoutCh:
			t.Fatalf("timed out after %v waiting for log %s", 3*time.Second, expectedLog)
		}
	}
}

func waitForFile(t *testing.T, file string, timeout time.Duration) {
	t.Helper()

	timeoutCh := time.After(timeout)
	ticker := time.NewTicker(time.Millisecond)
	for {
		select {
		case <-timeoutCh:
			t.Fatalf("timeout waiting for file %s", file)

		case <-ticker.C:
			stat, err := os.Stat(file)
			if err == nil && stat.Mode().IsRegular() {
				os.Remove(file)
				return
			}
		}
	}
}

func runPebbleCommand(t *testing.T, pebbleDir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("../pebble", args...)
	cmd.Env = append(os.Environ(), "PEBBLE="+pebbleDir)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("error executing pebble command: %v", err)
	}

	return string(output)
}
