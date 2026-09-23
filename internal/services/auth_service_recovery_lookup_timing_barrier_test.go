package services

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"testing"

	"golang.org/x/crypto/bcrypt"
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
// a variable cannot return before the next statement runs. The same applies to
// the early-return equalizer, which must spend BOTH operands' compute.
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

	for _, functionName := range []string{"FindUserByEmailRecoveryCodeAndPassword", "equalizeRecoveryCodeLookupTiming"} {
		body, ok := bodies[functionName]
		if !ok {
			t.Fatalf("%s is missing from auth_service.go — the recovery reset must verify both operands", functionName)
		}

		compares := 0
		ast.Inspect(body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || !isBcryptCompareCall(call) {
				return true
			}
			compares++
			return true
		})
		if compares != 2 {
			t.Fatalf("%s performs %d bcrypt comparisons, want 2 (recovery code AND password)", functionName, compares)
		}
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

// printExpr renders an AST expression back to source text, so a test can
// compare what argument a call actually names without hand-walking its node
// shape (an Ident for a bare name, a nested CallExpr for a wrapped one).
func printExpr(t *testing.T, fileSet *token.FileSet, expr ast.Expr) string {
	t.Helper()
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fileSet, expr); err != nil {
		t.Fatalf("print expression: %v", err)
	}
	return buf.String()
}

// TestRecoveryLookupEqualizerSpendsBothPlaceholdersAtTargetCost is TS-M06's
// test hardening: the count-only check above (compares != 2) is satisfied by
// a body that compares the WRONG operand against a placeholder — for example
// the code against credentialsTimingEqualizationHash and the password against
// recoveryCodeTimingEqualizationHash — because the two placeholders share the
// same cost today. That still passes count==2 while breaking the intent the
// helper's own doc comment states: the recovery-code compare must run against
// the code operand and the password compare against the password operand,
// each at passwordHashCost, so a real "unknown address" refusal genuinely
// costs what a real "wrong password" refusal costs. equalizeRecoveryCodeLookupTiming
// is declared as a bare func (not a swappable var) precisely so its literal
// calls can be read from source instead of intercepted at runtime — this test
// reads the two calls' actual arguments, in order, off the shipped body, and
// pins the cost of the two constants those arguments compare against. No
// wall-clock threshold is involved anywhere in this test.
func TestRecoveryLookupEqualizerSpendsBothPlaceholdersAtTargetCost(t *testing.T) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "auth_service.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse auth_service.go: %v", err)
	}

	var body *ast.BlockStmt
	ast.Inspect(file, func(node ast.Node) bool {
		decl, ok := node.(*ast.FuncDecl)
		if ok && decl.Name.Name == "equalizeRecoveryCodeLookupTiming" && decl.Body != nil {
			body = decl.Body
		}
		return true
	})
	if body == nil {
		t.Fatal("equalizeRecoveryCodeLookupTiming is missing from auth_service.go")
	}

	type observedCompare struct {
		hash    string
		operand string
	}
	var observed []observedCompare
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !isBcryptCompareCall(call) {
			return true
		}
		if len(call.Args) != 2 {
			t.Fatalf("a bcrypt.CompareHashAndPassword call in equalizeRecoveryCodeLookupTiming has %d arguments, want 2", len(call.Args))
		}
		observed = append(observed, observedCompare{
			hash:    printExpr(t, fileSet, call.Args[0]),
			operand: printExpr(t, fileSet, call.Args[1]),
		})
		return true
	})

	want := []observedCompare{
		{hash: "[]byte(recoveryCodeTimingEqualizationHash)", operand: "[]byte(NormalizeRecoveryCode(code))"},
		{hash: "[]byte(credentialsTimingEqualizationHash)", operand: "[]byte(password)"},
	}
	if len(observed) != len(want) {
		t.Fatalf("equalizeRecoveryCodeLookupTiming spends %d bcrypt comparisons, want %d: %+v", len(observed), len(want), observed)
	}
	for index, wantCompare := range want {
		if observed[index] != wantCompare {
			t.Fatalf("comparison %d compared %+v, want %+v — the wrong operand against a placeholder still counts as a compare, "+
				"but a rejection then costs the wrong secret's oracle", index, observed[index], wantCompare)
		}
	}

	for name, hash := range map[string]string{
		"recoveryCodeTimingEqualizationHash": recoveryCodeTimingEqualizationHash,
		"credentialsTimingEqualizationHash":  credentialsTimingEqualizationHash,
	} {
		cost, err := bcrypt.Cost([]byte(hash))
		if err != nil {
			t.Fatalf("bcrypt.Cost(%s): %v", name, err)
		}
		if cost != passwordHashCost {
			t.Fatalf("%s costs %d, want passwordHashCost (%d) — a cheaper placeholder makes the refusal this helper equalizes measurably faster than a real compare",
				name, cost, passwordHashCost)
		}
	}
}
