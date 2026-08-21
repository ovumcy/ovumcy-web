package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// Shown-once secret reveals — the sweep that keeps the set closed.
//
// Two surfaces put a secret in front of the owner exactly once: the
// recovery-code reveal (a dedicated page plus the inline post-registration
// block) and the calendar-feed subscribe URL. Each opens a sealed one-time
// cookie through a reader in this package, and each is made single use by
// claiming the owner's server-side consumption mark (migration 036) before it
// renders anything.
//
// The behavioural guards live with their surfaces
// (TestRecoveryCodeRevealRefusesAReplayedCookieAndRearmsOnRegenerate,
// TestRecoveryCodeRevealRefusesAReplayedInlineCookie,
// TestCalendarFeedRevealRefusesAReplayedCookieAndRearmsOnRotate). They prove the
// three reveal sites that exist today claim; they say nothing about a fourth.
// This file is the half they cannot cover: it derives the reveal sites from the
// package source, so a handler added later is judged by the same rule instead of
// inheriting silence. It stays single-purpose for the reason the codec
// invariants do — it is a narrow structural guard, not a page regression.
//
// A cookie reader that could be used without claiming would not fail to compile
// and would not fail any page test: it would simply reveal the secret again,
// which is the whole defect this mark exists to close.

// revealClaimPairings maps each sealed reveal-cookie reader to the claim a
// function using it must also perform. No allowlist: a reader added to this map,
// or a function added to the package, is covered without an exemption to write.
var revealClaimPairings = map[string]string{
	"readRecoveryCodeDisplayState": "claimRecoveryCodeReveal",
	"readCalendarFeedRevealState":  "ClaimFeedReveal",
}

// TestEverySecretRevealClaimsItsConsumptionMark requires every production
// function that opens a shown-once reveal cookie to claim the matching mark in
// the same function.
func TestEverySecretRevealClaimsItsConsumptionMark(t *testing.T) {
	assertRevealClaimSweepAnswersBothWays(t)

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fileSet := token.NewFileSet()
	revealSites := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fileSet, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			called := calledFunctionNames(function.Body)
			for reader, claim := range revealClaimPairings {
				if !called[reader] {
					continue
				}
				revealSites++
				if !called[claim] {
					t.Errorf(
						"%s: %s opens a shown-once reveal cookie and must call %s in the same function — clearing the cookie asks a browser to forget the value and does not bind a client that kept it",
						fileSet.Position(function.Pos()), function.Name.Name, claim,
					)
				}
			}
		}
	}

	if revealSites == 0 {
		t.Fatal("the sweep found no reveal site at all — it is measuring nothing, so the pairing it claims to enforce is unproven")
	}
}

// calledFunctionNames collects the identifier every call in a function body
// dispatches on, taking the selector's final name so `handler.claimX(...)` and
// `handler.svc.ClaimY(...)` both register under the name that carries the
// meaning.
func calledFunctionNames(body *ast.BlockStmt) map[string]bool {
	called := make(map[string]bool)
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch function := call.Fun.(type) {
		case *ast.Ident:
			called[function.Name] = true
		case *ast.SelectorExpr:
			called[function.Sel.Name] = true
		}
		return true
	})
	return called
}

// assertRevealClaimSweepAnswersBothWays anchors the sweep on fixtures it owns —
// one function body that must be read as claiming and one that must not — so a
// calledFunctionNames that stopped recognising calls could not report success
// over a package it no longer understands.
func assertRevealClaimSweepAnswersBothWays(t *testing.T) {
	t.Helper()

	claiming := parseFunctionBodyForTest(t, `
		state := handler.readCalendarFeedRevealState(c, user.ID)
		claimed, err := handler.calendarFeedSettings.ClaimFeedReveal(c.Context(), user.ID, time.Now())
		_, _, _ = state, claimed, err
	`)
	if names := calledFunctionNames(claiming); !names["readCalendarFeedRevealState"] || !names["ClaimFeedReveal"] {
		t.Fatal("the sweep must see both the reader and the claim in a compliant body")
	}

	unclaiming := parseFunctionBodyForTest(t, `
		state := handler.readCalendarFeedRevealState(c, user.ID)
		handler.clearCalendarFeedRevealCookie(c)
		_ = state
	`)
	if names := calledFunctionNames(unclaiming); !names["readCalendarFeedRevealState"] || names["ClaimFeedReveal"] {
		t.Fatal("the sweep must see a body that only retracts the cookie as NOT claiming")
	}
}

func parseFunctionBodyForTest(t *testing.T, body string) *ast.BlockStmt {
	t.Helper()

	parsed, err := parser.ParseFile(token.NewFileSet(), "fixture.go", "package fixture\nfunc fixture() {\n"+body+"\n}\n", 0)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	function, ok := parsed.Decls[0].(*ast.FuncDecl)
	if !ok {
		t.Fatal("fixture is not a function declaration")
	}
	return function.Body
}
