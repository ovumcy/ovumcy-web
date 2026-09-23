package services

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// Barrier for the recovery-lookup timing oracle.
//
// FindUserByEmailRecoveryCodeAndPassword verifies TWO secrets — the account's
// recovery code and its password — and every rejection collapses to one
// ErrRecoveryCodeNotFound. That single error shape is only half of "the failure
// paths are indistinguishable": if the password compare is skipped once the
// recovery-code compare has already failed, a rejection costs one cost-12
// bcrypt comparison where a wrong password on a real account costs two, and the
// response time tells the attacker which operand they got right — and, through
// the code operand, whether the account exists at all (CWE-208 / CWE-204).
//
// The property is therefore "both comparisons always run, and only their
// combined result decides". A wall-clock budget would pin it flakily on shared
// CI runners, so this reads the shipped source instead: a comparison written as
// an `if` CONDITION is a short-circuit by construction, while one written into
// a variable cannot return before the next statement runs. The early-return
// equalizer, which must spend BOTH operands' compute, spends through
// authTimingEqualizerCompare and is observed at runtime by the test below.
//
// What it cannot see: a `return` inserted between the two assignments, or a
// compare hidden behind a helper it does not name. Neither is invisible to the
// enumeration guards in internal/api, which compare the answers themselves. The
// two helpers this route DOES spend bcrypt work through are named and counted
// at the end of the test.
func TestRecoveryLookupSpendsBothCredentialComparesWithoutShortCircuit(t *testing.T) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "auth_service.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse auth_service.go: %v", err)
	}

	bodies := map[string]*ast.BlockStmt{}
	ast.Inspect(file, func(node ast.Node) bool {
		decl, ok := node.(*ast.FuncDecl)
		if !ok || decl.Body == nil {
			return true
		}
		bodies[decl.Name.Name] = decl.Body
		return true
	})

	body, ok := bodies["FindUserByEmailRecoveryCodeAndPassword"]
	if !ok {
		t.Fatal("FindUserByEmailRecoveryCodeAndPassword is missing from auth_service.go — the recovery reset must verify both operands")
	}
	compares := 0
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if ok && isBcryptCompareCall(call) {
			compares++
		}
		return true
	})
	if compares != 2 {
		t.Fatalf("FindUserByEmailRecoveryCodeAndPassword performs %d bcrypt comparisons, want 2 (recovery code AND password)", compares)
	}

	ast.Inspect(bodies["FindUserByEmailRecoveryCodeAndPassword"], func(node ast.Node) bool {
		ifStmt, ok := node.(*ast.IfStmt)
		if !ok || ifStmt.Cond == nil {
			return true
		}
		found := false
		ast.Inspect(ifStmt.Cond, func(inner ast.Node) bool {
			call, ok := inner.(*ast.CallExpr)
			if ok && isBcryptCompareCall(call) {
				found = true
			}
			return true
		})
		if found {
			t.Fatalf("FindUserByEmailRecoveryCodeAndPassword compares a credential inside an if-condition at %s: "+
				"that short-circuits the other operand's bcrypt work and reintroduces the recovery timing oracle. "+
				"Assign each comparison, then decide on the combined result",
				fileSet.Position(ifStmt.Pos()))
		}
		return true
	})

	// The inline comparisons above are only half the story: two rejection paths
	// spend their bcrypt work through a helper instead, and deleting either call
	// is invisible both to a compare count and to the work ledgers in
	// auth_service_timing_cost_topup_test.go, which read the unknown-address
	// baseline off the placeholder constants rather than measuring it. Pin the
	// calls themselves. Two equalizer calls: the unknown-address branch and the
	// no-local-auth/empty-hash branch. One top-up call: the branch where both
	// comparisons ran, against stored hashes that may predate passwordHashCost.
	helperCalls := map[string]int{}
	ast.Inspect(bodies["FindUserByEmailRecoveryCodeAndPassword"], func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ident, ok := call.Fun.(*ast.Ident); ok {
			helperCalls[ident.Name]++
		}
		return true
	})

	for _, want := range []struct {
		helper string
		calls  int
		why    string
	}{
		{"equalizeRecoveryCodeLookupTiming", 2, "an unknown address would then be refused with no bcrypt work at all — the loudest account-enumeration signal this route can emit"},
		{"topUpRecoveryLookupTiming", 1, "a row whose stored hashes predate passwordHashCost would then be refused more cheaply than an unknown address"},
	} {
		if got := helperCalls[want.helper]; got != want.calls {
			t.Fatalf("FindUserByEmailRecoveryCodeAndPassword calls %s %d times, want %d: %s",
				want.helper, got, want.calls, want.why)
		}
	}
}

func isBcryptCompareCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel == nil || selector.Sel.Name != "CompareHashAndPassword" {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && pkg.Name == "bcrypt"
}

// TestRecoveryLookupEqualizerComparesEachPlaceholderAgainstItsOwnOperand is
// TS-M06's guard on the shipped equalizer body: it records, through
// authTimingEqualizerCompare, every (hash, operand) pair the body actually
// hands to bcrypt. A count alone is satisfied by a body that compares the code
// against credentialsTimingEqualizationHash and the password against
// recoveryCodeTimingEqualizationHash: two comparisons still run, but they no
// longer mirror the real two-secret compare each placeholder stands in for —
// including if the placeholders' costs ever diverge (pinned equal today by
// TestTimingEqualizationHashesMatchTargetCost, not by this test). The code is
// submitted un-normalized so a body that skips NormalizeRecoveryCode fails
// too. No wall-clock threshold is involved.
func TestRecoveryLookupEqualizerComparesEachPlaceholderAgainstItsOwnOperand(t *testing.T) {
	const submittedCode = "  ovm-abcd-efgh-jklm "
	const submittedPassword = "WrongGuess1!"
	normalizedCode := NormalizeRecoveryCode(submittedCode)
	if normalizedCode == submittedCode {
		t.Fatalf("NormalizeRecoveryCode left %q unchanged — the fixture no longer tells a normalized operand from a raw one", submittedCode)
	}

	recorded := withEqualizerCompareRecorder(t)
	equalizeRecoveryCodeLookupTiming(submittedCode, submittedPassword)

	want := []equalizerCompare{
		{hash: recoveryCodeTimingEqualizationHash, operand: normalizedCode},
		{hash: credentialsTimingEqualizationHash, operand: submittedPassword},
	}
	if len(*recorded) != len(want) {
		t.Fatalf("equalizeRecoveryCodeLookupTiming spent %d bcrypt comparisons, want %d — an early return that spends less than "+
			"the real two-secret compare is the account-enumeration oracle it exists to close", len(*recorded), len(want))
	}
	for index, wantCompare := range want {
		got := (*recorded)[index]
		if got != wantCompare {
			t.Fatalf("comparison %d ran %q against operand %q, want %q against %q — the wrong operand against a placeholder "+
				"still counts as a compare, but no longer mirrors the real compare that placeholder stands in for",
				index, hashPrefix(got.hash), got.operand, hashPrefix(wantCompare.hash), wantCompare.operand)
		}
	}
}
