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

// Command gencertloader generates certloader_unix.go and
// certloader_linux.go in the output directory by extracting and
// transforming certificate loading code from the Go standard library.
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// commentNote records a single mutation: searchFor is a line of the
// formatted output to locate; comment is the original code to insert
// as a comment immediately before that line.
type commentNote struct {
	searchFor string
	comment   string
}

// stmtTransform inspects a statement and, if it matches a pattern,
// returns the comment note for the mutation and the (possibly new)
// replacement statement.
type stmtTransform func(*token.FileSet, ast.Stmt) (commentNote, ast.Stmt, bool)

func main() {
	outDir, err := parseFlags()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	goroot, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error running go env GOROOT: %v\n", err)
		os.Exit(1)
	}
	gorootStr := strings.TrimSpace(string(goroot))

	fileSet := token.NewFileSet()

	unixSrc := filepath.Join(
		gorootStr, "src", "crypto", "x509", "root_unix.go",
	)
	if err := processUnix(fileSet, unixSrc, outDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error processing root_unix.go: %v\n", err)
		os.Exit(1)
	}

	linuxSrc := filepath.Join(
		gorootStr, "src", "crypto", "x509", "root_linux.go",
	)
	if err := processLinux(fileSet, linuxSrc, outDir); err != nil {
		fmt.Fprintf(os.Stderr, "Error processing root_linux.go: %v\n", err)
		os.Exit(1)
	}
}

func parseFlags() (string, error) {
	args := os.Args[1:]
	outDir := "."
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-output":
			if i+1 >= len(args) {
				return "", fmt.Errorf("-output flag requires an argument")
			}
			outDir = args[i+1]
			i++
		default:
			return "", fmt.Errorf("unknown flag: %s", args[i])
		}
	}
	return outDir, nil
}

func processUnix(fset *token.FileSet, srcPath string, outDir string) error {
	src, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	f, err := parser.ParseFile(fset, srcPath, src, parser.ParseComments)
	if err != nil {
		return err
	}

	systemVerifyText := findFuncDeclText(fset, src, f.Decls, "systemVerify")

	var excluded []ast.Node
	if fn := findFuncDecl(f.Decls, "systemVerify"); fn != nil {
		excluded = append(excluded, fn)
	}
	f.Comments = filterComments(f.Comments, fset, excluded)

	f.Name.Name = "httputil"
	f.Decls = removeMethod(f.Decls, "systemVerify")
	addImport(f, "crypto/x509")
	notes, funcStart := fixLoadSystemRoots(fset, f)

	var body strings.Builder
	if err := format.Node(&body, fset, f); err != nil {
		return fmt.Errorf("format certloader_unix.go: %w", err)
	}
	code := postProcessUnix(body.String(), notes, systemVerifyText, funcStart)
	goVersion := goVersion()
	return writeOutputText(
		outDir, "certloader_unix.go", "root_unix.go",
		goVersion, code,
	)
}

func processLinux(fset *token.FileSet, srcPath string, outDir string) error {
	src, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}
	f, err := parser.ParseFile(fset, srcPath, src, parser.ParseComments)
	if err != nil {
		return err
	}

	importsText := findImportDeclText(fset, src, f)
	initText := findFuncDeclText(fset, src, f.Decls, "init")

	var excluded []ast.Node
	if imp := findImportDecl(f); imp != nil {
		excluded = append(excluded, imp)
	}
	if fn := findFuncDecl(f.Decls, "init"); fn != nil {
		excluded = append(excluded, fn)
	}
	f.Comments = filterComments(f.Comments, fset, excluded)

	f.Name.Name = "httputil"
	f.Decls = removeInitFunction(f.Decls)
	removeAllImports(f)

	var body strings.Builder
	if err := format.Node(&body, fset, f); err != nil {
		return fmt.Errorf("format certloader_linux.go: %w", err)
	}
	code := postProcessLinux(
		body.String(), f.Name.Name, importsText, initText,
	)
	goVersion := goVersion()
	return writeOutputText(
		outDir, "certloader_linux.go", "root_linux.go",
		goVersion, code,
	)
}

// goVersion returns the current Go toolchain version string (e.g. "go1.26.3").
func goVersion() string {
	out, err := exec.Command("go", "env", "GOVERSION").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

// formatNode formats any single AST node to trimmed source text.
func formatNode(fset *token.FileSet, node ast.Node) string {
	var buf bytes.Buffer
	_ = format.Node(&buf, fset, node)
	return strings.TrimSpace(buf.String())
}

// firstLine returns the first newline-delimited line of s.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func removeMethod(decls []ast.Decl, name string) []ast.Decl {
	var result []ast.Decl
	for _, decl := range decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == name {
			continue
		}
		result = append(result, decl)
	}
	return result
}

