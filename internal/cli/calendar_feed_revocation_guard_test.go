package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"sort"
	"strings"
	"testing"
)

// feedRevokingServiceCalls are the service methods that remove an owner's
// calendar-feed access on someone ELSE's behalf. They are named by hand
// because they live in another package: internal/db's own writers guard
// derives the repository writers from the file it scans, and each entry here
// records which of those a CLI-visible call reaches.
//
// The reasons are the point. An exemption or an omission without one is how
// this guard would quietly become a list of whatever was easiest to gate.
var feedRevokingServiceCalls = map[string]string{
	"DeleteUserByID":         "OperatorUserService.deleteUser calls DeleteAccountAndRelatedData: the erased row takes its armed feed with it, and a restore brings both back together",
	"DeleteUserByEmail":      "the address-addressed form of the same erasure, reaching the same DeleteAccountAndRelatedData",
	"ForceResetPasswordByID": "AuthService.forceResetPassword calls ForceResetPasswordAndRevokeSessions, which force-clears the three calendar-feed access columns in the same statement",
}

// TestEveryFeedRevokingCLICallPassesTheFenceGate is the CLI counterpart of
// internal/db's TestEveryCalendarFeedWriterAdvancesTheRestoreFence, and it
// guards the half that one structurally cannot: the repository's own advance
// is best-effort and records the removal only where a restore can roll it
// back, so for an operator-driven revocation the containment rests entirely on
// confirmOperatorFeedRevocation being reached FIRST. A subcommand that calls
// one of the revoking methods without passing the gate revokes a feed on
// someone else's behalf and records it nowhere a restore could contradict —
// exactly the defect the fence exists to close — and nothing else in the suite
// would notice.
//
// The check follows calls through package-local helpers, because the call that
// erases the row does not sit in the function that gates: runUsersDelete gates
// and then calls usersDeleteOptions.delete, which is where DeleteUserByID
// actually appears. A scan that looked only at the function containing the
// revoking call would demand a gate inside that two-line helper and report the
// real, correctly-gated command as unguarded.
func TestEveryFeedRevokingCLICallPassesTheFenceGate(t *testing.T) {
	t.Parallel()

	functions := parseCLIPackageFunctions(t)

	// ungated[name] means: this function reaches a revoking call along some
	// path that crosses no confirmOperatorFeedRevocation first. Computed to a
	// fixed point, since one helper calls another.
	ungated := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for name, function := range functions {
			if ungated[name] {
				continue
			}
			for _, call := range function.calls {
				reaches := feedRevokingServiceCalls[call.name] != "" || ungated[call.name]
				if !reaches {
					continue
				}
				if function.confirmAt >= 0 && function.confirmAt < call.at {
					continue
				}
				ungated[name] = true
				changed = true
				break
			}
		}
	}

	// Anti-vacuity, on fixtures this test owns rather than on the data it
	// judges: the scan must find every revoking method somewhere in the
	// package (or the list names calls that no longer exist and the guard is
	// measuring nothing), and the two commands that gate must come out gated.
	found := map[string]bool{}
	for _, function := range functions {
		for _, call := range function.calls {
			if feedRevokingServiceCalls[call.name] != "" {
				found[call.name] = true
			}
		}
	}
	for name, reason := range feedRevokingServiceCalls {
		if !found[name] {
			t.Fatalf("no call to %s remains in this package (%s): drop the entry rather than leaving the guard listing a call it can never see", name, reason)
		}
	}
	for _, gated := range []string{"runUsersDelete", "runResetPasswordCommand"} {
		if _, ok := functions[gated]; !ok {
			t.Fatalf("%s is not in the parsed package: this guard is not looking at what it claims", gated)
		}
		if ungated[gated] {
			t.Fatalf("%s reaches a calendar-feed revocation without confirming the restore fence first: an operator-driven removal recorded only inside the database is undone by restoring a backup taken before it", gated)
		}
	}

	// Every remaining ungated function must be a helper whose callers gate,
	// and it must actually HAVE such a caller — an ungated function nobody
	// calls is a revocation path reachable from the binary with no gate on it.
	callers := map[string][]string{}
	for name, function := range functions {
		for _, call := range function.calls {
			if _, ok := functions[call.name]; ok {
				callers[call.name] = append(callers[call.name], name)
			}
		}
	}
	var unguarded []string
	for name := range ungated {
		gatedBySomeCaller := len(callers[name]) > 0
		for _, caller := range callers[name] {
			if ungated[caller] {
				gatedBySomeCaller = false
				break
			}
		}
		if !gatedBySomeCaller {
			unguarded = append(unguarded, name)
		}
	}
	sort.Strings(unguarded)
	if len(unguarded) > 0 {
		t.Fatalf("these functions revoke an owner's calendar feed without confirmOperatorFeedRevocation anywhere ahead of the write, so the removal is recorded only inside the database and a restore undoes it silently: %v", unguarded)
	}
}

// cliCall is one outgoing call: the selector's name and where it sits, so the
// guard can ask whether the gate ran BEFORE it. Positions come from the AST,
// never from the body text — a doc comment beside the write that quoted the
// gate's name would otherwise move a text offset and flip the verdict.
type cliCall struct {
	name string
	at   int
}

type cliFunction struct {
	// confirmAt is the earliest confirmOperatorFeedRevocation call in the
	// body, or -1 when there is none.
	confirmAt int
	calls     []cliCall
}

// parseCLIPackageFunctions reads every non-test source file in this package and
// keys the functions by name — receiver-qualified names are deliberately NOT
// used, because a call site says only `opts.delete`, and the selector is all
// the scan can match against.
func parseCLIPackageFunctions(t *testing.T) map[string]cliFunction {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}

	functions := map[string]cliFunction{}
	fileSet := token.NewFileSet()
	sources := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		parsed, err := parser.ParseFile(fileSet, name, source, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		sources++

		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			collected := cliFunction{confirmAt: -1}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				at := int(call.Pos())
				switch callee := call.Fun.(type) {
				case *ast.Ident:
					if callee.Name == "confirmOperatorFeedRevocation" {
						if collected.confirmAt < 0 || at < collected.confirmAt {
							collected.confirmAt = at
						}
						return true
					}
					collected.calls = append(collected.calls, cliCall{name: callee.Name, at: at})
				case *ast.SelectorExpr:
					collected.calls = append(collected.calls, cliCall{name: callee.Sel.Name, at: at})
				}
				return true
			})
			functions[function.Name.Name] = collected
		}
	}

	if sources == 0 {
		t.Fatal("no non-test sources were parsed: the scan is looking at the wrong directory")
	}
	return functions
}
