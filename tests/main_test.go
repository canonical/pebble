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
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/canonical/pebble/internals/servicelog"
)

var pebbleBin = flag.String("pebblebin", "", "Path to the pre-built Pebble binary")

// TestMain builds the pebble binary of `-pebblebin` flag is not set
// before running the integration tests.
func TestMain(m *testing.M) {
	flag.Parse()

	if *pebbleBin == "" {
		goBuild := exec.Command("go", "build", "-o", "../pebble", "../cmd/pebble")
		if err := goBuild.Run(); err != nil {
			fmt.Println("Cannot build pebble binary:", err)
			os.Exit(1)
		}
		*pebbleBin = "../pebble"
	} else {
		// Use the pre-built Pebble binary provided by the pebbleBin flag.
		fmt.Println("Using pre-built Pebble binary at:", *pebbleBin)
	}

	exitCode := m.Run()
	os.Exit(exitCode)
}

// createLayer creates a layer file with layerYAML under the directory "pebbleDir/layers".
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

// pebbleRun starts the pebble daemon (`pebble run`) with optional arguments
// and returns two channels for standard output and standard error.
func pebbleRun(t *testing.T, pebbleDir string, args ...string) (stdoutCh chan servicelog.Entry, stderrCh chan servicelog.Entry) {
	t.Helper()

	stdoutCh = make(chan servicelog.Entry)
	stderrCh = make(chan servicelog.Entry)

	cmd := exec.Command(*pebbleBin, append([]string{"run"}, args...)...)
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

	stopStdout := make(chan struct{})
	stopStderr := make(chan struct{})

	t.Cleanup(func() {
		err := cmd.Process.Signal(os.Interrupt)
		if err != nil {
			t.Errorf("Error sending SIGINT/Ctrl+C to pebble: %v", err)
		}
		cmd.Wait()
		close(stopStdout)
		close(stopStderr)
	})

	readLogs := func(parser *servicelog.Parser, ch chan servicelog.Entry, stop <-chan struct{}) {
		for parser.Next() {
			if err := parser.Err(); err != nil {
				t.Errorf("Cannot parse Pebble logs: %v", err)
			}
			select {
			case ch <- parser.Entry():
			case <-stop:
				return
			}
		}
	}

	// Both stderr and stdout are needed, because pebble logs to stderr
	// while with "--verbose", services output to stdout.
	stderrParser := servicelog.NewParser(stderrPipe, 4*1024)
	stdoutParser := servicelog.NewParser(stdoutPipe, 4*1024)

	go readLogs(stdoutParser, stdoutCh, stopStdout)
	go readLogs(stderrParser, stderrCh, stopStderr)

	return stdoutCh, stderrCh
}

// pebbleEnter runs `pebble enter` with optional arguments
// and returns two channels for standard output and standard error.
func pebbleEnter(t *testing.T, pebbleDir string, args ...string) (stdoutCh chan string, stderrCh chan string) {
	t.Helper()

	stdoutCh = make(chan string)
	stderrCh = make(chan string)

	cmd := exec.Command(*pebbleBin, append([]string{"enter"}, args...)...)
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
		t.Fatalf("Error starting 'pebble enter': %v", err)
	}

	stopStdout := make(chan struct{})
	stopStderr := make(chan struct{})

	t.Cleanup(func() {
		err := cmd.Process.Signal(os.Interrupt)
		if err != nil {
			t.Errorf("Error sending SIGINT/Ctrl+C to pebble: %v", err)
		}
		cmd.Wait()
		close(stopStdout)
		close(stopStderr)
	})

	readLines := func(reader io.Reader, ch chan string, stop <-chan struct{}) {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			select {
			case ch <- scanner.Text():
			case <-stop:
				return
			}
		}
		if err := scanner.Err(); err != nil {
			t.Errorf("Error reading output: %v", err)
		}
	}

	go readLines(stdoutPipe, stdoutCh, stopStdout)
	go readLines(stderrPipe, stderrCh, stopStderr)

	return stdoutCh, stderrCh
}

// waitForLog waits until an expectedLog from an expectedService appears in the logs channel, or fails the test after a
// specified timeout if the expectedLog is still not found.
func waitForLog(t *testing.T, logsCh <-chan servicelog.Entry, expectedService, expectedLog string, timeout time.Duration) {
	t.Helper()

	timeoutCh := time.After(timeout)
	for {
		select {
		case log, ok := <-logsCh:
			if !ok {
				t.Error("channel closed before all expected logs were received")
			}
			if log.Service == expectedService && strings.Contains(log.Message, expectedLog) {
				return
			}

		case <-timeoutCh:
			t.Fatalf("timed out after %v waiting for log %s", 3*time.Second, expectedLog)
		}
	}
}

// waitForText waits until an expected string appears in the textCh channel, or fails the test after a
// specified timeout if the expectedText is still not found.
func waitForText(t *testing.T, textCh <-chan string, expectedText string, timeout time.Duration) {
	t.Helper()

	timeoutCh := time.After(timeout)
	for {
		select {
		case text, ok := <-textCh:
			if !ok {
				t.Error("channel closed before expected text was received")
				return // Exit the loop if the channel is closed
			}
			if strings.Contains(text, expectedText) {
				return // Exit the loop if the expected text is found
			}

		case <-timeoutCh:
			t.Fatalf("timed out after %v waiting for text: %q", timeout, expectedText)
		}
	}
}

// waitForFile waits until a file exists, or fails the test after a specified timeout
// if the file still doesn't exist.
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
				return
			}
		}
	}
}

// runPebbleCommand runs a pebble command and returns the standard output.
func runPebbleCommand(t *testing.T, pebbleDir string, args ...string) string {
	t.Helper()

	cmd := exec.Command(*pebbleBin, args...)
	cmd.Env = append(os.Environ(), "PEBBLE="+pebbleDir)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("error executing pebble command: %v", err)
	}

	return string(output)
}