func removeInitFunction(decls []ast.Decl) []ast.Decl {
	var result []ast.Decl
	for _, decl := range decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.Name == "init" {
			continue
		}
		result = append(result, decl)
	}
	return result
}

// fixLoadSystemRoots applies all mutations to the loadSystemRoots
// function. It returns a slice of comment notes (one per mutation)
// and the function-start string used to anchor the systemVerify
// comment block insertion.
func fixLoadSystemRoots(
	fset *token.FileSet, f *ast.File,
) (notes []commentNote, funcStart string) {
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "loadSystemRoots" {
			continue
		}
		funcStart = "func " + fn.Name.Name + "("
		notes = append(notes, fixReturnType(fset, fn)...)
		notes = append(notes, transformBlock(fset, fn.Body, []stmtTransform{
			transformNewCertPool,
			transformAppendCerts,
			transformRootsLen,
		})...)
		// Insert var hasContent bool before the first range statement.
		hasContentVar := &ast.DeclStmt{
			Decl: &ast.GenDecl{
				Tok: token.VAR,
				Specs: []ast.Spec{
					&ast.ValueSpec{
						Names: []*ast.Ident{{Name: "hasContent"}},
						Type:  &ast.Ident{Name: "bool"},
					},
				},
			},
		}
		idx := indexOfFirstRange(fn.Body.List)
		fn.Body.List = append(
			fn.Body.List[:idx],
			append([]ast.Stmt{hasContentVar}, fn.Body.List[idx:]...)...,
		)
		return
	}
	return
}

// fixReturnType changes *CertPool in the results list to *x509.CertPool,
// returning a comment note derived from the actual formatted signature.
func fixReturnType(fset *token.FileSet, fn *ast.FuncDecl) []commentNote {
	if fn.Type.Results == nil {
		return nil
	}
	for _, field := range fn.Type.Results.List {
		star, ok := field.Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		id, ok := star.X.(*ast.Ident)
		if !ok || id.Name != "CertPool" {
			continue
		}
		origSig := firstLine(formatNode(fset, fn))
		star.X = &ast.SelectorExpr{
			X:   &ast.Ident{Name: "x509"},
			Sel: &ast.Ident{Name: "CertPool"},
		}
		newSig := firstLine(formatNode(fset, fn))
		return []commentNote{{searchFor: newSig, comment: origSig}}
	}
	return nil
}

// transformBlock applies each transform to every statement in block,
// recursing into nested block-containing statements.
func transformBlock(
	fset *token.FileSet,
	block *ast.BlockStmt,
	ts []stmtTransform,
) []commentNote {
	var notes []commentNote
	for i, stmt := range block.List {
		for _, t := range ts {
			note, newStmt, ok := t(fset, stmt)
			if !ok {
				continue
			}
			block.List[i] = newStmt
			notes = append(notes, note)
			break
		}
		notes = append(notes, recurseBlocks(fset, block.List[i], ts)...)
	}
	return notes
}

// recurseBlocks recurses transformBlock into the bodies of statements
// that contain nested block statements.
func recurseBlocks(
	fset *token.FileSet,
	stmt ast.Stmt,
	ts []stmtTransform,
) []commentNote {
	switch s := stmt.(type) {
	case *ast.RangeStmt:
		return transformBlock(fset, s.Body, ts)
	case *ast.ForStmt:
		return transformBlock(fset, s.Body, ts)
	case *ast.IfStmt:
		notes := transformBlock(fset, s.Body, ts)
		switch e := s.Else.(type) {
		case *ast.BlockStmt:
			notes = append(notes, transformBlock(fset, e, ts)...)
		case *ast.IfStmt:
			notes = append(notes, recurseBlocks(fset, e, ts)...)
		}
		return notes
	}
	return nil
}

// transformNewCertPool replaces NewCertPool() with x509.NewCertPool(),
// deriving the comment note from the formatted AST before and after.
func transformNewCertPool(
	fset *token.FileSet, stmt ast.Stmt,
) (commentNote, ast.Stmt, bool) {
	assign, ok := stmt.(*ast.AssignStmt)
	if !ok || len(assign.Rhs) != 1 {
		return commentNote{}, nil, false
	}
	call, ok := assign.Rhs[0].(*ast.CallExpr)
	if !ok {
		return commentNote{}, nil, false
	}
	id, ok := call.Fun.(*ast.Ident)
	if !ok || id.Name != "NewCertPool" {
		return commentNote{}, nil, false
	}
	orig := formatNode(fset, stmt)
	call.Fun = &ast.SelectorExpr{
		X:   &ast.Ident{Name: "x509"},
		Sel: &ast.Ident{Name: "NewCertPool"},
	}
	return commentNote{
		searchFor: formatNode(fset, stmt),
		comment:   orig,
	}, stmt, true
}

