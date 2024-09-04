//go:build integration

package testintegration_test

import (
	"fmt"
	"os"
	"testing"

	. "github.com/canonical/pebble/internals/testintegration"
)

func TestMain(m *testing.M) {
	if err := Setup(); err != nil {
		fmt.Println("Setup failed with error:", err)
		os.Exit(1)
	}

	exitVal := m.Run()
	os.Exit(exitVal)
}

func TestPebbleRunWithSimpleLayer(t *testing.T) {
	pebbleDir := t.TempDir()

	layerYAML := `
services:
    demo-service:
        override: replace
        command: sleep 1000
        startup: enabled
`[1:]
	CreateLayer(t, pebbleDir, "001-simple-layer.yaml", layerYAML)

	logs := PebbleRun(t, pebbleDir)

	expected := []string{
		"Service \"demo-service\" starting",
		"Started default services with change",
	}

	if foundAll, notFound := AllExpectedKeywordsFoundInLogs(logs, expected); !foundAll {
		t.Errorf("Expected keywords not found in logs: %v", notFound)
	}
}
