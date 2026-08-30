package services

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestServicesNeverReRootAContext refuses context.Background and context.TODO
// anywhere in this package's production files.
//
// It is the middle link of the chain the boot-pass storage budget rides on.
// cmd/ovumcy bounds each boot pass with a deadline and a guard there proves the
// pass RECEIVES it; internal/db proves a cancelled context aborts the query. A
// service in between that re-rooted to context.Background would break the chain
// without breaking either of those tests: the deadline would never reach
// storage, a stuck database would hang the boot exactly as it did before the
// budget existed, and every test would stay green.
//
// The rule is not new, only unenforced here. persistence.md already requires a
// ctx parameter over context.Background for repositories, and this package held
// to it on its own — the sweep found zero occurrences when it was written, which
// is why it can be an outright refusal rather than an allowlist. A service that
// genuinely needs a detached context (a goroutine outliving its request) has to
// say so here, and that is the point: it becomes a decision someone makes on
// purpose rather than a line that slips in.
func TestServicesNeverReRootAContext(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob package files: %v", err)
	}

	type violation struct {
		file string
		line int
		call string
	}
	var violations []violation
	walked := 0
	sawKnownFile := false

	fileSet := token.NewFileSet()
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		walked++
		if path == "luteal_phase_recompute.go" {
			sawKnownFile = true
		}

		file, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := selector.X.(*ast.Ident)
			if !ok || pkg.Name != "context" {
				return true
			}
			if selector.Sel.Name != "Background" && selector.Sel.Name != "TODO" {
				return true
			}
			violations = append(violations, violation{
				file: path,
				line: fileSet.Position(call.Pos()).Line,
				call: "context." + selector.Sel.Name + "()",
			})
			return true
		})
	}

	// The floor. A sweep that parsed nothing and a sweep that found nothing
	// print the same nothing, and a glob that silently matched no files is the
	// ordinary way the first happens.
	const minimumFiles = 20
	if walked < minimumFiles {
		t.Fatalf("walked only %d production file(s); the sweep is too small to have measured anything", walked)
	}
	if !sawKnownFile {
		t.Fatal("luteal_phase_recompute.go was not among the files walked; the sweep is not reading the package it claims to")
	}

	for _, found := range violations {
		t.Errorf("%s:%d re-roots the context with %s — a boot pass reached through here would ignore its storage budget, and a request-scoped call would ignore its cancellation", found.file, found.line, found.call)
	}
}
