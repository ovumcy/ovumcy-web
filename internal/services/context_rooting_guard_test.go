package services

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// detachesFromTheCaller names the context constructors that leave a call
// unreachable by the caller's deadline. Background and TODO are the obvious
// pair. WithoutCancel is the one worth spelling out: it keeps the values and
// drops cancellation and the deadline, so it reads like careful context
// threading while doing exactly what the other two do — and a boot pass written
// with it would ignore its storage budget while looking correct.
var detachesFromTheCaller = map[string]bool{
	"Background":    true,
	"TODO":          true,
	"WithoutCancel": true,
}

// TestServicesNeverReRootAContext refuses every constructor in
// detachesFromTheCaller anywhere in this package's production files, and
// refuses an aliased or dot import of context on top — without which the
// spelling the call check matches on would be an assumption rather than a
// property.
//
// It is the middle link of the chain the boot-pass storage budget rides on.
// cmd/ovumcy bounds each boot pass with a deadline and a guard there proves the
// pass RECEIVES it; internal/db proves a cancelled context aborts the query. A
// service in between that re-rooted its context would break the chain without
// breaking either of those tests: the deadline would never reach storage, a
// stuck database would hang the boot exactly as it did before the budget
// existed, and every test would stay green.
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

	// message is the WHOLE sentence, not a fragment sharing a suffix. The two
	// kinds this sweep reports have different consequences — a re-rooting call
	// drops a deadline, an aliased import only hides one from the check — and a
	// shared tail would accuse the second of the first's damage.
	type violation struct {
		file    string
		line    int
		message string
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

		// The detection below keys on the spelling `context.X`, so first make
		// that spelling a property this guard enforces rather than one it
		// assumes: an aliased or dot import would let a re-rooting call through
		// under a different name. A rule keyed on a spelling is not keyed on the
		// thing, and the tell is a class that grows by one pattern per audit.
		for _, imported := range file.Imports {
			if imported.Path == nil || imported.Path.Value != `"context"` {
				continue
			}
			if imported.Name != nil {
				violations = append(violations, violation{
					file:    path,
					line:    fileSet.Position(imported.Pos()).Line,
					message: "imports context under the name " + imported.Name.Name + " — this is not itself a dropped deadline, but it puts every call in this file beyond the spelling the check below matches on, so import context under its own name",
				})
			}
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
			if !detachesFromTheCaller[selector.Sel.Name] {
				return true
			}
			violations = append(violations, violation{
				file:    path,
				line:    fileSet.Position(call.Pos()).Line,
				message: "re-roots the context with context." + selector.Sel.Name + "() — a boot pass reached through here would ignore its storage budget, and a request-scoped call would ignore its cancellation; take a ctx parameter instead",
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
		t.Errorf("%s:%d %s", found.file, found.line, found.message)
	}
}
