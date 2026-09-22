package db

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"
)

// scopedUserUpdateBuilders are the only two functions in user_repository.go
// allowed to build a users-table UPDATE query's bare `id = ?` clause. Both
// refuse a zero id (ErrUserOwnerRequired) before returning the query, which is
// exactly the property this guard exists to keep true of every OTHER function
// in the file: none of them may construct that same
// `Model(&models.User{}).Where("id = ?", userID)` shape on their own, because
// doing so would skip the refusal.
var scopedUserUpdateBuilders = map[string]bool{
	"scopedUserUpdate":   true,
	"scopedUserUpdateTx": true,
}

// TestUserRepositoryUpdatesGoThroughTheScopingHelper is the completeness
// guard the zero-id refusal depends on. The privacy boundary treats an
// absent or zero id as invalid input, never as a wildcard: a raw
// `Model(&models.User{}).Where("id = ?", userID)` (bare, or compounded with
// extra predicates as `"id = ? AND ..."`) built outside a guarded shape would
// let a zero id reach the database unrefused. Two shapes are accepted for a
// function that builds such a clause:
//
//  1. It is scopedUserUpdate/scopedUserUpdateTx themselves (the bare clause's
//     only builders).
//  2. It calls requireUserOwnerID directly before building the query, OR it
//     inspects RowsAffected from the query's result and reports a zero-row
//     outcome to its caller (a compare-and-set write whose zero-row result is
//     legitimately ambiguous between "no owner" and "predicate didn't match",
//     and which already surfaces that ambiguity honestly instead of
//     swallowing it).
//
// A function that builds the clause and does neither is a silent-success
// hole: a zero id matches zero rows, the write reports success, and nothing
// downstream notices.
//
// The set of users-table UPDATE builders is derived from the file's own AST
// rather than re-listed: a hand-written mirror of the writers would agree
// with itself while a new writer went unguarded.
func TestUserRepositoryUpdatesGoThroughTheScopingHelper(t *testing.T) {
	const path = "user_repository.go"

	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, path, source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	var offenders []string
	var bareBuilderHits = map[string]bool{}
	var compoundSites []string

	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		name := function.Name.Name

		var buildsIDClause bool
		var isBareClause bool
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Where" {
				return true
			}
			if len(call.Args) == 0 {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			// literal.Value carries the Go source quoting, e.g. `"id = ?"`.
			unquoted := strings.Trim(literal.Value, `"`)
			if unquoted == "id = ?" {
				buildsIDClause = true
				isBareClause = true
			} else if strings.HasPrefix(unquoted, "id = ? AND ") {
				buildsIDClause = true
			}
			return true
		})
		if !buildsIDClause {
			continue
		}

		if scopedUserUpdateBuilders[name] {
			// The builders themselves are the one place the bare shape is
			// allowed to appear.
			bareBuilderHits[name] = true
			continue
		}

		if isBareClause {
			// A bare `Where("id = ?", userID)` outside the two builders:
			// whatever query it is chained off of (repo.database or a
			// transaction's tx), it built the scoped clause without the
			// zero-id refusal.
			offenders = append(offenders, name)
			continue
		}

		// Compound clause outside the two builders: acceptable only if the
		// function either calls requireUserOwnerID directly, or inspects
		// RowsAffected (and so already reports a zero-row outcome honestly).
		compoundSites = append(compoundSites, name)
		var callsGuard bool
		var checksRowsAffected bool
		ast.Inspect(function.Body, func(node ast.Node) bool {
			if ident, ok := node.(*ast.Ident); ok && ident.Name == "requireUserOwnerID" {
				callsGuard = true
			}
			if selector, ok := node.(*ast.SelectorExpr); ok && selector.Sel.Name == "RowsAffected" {
				checksRowsAffected = true
			}
			return true
		})
		if !callsGuard && !checksRowsAffected {
			offenders = append(offenders, name)
		}
	}

	// Anti-vacuity: the scan must find the two builders' own occurrences of
	// the bare literal, or it is not looking at the file it claims to.
	for name := range scopedUserUpdateBuilders {
		if !bareBuilderHits[name] {
			t.Fatalf("the scan did not find %s building the scoped WHERE clause: it is not measuring what it claims", name)
		}
	}
	// Anti-vacuity: the scan must find at least one compound-predicate site,
	// or the compound branch above is dead code that never runs.
	if len(compoundSites) == 0 {
		t.Fatalf("the scan found no compound `id = ? AND ...` site: the compound-clause branch is unexercised")
	}

	sort.Strings(offenders)
	if len(offenders) > 0 {
		t.Fatalf("these functions build a users-table UPDATE scoped by a raw `Where(\"id = ?\", ...)` (bare or compounded) without going through scopedUserUpdate/scopedUserUpdateTx, calling requireUserOwnerID, or checking RowsAffected, so a zero id would reach it unrefused: %v", offenders)
	}
}
