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
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func CreateLayer(t *testing.T, pebbleDir string, layerFileName string, layerYAML string) {
	t.Helper()

	layersDir := filepath.Join(pebbleDir, "layers")
	err := os.MkdirAll(layersDir, 0755)
	if err != nil {
		t.Fatalf("Error creating layers directory: pipe: %v", err)
	}

	layerPath := filepath.Join(layersDir, layerFileName)
	err = os.WriteFile(layerPath, []byte(layerYAML), 0755)
	if err != nil {
		t.Fatalf("Error creating layers file: %v", err)
	}
}

func PebbleRun(t *testing.T, pebbleDir string) <-chan string {
	t.Helper()

	logsCh := make(chan string)

	cmd := exec.Command("../pebble", "run")
	cmd.Env = append(os.Environ(), fmt.Sprintf("PEBBLE=%s", pebbleDir))

	t.Cleanup(func() {
		err := cmd.Process.Signal(os.Interrupt)
		if err != nil {
			t.Errorf("Error sending SIGINT/Ctrl+C to pebble: %v", err)
		}
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

		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			line := scanner.Text()
			logsCh <- line
		}
	}()

	return logsCh
}

func WaitForLogs(logsCh <-chan string, expectedLogs []string, timeout time.Duration) error {
	receivedLogs := make(map[string]struct{})
	start := time.Now()

	for {
		select {
		case log, ok := <-logsCh:
			if !ok {
				return errors.New("channel closed before all expected logs were received")
			}

			for _, expectedLog := range expectedLogs {
				if _, ok := receivedLogs[expectedLog]; !ok && containsSubstring(log, expectedLog) {
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

		default:
			if time.Since(start) > timeout {
				missingLogs := []string{}
				for _, log := range expectedLogs {
					if _, ok := receivedLogs[log]; !ok {
						missingLogs = append(missingLogs, log)
					}
				}
				return errors.New("timed out waiting for log: " + strings.Join(missingLogs, ", "))
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func containsSubstring(s, substr string) bool {
	return strings.Contains(s, substr)
}