// transformAppendCerts replaces roots.AppendCertsFromPEM(data) with
// hasContent = roots.AppendCertsFromPEM(data) || hasContent, deriving
// the comment note from the formatted AST before and after.
func transformAppendCerts(
	fset *token.FileSet, stmt ast.Stmt,
) (commentNote, ast.Stmt, bool) {
	exprStmt, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return commentNote{}, nil, false
	}
	call, ok := exprStmt.X.(*ast.CallExpr)
	if !ok || !isAppendCertsCall(call) {
		return commentNote{}, nil, false
	}
	orig := formatNode(fset, exprStmt)
	newStmt := &ast.AssignStmt{
		Lhs: []ast.Expr{&ast.Ident{Name: "hasContent"}},
		Tok: token.ASSIGN,
		Rhs: []ast.Expr{
			&ast.BinaryExpr{
				X:  call,
				Op: token.LOR,
				Y:  &ast.Ident{Name: "hasContent"},
			},
		},
	}
	return commentNote{
		searchFor: formatNode(fset, newStmt),
		comment:   orig,
	}, newStmt, true
}

// transformRootsLen replaces the roots.len() > 0 sub-expression in
// the final if condition with hasContent, deriving the comment note
// from the first line of the formatted if statement before and after.
func transformRootsLen(
	fset *token.FileSet, stmt ast.Stmt,
) (commentNote, ast.Stmt, bool) {
	ifStmt, ok := stmt.(*ast.IfStmt)
	if !ok {
		return commentNote{}, nil, false
	}
	binCond, ok := ifStmt.Cond.(*ast.BinaryExpr)
	if !ok || binCond.Op != token.LOR {
		return commentNote{}, nil, false
	}
	lhs, ok := binCond.X.(*ast.BinaryExpr)
	if !ok || lhs.Op != token.GTR {
		return commentNote{}, nil, false
	}
	call, ok := lhs.X.(*ast.CallExpr)
	if !ok || !isRootsLenCall(call) {
		return commentNote{}, nil, false
	}
	origLine := firstLine(formatNode(fset, ifStmt))
	binCond.X = &ast.Ident{Name: "hasContent"}
	newLine := firstLine(formatNode(fset, ifStmt))
	return commentNote{
		searchFor: newLine,
		comment:   origLine,
	}, stmt, true
}

// indexOfFirstRange returns the index of the first RangeStmt in stmts.
func indexOfFirstRange(stmts []ast.Stmt) int {
	for i, s := range stmts {
		if _, ok := s.(*ast.RangeStmt); ok {
			return i
		}
	}
	return len(stmts)
}

func isAppendCertsCall(call *ast.CallExpr) bool {
	if len(call.Args) != 1 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == "roots" && sel.Sel.Name == "AppendCertsFromPEM"
}

func isRootsLenCall(call *ast.CallExpr) bool {
	if len(call.Args) != 0 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == "roots" && sel.Sel.Name == "len"
}

// filterComments drops comments whose position falls inside one of
// the excluded nodes (i.e. comments belonging to removed declarations).
func filterComments(
	comments []*ast.CommentGroup,
	fset *token.FileSet,
	exclude []ast.Node,
) []*ast.CommentGroup {
	if len(exclude) == 0 {
		return comments
	}
	var result []*ast.CommentGroup
	for _, cg := range comments {
		if len(cg.List) == 0 {
			continue
		}
		pos := fset.Position(cg.Pos()).Offset
		inExcluded := false
		for _, node := range exclude {
			start := fset.Position(node.Pos()).Offset
			end := fset.Position(node.End()).Offset
			if pos >= start && pos < end {
				inExcluded = true
				break
			}
		}
		if !inExcluded {
			result = append(result, cg)
		}
	}
	return result
}

// findFuncDecl returns the named function declaration or nil.
func findFuncDecl(decls []ast.Decl, name string) *ast.FuncDecl {
	for _, decl := range decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == name {
			return fn
		}
	}
	return nil
}

// findImportDecl returns the first import declaration in f or nil.
func findImportDecl(f *ast.File) *ast.GenDecl {
	for _, decl := range f.Decls {
		if g, ok := decl.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
			return g
		}
	}
	return nil
}

