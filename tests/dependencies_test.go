// Copyright (c) 2026 Canonical Ltd
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
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

// packageInfo represents the relevant fields from `go list -json` output
type packageInfo struct {
	ImportPath   string   `json:"ImportPath"`
	Deps         []string `json:"Deps"`
	Imports      []string `json:"Imports"`
	TestImports  []string `json:"TestImports"`
	XTestImports []string `json:"XTestImports"`
	GoFiles      []string `json:"GoFiles"`
	Dir          string   `json:"Dir"`
}

// TestNoCryptoInProductionCode ensures that production code (non-test) doesn't
// directly import any crypto/* packages from the standard library.
func TestNoCryptoInProductionCode(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "-json", "../...")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to run go list: %v\nOutput: %s", err, output)
	}

	decoder := json.NewDecoder(strings.NewReader(string(output)))

	var violations []string
	packageMap := make(map[string]*packageInfo)
	var allPackages []*packageInfo

	for decoder.More() {
		var pkg packageInfo
		if err := decoder.Decode(&pkg); err != nil {
			t.Fatalf("Failed to decode package info: %v", err)
		}
		allPackages = append(allPackages, &pkg)
		packageMap[pkg.ImportPath] = &pkg
	}

	for _, pkg := range allPackages {
		// Only check packages from this module (not stdlib or third-party)
		if !strings.HasPrefix(pkg.ImportPath, "github.com/canonical/pebble") {
			continue
		}

		// Skip test packages (packages ending with _test or containing test files)
		if strings.HasSuffix(pkg.ImportPath, "_test") {
			continue
		}

		// Skip if this is a test-only package (has test imports but is internal test package)
		if strings.Contains(pkg.ImportPath, "/testutil") ||
			strings.Contains(pkg.ImportPath, "/tests") {
			continue
		}

		// Check if this package directly imports crypto/x509
		for _, imp := range pkg.Imports {
			if strings.HasPrefix(imp, "crypto/") {
				violations = append(violations, pkg.ImportPath+" imports "+imp)
				t.Logf("Package %s directly imports %s", pkg.ImportPath, imp)
				if len(pkg.GoFiles) > 0 {
					t.Logf("  Files in %s: %v", pkg.Dir, pkg.GoFiles)
				}
			}
		}
	}

	if len(violations) > 0 {
		t.Errorf("The following packages directly import crypto/* packages in production code:\n%s",
			strings.Join(violations, "\n"))
	}
}

// TestNoNewCryptoInThirdPartyDeps ensures that third-party dependencies (outside stdlib
// and websocket) don't directly import crypto/* packages. This catches new dependencies
// that might introduce crypto usage.
func TestNoNewCryptoInThirdPartyDeps(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "-json", "../...")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Failed to run go list: %v\nOutput: %s", err, output)
	}

	decoder := json.NewDecoder(strings.NewReader(string(output)))

	allowedCryptoDeps := map[string]bool{
		"github.com/gorilla/websocket":       true, // Uses crypto/tls, crypto/rand, crypto/sha1
		"github.com/canonical/x-go/randutil": true, // Uses crypto/rand, crypto/sha256
	}

	var violations []string
	var allPackages []*packageInfo

	for decoder.More() {
		var pkg packageInfo
		if err := decoder.Decode(&pkg); err != nil {
			t.Fatalf("Failed to decode package info: %v", err)
		}
		allPackages = append(allPackages, &pkg)
	}

	for _, pkg := range allPackages {
		// Go standard library packages
		if !strings.Contains(pkg.ImportPath, ".") {
			continue
		}

		// Go standard library packages included in the tool chain
		if strings.HasPrefix(pkg.ImportPath, "vendor/golang.org/") {
			continue
		}

		// Go first party packages
		if strings.HasPrefix(pkg.ImportPath, "golang.org/x/") {
			continue
		}

		if strings.HasPrefix(pkg.ImportPath, "github.com/canonical/pebble") {
			continue
		}

		if strings.HasSuffix(pkg.ImportPath, "_test") {
			continue
		}

		if allowedCryptoDeps[pkg.ImportPath] {
			continue
		}

		for _, imp := range pkg.Imports {
			if strings.HasPrefix(imp, "crypto/") {
				violations = append(violations, pkg.ImportPath+" imports "+imp)
				t.Logf("Third-party package %s directly imports %s", pkg.ImportPath, imp)
				if len(pkg.GoFiles) > 0 {
					t.Logf("  Files in %s: %v", pkg.Dir, pkg.GoFiles)
				}
			}
		}
	}

	if len(violations) > 0 {
		t.Errorf("The following third-party packages directly import crypto/* packages:\n%s\n\n"+
			"If this is expected, add the package to allowedCryptoDeps in the test.",
			strings.Join(violations, "\n"))
	}
}
