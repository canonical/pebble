// Copyright (c) 2025 Canonical Ltd
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

package testutil_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHTTPClientCheckerSelf is a self-test that verifies the static analyzer
// correctly identifies http.Client instances with and without CheckRedirect.
func TestHTTPClientCheckerSelf(t *testing.T) {
	testdataDir := "./testdata"

	goodFile := filepath.Join(testdataDir, "good_client.go")
	badFile := filepath.Join(testdataDir, "bad_client.go")

	// Test that good_client.go has no violations
	goodViolations := checkHTTPClientsInFile(t, goodFile)
	if len(goodViolations) != 0 {
		t.Errorf("Expected no violations in good_client.go, but found %d", len(goodViolations))
	}

	// Test that bad_client.go has exactly 1 violation
	badViolations := checkHTTPClientsInFile(t, badFile)
	if len(badViolations) != 1 {
		t.Errorf("Expected 1 violation in bad_client.go, but found %d", len(badViolations))
	}
}

// checkHTTPClientsInFile checks a single file for http.Client violations
func checkHTTPClientsInFile(t *testing.T, path string) []string {
	var violations []string

	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse %s: %v", path, err)
	}

	ast.Inspect(node, func(n ast.Node) bool {
		compLit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}

		// Check if this is http.Client or *http.Client
		var isHTTPClient bool
		switch typ := compLit.Type.(type) {
		case *ast.SelectorExpr:
			if ident, ok := typ.X.(*ast.Ident); ok {
				if ident.Name == "http" && typ.Sel.Name == "Client" {
					isHTTPClient = true
				}
			}
		}

		if !isHTTPClient {
			return true
		}

		// Check if CheckRedirect field is set
		hasCheckRedirect := false
		for _, elt := range compLit.Elts {
			if kv, ok := elt.(*ast.KeyValueExpr); ok {
				if key, ok := kv.Key.(*ast.Ident); ok {
					if key.Name == "CheckRedirect" {
						hasCheckRedirect = true
						break
					}
				}
			}
		}

		if !hasCheckRedirect {
			pos := fset.Position(compLit.Pos())
			violations = append(violations, path+":"+pos.String())
		}

		return true
	})

	return violations
}

// TestAllHTTPClientsHaveRedirectCheck ensures that all http.Client instances
// in the codebase have a CheckRedirect function configured. This is important
// for security to prevent unintended redirects.
func TestAllHTTPClientsHaveRedirectCheck(t *testing.T) {
	root := "../.." // Go up to pebble root
	var violations []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip non-Go files, test files, testdata, and vendor/generated code
		if !strings.HasSuffix(path, ".go") ||
			strings.Contains(path, "/vendor/") ||
			strings.Contains(path, "/.git/") ||
			strings.Contains(path, "/dist/") ||
			strings.Contains(path, "/testdata/") {
			return nil
		}

		// Parse the file
		fset := token.NewFileSet()
		node, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			// Skip files that can't be parsed (might be build-tagged for other platforms)
			return nil
		}

		// Find all composite literals that create http.Client
		ast.Inspect(node, func(n ast.Node) bool {
			compLit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}

			// Check if this is http.Client or *http.Client
			var isHTTPClient bool
			switch typ := compLit.Type.(type) {
			case *ast.SelectorExpr:
				// http.Client
				if ident, ok := typ.X.(*ast.Ident); ok {
					if ident.Name == "http" && typ.Sel.Name == "Client" {
						isHTTPClient = true
					}
				}
			}

			if !isHTTPClient {
				return true
			}

			// Check if CheckRedirect field is set
			hasCheckRedirect := false
			for _, elt := range compLit.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					if key, ok := kv.Key.(*ast.Ident); ok {
						if key.Name == "CheckRedirect" {
							hasCheckRedirect = true
							break
						}
					}
				}
			}

			if !hasCheckRedirect {
				pos := fset.Position(compLit.Pos())
				relPath, _ := filepath.Rel(root, path)
				violations = append(violations, relPath+":"+pos.String())
			}

			return true
		})

		return nil
	})

	if err != nil {
		t.Fatalf("Error walking directory: %v", err)
	}

	if len(violations) > 0 {
		t.Errorf("Found %d http.Client instances without CheckRedirect configured:\n", len(violations))
		for _, v := range violations {
			t.Errorf("  - %s", v)
		}
		t.Error("\nAll http.Client instances must have CheckRedirect configured to control redirect behavior.")
	}
}
