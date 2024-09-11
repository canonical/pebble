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
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPebbleRunNormal(t *testing.T) {
	pebbleDir := t.TempDir()

	layerYAML := `
services:
    svc1:
        override: replace
        command: {{.svc1Cmd}}
        startup: enabled
    svc2:
        override: replace
        command: {{.svc2Cmd}}
        startup: enabled
`
	svc1Cmd := fmt.Sprintf("touch %s ; sleep 1000", filepath.Join(pebbleDir, "svc1"))
	svc2Cmd := fmt.Sprintf("touch %s ; sleep 1000", filepath.Join(pebbleDir, "svc2"))
	layerYAML = strings.Replace(layerYAML, "{{.svc1Cmd}}", svc1Cmd, -1)
	layerYAML = strings.Replace(layerYAML, "{{.svc2Cmd}}", svc2Cmd, -1)

	createLayer(t, pebbleDir, "001-simple-layer.yaml", layerYAML)

	_ = pebbleRun(t, pebbleDir)

	expectedServices := []string{"svc1", "svc2"}
	waitForServices(t, pebbleDir, expectedServices, time.Second*3)
}