// findFuncDeclText returns the source text of the named function
// declaration, or empty string if not found.
func findFuncDeclText(
	fset *token.FileSet, src []byte,
	decls []ast.Decl, name string,
) string {
	for _, decl := range decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != name {
			continue
		}
		start := fset.Position(fn.Pos()).Offset
		end := fset.Position(fn.End()).Offset
		return strings.TrimSpace(string(src[start:end]))
	}
	return ""
}

// findImportDeclText returns the source text of the first import
// declaration in f, or empty string if none.
func findImportDeclText(
	fset *token.FileSet, src []byte, f *ast.File,
) string {
	for _, decl := range f.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.IMPORT {
			continue
		}
		start := fset.Position(genDecl.Pos()).Offset
		end := fset.Position(genDecl.End()).Offset
		return strings.TrimSpace(string(src[start:end]))
	}
	return ""
}

// commentBlock prepends "// " to each non-empty line and "//" to
// empty lines.
func commentBlock(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line == "" {
			lines[i] = "//"
		} else {
			lines[i] = "// " + line
		}
	}
	return strings.Join(lines, "\n")
}

// insertCommentBefore finds each line containing searchFor and inserts
// a comment line (with the same indentation) immediately before it.
func insertCommentBefore(text, searchFor, comment string) string {
	lines := strings.Split(text, "\n")
	var result []string
	for _, line := range lines {
		if strings.Contains(line, searchFor) {
			indent := leadingTabs(line)
			result = append(result, indent+"// "+comment)
		}
		result = append(result, line)
	}
	return strings.Join(result, "\n")
}

// insertBefore inserts ins immediately before the first occurrence of
// before in s.
func insertBefore(s, before, ins string) string {
	idx := strings.Index(s, before)
	if idx < 0 {
		return s
	}
	return s[:idx] + ins + s[idx:]
}

// leadingTabs returns the leading tab characters of s.
func leadingTabs(s string) string {
	trimmed := strings.TrimLeft(s, "\t")
	return s[:len(s)-len(trimmed)]
}

// postProcessUnix inserts comment lines for every mutation recorded
// in notes, and prepends the commented-out systemVerify block before
// the loadSystemRoots function.
func postProcessUnix(
	code string,
	notes []commentNote,
	systemVerifyText, funcStart string,
) string {
	if systemVerifyText != "" && funcStart != "" {
		code = insertBefore(
			code,
			funcStart,
			commentBlock(systemVerifyText)+"\n\n",
		)
	}
	for _, note := range notes {
		code = insertCommentBefore(code, note.searchFor, note.comment)
	}
	return code
}

// postProcessLinux inserts the commented-out import and init() for
// the linux certloader source. The package name is derived from the
// AST rather than hardcoded.
func postProcessLinux(
	code, packageName, importsText, initText string,
) string {
	if importsText != "" {
		code = strings.Replace(
			code,
			"package "+packageName+"\n",
			"package "+packageName+"\n\n"+commentBlock(importsText)+"\n",
			1,
		)
	}
	if initText != "" {
		code = strings.TrimRight(code, "\n") +
			"\n\n" + commentBlock(initText) + "\n"
	}
	return code
}

// addImport adds a new import path to the file's existing import block.
func addImport(f *ast.File, path string) {
	spec := &ast.ImportSpec{
		Path: &ast.BasicLit{
			Kind:  token.STRING,
			Value: `"` + path + `"`,
		},
	}
	f.Imports = append(f.Imports, spec)
	for _, decl := range f.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.IMPORT {
			continue
		}
		genDecl.Specs = append([]ast.Spec{spec}, genDecl.Specs...)
		return
	}
	// No import block exists — create one.
	genDecl := &ast.GenDecl{
		Tok:    token.IMPORT,
		Lparen: 1,
		Specs:  []ast.Spec{spec},
	}
	f.Decls = append([]ast.Decl{genDecl}, f.Decls...)
}

// removeAllImports removes all import declarations from f.
func removeAllImports(f *ast.File) {
	f.Imports = nil
	var decls []ast.Decl
	for _, d := range f.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
			continue
		}
		decls = append(decls, d)
	}
	f.Decls = decls
}

func writeOutputText(
	outDir, outFile, srcFile, ver, code string,
) error {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	var out strings.Builder
	fmt.Fprintf(
		&out,
		"// Code generated by tools/gencertloader"+
			" from crypto/x509/%s (%s); DO NOT EDIT.\n\n",
		srcFile, ver,
	)
	out.WriteString(code)
	return os.WriteFile(
		filepath.Join(outDir, outFile),
		[]byte(out.String()),
		fs.ModePerm,
	)
}
