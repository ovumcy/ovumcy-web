package services

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"
)

// packageFunctionDecls parses every non-test .go file of this package and hands
// each top-level function declaration to visit, named as the reader sees it.
// Both guards below are about the SHAPE of the package's own source — "there is
// one of these, not two" — which no behavioral assertion can express, so they
// read the source the same way a reviewer would.
func packageFunctionDecls(t *testing.T, visit func(name string, decl *ast.FuncDecl)) {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the services package directory: %v", err)
	}

	fileSet := token.NewFileSet()
	seen := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fileSet, name, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}
		for _, decl := range file.Decls {
			function, isFunction := decl.(*ast.FuncDecl)
			if !isFunction || function.Body == nil {
				continue
			}
			seen++
			visit(function.Name.Name, function)
		}
	}
	if seen == 0 {
		t.Fatal("no function declarations found; the guards below would pass vacuously")
	}
}

// TestOneURLHostRedactionHelper is the R2-0103 regression. A webhook URL may
// embed a notification token, so "the hostname and nothing else" is the single
// redaction rule that decides what may reach an operator's screen or log. The
// package had TWO byte-identical implementations of it under different names —
// hostOnly (notify pass + CLI) and webhookURLHost (the settings display) — which
// means the next hardening of that rule has two places to reach and only one
// obvious one. Detected by shape, not by name: any function that parses a URL
// and returns its Hostname() is that helper, whatever it is called.
func TestOneURLHostRedactionHelper(t *testing.T) {
	helpers := []string{}
	packageFunctionDecls(t, func(name string, decl *ast.FuncDecl) {
		if returnsParsedHostname(decl) {
			helpers = append(helpers, name)
		}
	})
	sort.Strings(helpers)

	if len(helpers) != 1 || helpers[0] != "hostOnly" {
		t.Fatalf("URL-hostname redaction helpers in package services = %v, want exactly [hostOnly]: "+
			"a second copy of the rule means a hardening applied to one leaves the other", helpers)
	}
}

// returnsParsedHostname reports whether a function's body returns the hostname
// of a parsed URL directly (`return parsed.Hostname()`) — the redaction helper's
// signature move. A Hostname() call fed to a log line or an error message is a
// USE of the rule, not another copy of it, so only a bare return counts.
func returnsParsedHostname(decl *ast.FuncDecl) bool {
	found := false
	ast.Inspect(decl.Body, func(node ast.Node) bool {
		result, isReturn := node.(*ast.ReturnStmt)
		if !isReturn || len(result.Results) != 1 {
			return true
		}
		call, isCall := result.Results[0].(*ast.CallExpr)
		if !isCall {
			return true
		}
		selector, isSelector := call.Fun.(*ast.SelectorExpr)
		if isSelector && selector.Sel.Name == "Hostname" {
			found = true
		}
		return true
	})
	return found
}

// TestNotifyPassRunsTheDecisionOncePerOwner is the R2-0110 regression. The
// decision is not cheap: every call rebuilds the owner's cycle statistics from
// their entire logged history, which the pass reads unbounded. The pass used to
// run it TWICE per owner — once for the authoritative due set and once more,
// with the watermarks cleared, purely to populate report.SkippedIdempotent — so
// the cost of the observability counter equalled the cost of the work it
// reports. One traversal now yields both, and this guard keeps it that way by
// naming every non-test function that enters the decision.
func TestNotifyPassRunsTheDecisionOncePerOwner(t *testing.T) {
	callers := map[string]int{}
	packageFunctionDecls(t, func(name string, decl *ast.FuncDecl) {
		// The exported wrapper exists to call the traversal; it is not a second
		// traversal of its own.
		if name == "DecideDueReminders" {
			return
		}
		if calls := countDecisionCalls(decl); calls > 0 {
			callers[name] = calls
		}
	})

	if len(callers) != 1 || callers["processOwner"] != 1 {
		names := make([]string, 0, len(callers))
		for name, calls := range callers {
			names = append(names, name)
			if calls > 1 {
				t.Errorf("%s enters the reminder decision %d times; one traversal must yield both the due set and the suppressed count", name, calls)
			}
		}
		sort.Strings(names)
		t.Fatalf("functions entering the reminder decision = %v, want exactly [processOwner]", names)
	}
}

// countDecisionCalls counts the calls a function makes into the reminder
// decision under either of its names — the exported entry point and the
// single-traversal form the notify pass consumes.
func countDecisionCalls(decl *ast.FuncDecl) int {
	calls := 0
	ast.Inspect(decl.Body, func(node ast.Node) bool {
		call, isCall := node.(*ast.CallExpr)
		if !isCall {
			return true
		}
		if identifier, isIdentifier := call.Fun.(*ast.Ident); isIdentifier {
			if identifier.Name == "DecideDueReminders" || identifier.Name == "decideDueReminders" {
				calls++
			}
		}
		return true
	})
	return calls
}
