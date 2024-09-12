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
	"net"
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
		t.Fatalf("Cannot create layers file: %v", err)
	}
}

func createIdentitiesFile(t *testing.T, pebbleDir string, identitiesFileName string, identitiesYAML string) {
	t.Helper()

	identitiesPath := filepath.Join(pebbleDir, identitiesFileName)
	if err := os.WriteFile(identitiesPath, []byte(identitiesYAML), 0o755); err != nil {
		t.Fatalf("Cannot create layers file: %v", err)
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

	done := make(chan struct{})
	go func() {
		defer close(logsCh)

		readLogs := func(parser *servicelog.Parser) {
			for parser.Next() {
				if err := parser.Err(); err != nil {
					t.Errorf("Cannot parse Pebble logs: %v", err)
				}
				select {
				case logsCh <- parser.Entry():
				case <-done:
					return
				}
			}
		}

		// Both stderr and stdout are needed, because pebble logs to stderr
		// while with "--verbose", services otuput to stdout.
		stderrParser := servicelog.NewParser(stderrPipe, 4*1024)
		stdoutParser := servicelog.NewParser(stdoutPipe, 4*1024)

		// Channel to signal completion and close logsCh
		done := make(chan struct{})
		defer close(done)

		go readLogs(stderrParser)
		go readLogs(stdoutParser)

		// Wait for both parsers to finish
		<-done
		<-done
	}()

	return logsCh
}

func waitForLogs(logsCh <-chan servicelog.Entry, expectedLogs []string, timeout time.Duration) error {
	receivedLogs := make(map[string]struct{})

	timeoutCh := time.After(timeout)
	for {
		select {
		case log, ok := <-logsCh:
			if !ok {
				return errors.New("channel closed before all expected logs were received")
			}

			for _, expectedLog := range expectedLogs {
				if _, ok := receivedLogs[expectedLog]; !ok && containsSubstring(log.Message, expectedLog) {
					receivedLogs[expectedLog] = struct{}{}
					break
				}
			}

			allLogsReceived := true
			for _, log := range expectedLogs {
				if _, ok := receivedLogs[log]; !ok {
					allLogsReceived = false
					break
				}
			}

			if allLogsReceived {
				return nil
			}

		case <-timeoutCh:
			missingLogs := []string{}
			for _, log := range expectedLogs {
				if _, ok := receivedLogs[log]; !ok {
					missingLogs = append(missingLogs, log)
				}
			}
			return errors.New("timed out waiting for log: " + strings.Join(missingLogs, ", "))
		}
	}
}

func containsSubstring(s, substr string) bool {
	return strings.Contains(s, substr)
}

func waitForServices(t *testing.T, pebbleDir string, expectedServices []string, timeout time.Duration) {
	t.Helper()

	for _, service := range expectedServices {
		waitForService(t, pebbleDir, service, timeout)
	}
}

func waitForService(t *testing.T, pebbleDir string, service string, timeout time.Duration) {
	t.Helper()

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

func isPortUsedByProcess(t *testing.T, port string, processName string) bool {
	t.Helper()

	conn, err := net.Listen("tcp", ":"+port)
	if err == nil {
		conn.Close()
		return false
	}
	if conn != nil {
		conn.Close()
	}

	cmd := exec.Command("lsof", "-i", ":"+port)
	output, err := cmd.Output()
	if err != nil {
		t.Errorf("Error running lsof command: %v", err)
		return false
	}

	outputStr := string(output)
	if strings.Contains(outputStr, processName) {
		return true
	}

	return false
}

func runPebbleCmdAndCheckOutput(t *testing.T, pebbleDir string, expectedOutput []string, args ...string) {
	t.Helper()

	cmd := exec.Command("../pebble", args...)
	cmd.Env = append(os.Environ(), "PEBBLE="+pebbleDir)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("error executing pebble command: %v", err)
	}

	outputStr := string(output)

	for _, expected := range expectedOutput {
		if !strings.Contains(outputStr, expected) {
			t.Errorf("Expected output %s not found in command output", expected)
		}
	}
}
