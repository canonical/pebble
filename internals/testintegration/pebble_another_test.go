//go:build integration

package testintegration_test

import (
	"testing"

	. "github.com/canonical/pebble/internals/testintegration"
)

func TestPebbleSomethingElse(t *testing.T) {
	pebbleDir := t.TempDir()
	CreateLayer(t, pebbleDir, "001-simple-layer.yaml", DefaultLayerYAML)
	_ = PebbleRun(t, pebbleDir)
	// do something
}
