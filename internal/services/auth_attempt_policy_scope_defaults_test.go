package services

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestNewAuthAttemptPolicyFloorFallsBackToTheScopesOwnDefault pins that a figure
// below its floor leaves the budget's OWN default, for every scope the package
// builds a policy for: the re-auth budget is five attempts, not the sign-in
// budget's eight.
func TestNewAuthAttemptPolicyFloorFallsBackToTheScopesOwnDefault(t *testing.T) {
	for scope, want := range scopeAttemptDefaults {
		t.Run(scope, func(t *testing.T) {
			policy := NewAuthAttemptPolicy(scope, NewAttemptLimiter(), 0, 0)
			if policy.attempts != want.attempts || policy.window != want.window {
				t.Fatalf("scope %q fell back to %d / %s, want its own default %d / %s", scope, policy.attempts, policy.window, want.attempts, want.window)
			}
		})
	}

	settings := NewSettingsService(nil)
	settings.ConfigureReauthAttempts([]byte("test-secret"), NewAttemptLimiter(), 0, 0)
	if got := settings.reauthPolicy.attempts; got != DefaultSettingsReauthAttemptsLimit {
		t.Fatalf("re-auth limit after an unusable configuration = %d, want %d", got, DefaultSettingsReauthAttemptsLimit)
	}
	if got := settings.reauthPolicy.window; got != DefaultSettingsReauthAttemptsWindow {
		t.Fatalf("re-auth window after an unusable configuration = %s, want %s", got, DefaultSettingsReauthAttemptsWindow)
	}
	if DefaultSettingsReauthAttemptsLimit == DefaultLoginAttemptsLimit {
		t.Fatal("the re-auth and sign-in limits coincide: this test can no longer tell their defaults apart")
	}
}

// TestEveryAttemptPolicyScopeHasItsOwnDefault reads every NewAuthAttemptPolicy
// call in this package's non-test source and refuses a scope with no entry in
// scopeAttemptDefaults: a new budget would otherwise fall back to the sign-in
// figures without anyone choosing them. The scope must be a string literal so the
// walk can read it.
func TestEveryAttemptPolicyScopeHasItsOwnDefault(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	seen := map[string]bool{}
	fset := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			callee, ok := call.Fun.(*ast.Ident)
			if !ok || callee.Name != "NewAuthAttemptPolicy" || len(call.Args) == 0 {
				return true
			}
			literal, ok := call.Args[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				t.Errorf("%s: NewAuthAttemptPolicy scope is not a string literal", fset.Position(call.Pos()))
				return true
			}
			scope, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Errorf("%s: unquote %s: %v", fset.Position(call.Pos()), literal.Value, err)
				return true
			}
			seen[scope] = true
			if _, ok := scopeAttemptDefaults[scope]; !ok {
				t.Errorf("%s: scope %q has no entry in scopeAttemptDefaults; add its own default there", fset.Position(call.Pos()), scope)
			}
			return true
		})
	}
	for _, scope := range []string{"login", "settings.reauth", "totp.enroll"} {
		if !seen[scope] {
			t.Fatalf("the walk never saw scope %q: it is reading nothing", scope)
		}
	}
	for scope, figures := range scopeAttemptDefaults {
		if !seen[scope] {
			t.Errorf("scopeAttemptDefaults names %q but no policy is built for it", scope)
		}
		if figures.attempts < 1 || figures.window < time.Second {
			t.Errorf("default for %q is below the floor: %d / %s", scope, figures.attempts, figures.window)
		}
	}
}
