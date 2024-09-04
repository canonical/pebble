//go:build integration

package testintegration

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var DefaultLayerYAML string = `
services:
    demo-service:
        override: replace
        command: sleep 1000
        startup: enabled
`[1:]

func Setup() error {
	cmd := exec.Command("go", "build", "./cmd/pebble")
	cmd.Dir = getRootDir()
	return cmd.Run()
}

func getRootDir() string {
	wd, _ := os.Getwd()
	return filepath.Join(wd, "../../")
}

func AllKeywordsFoundInLogs(logs []string, keywords []string) (bool, []string) {
	var notFound []string

	for _, keyword := range keywords {
		keywordFound := false
		for _, log := range logs {
			if strings.Contains(log, keyword) {
				keywordFound = true
				break
			}
		}
		if !keywordFound {
			notFound = append(notFound, keyword)
		}
	}

	return len(notFound) == 0, notFound
}

func CreateLayer(t *testing.T, pebbleDir string, layerFileName string, layerYAML string) {
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

func PebbleRun(t *testing.T, pebbleDir string) []string {
	cmd := exec.Command("./pebble", "run")
	cmd.Dir = getRootDir()
	cmd.Env = append(os.Environ(), fmt.Sprintf("PEBBLE=%s", pebbleDir))

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("Error creating stderr pipe: %v", err)
	}

	err = cmd.Start()
	defer cmd.Process.Kill()
	if err != nil {
		t.Fatalf("Error starting 'pebble run': %v", err)
	}

	var logs []string

	lastOutputTime := time.Now()

	go func() {
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			lastOutputTime = time.Now()
			line := scanner.Text()
			logs = append(logs, line)
		}
	}()

	for {
		time.Sleep(100 * time.Millisecond)
		if time.Since(lastOutputTime) > 1*time.Second {
			break
		}
	}

	return logs
}
