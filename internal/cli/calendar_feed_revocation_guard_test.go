package cli

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/testpackages"
	"golang.org/x/tools/go/packages"
)

const revocationGuardServicesPackage = "github.com/ovumcy/ovumcy-web/internal/services"

// revokingServiceCalls are the service methods a CLI subcommand calls that
// revoke an owner's calendar feed, webhook endpoint or sessions on someone
// ELSE's behalf. Each is keyed by its declaration — the types.Func's full,
// receiver-qualified name — never by the selector a call site spells, so a
// same-named method on another type cannot satisfy or escape the guard.
//
// The reasons are the point. An exemption or an omission without one is how
// this guard would quietly become a list of whatever was easiest to gate.
var revokingServiceCalls = map[string]string{
	"(*" + revocationGuardServicesPackage + ".OperatorUserService).DeleteUserByID":             "OperatorUserService.deleteUser calls DeleteAccountAndRelatedData: the erased row takes its armed feed with it, and a restore brings both back together",
	"(*" + revocationGuardServicesPackage + ".OperatorUserService).DeleteUserByEmail":          "the address-addressed form of the same erasure, reaching the same DeleteAccountAndRelatedData",
	"(*" + revocationGuardServicesPackage + ".AuthService).ForceResetPasswordByID":             "AuthService.forceResetPassword calls ForceResetPasswordAndRevokeSessions, which force-clears the three calendar-feed access columns in the same statement",
	"(*" + revocationGuardServicesPackage + ".WebhookSettingsCLIService).ApplyWebhookSettings": "a non-preview call reaches SaveWebhookSettings, which can disable, clear or replace the endpoint; withdrawn only in the database, a restore hands it back to notify (WEB-414). Previews go through PreviewWebhookSettings",
	"(*" + revocationGuardServicesPackage + ".OperatorUserService).SetEmailByID":               "re-homes the login identity and bumps AuthSessionVersion in the same write; recorded only in the database, a restore revives every session it revoked (WEB-418)",
	"(*" + revocationGuardServicesPackage + ".OIDCLoginService).ConfirmAndLinkIdentity":        "CreateAndRevokeSessions links the identity and bumps AuthSessionVersion in one write; recorded only in the database, a restore revives every session it revoked (WEB-505)",
}

// gateBeforeWriteCallbacks are the revoking methods that take the gate as a
// callback and run it after their own refusals and before their write, so a
// confirmOperatorFeedRevocation inside a function literal passed to THAT call
// gates it. Nowhere else does a gate inside a function literal count: a
// closure is not a statement that runs in order.
var gateBeforeWriteCallbacks = map[string]string{
	"(*" + revocationGuardServicesPackage + ".OperatorUserService).SetEmailByID": "runs beforeWrite after the no-op and refused-address checks and before SetUserEmailByIDAndRevokeSessions",
}

// TestEveryFeedRevokingCLICallPassesTheFenceGate is the CLI counterpart of
// internal/db's TestEveryCalendarFeedWriterAdvancesTheRestoreFence, and it
// guards the half that one structurally cannot: for an operator-driven
// revocation — a feed, a webhook endpoint, or the account's sessions — the
// containment rests on confirmOperatorFeedRevocation being reached FIRST. A
// subcommand that calls one of the revoking methods without passing the gate
// revokes on someone else's behalf and, with no fence configured, records it
// nowhere a restore could contradict — exactly the defect the fence exists to
// close — and nothing else in the suite would notice.
//
// The check follows calls through package-local helpers, resolved by
// declaration, because the call that erases the row does not sit in the
// function that gates: runUsersDelete gates and then calls
// usersDeleteOptions.delete, which is where DeleteUserByID actually appears.
func TestEveryFeedRevokingCLICallPassesTheFenceGate(t *testing.T) {
	if testpackages.Isolate(t) {
		return
	}
	t.Parallel()

	scope, functions := loadCLIPackageFunctions(t)

	reaches := func(call cliCall, ungated map[*types.Func]bool) bool {
		return revokingServiceCalls[call.callee.FullName()] != "" || ungated[call.callee]
	}

	// ungated[f] means: f reaches a revoking call along some path that crosses
	// no confirmOperatorFeedRevocation first. Computed to a fixed point, since
	// one helper calls another.
	ungated := map[*types.Func]bool{}
	for changed := true; changed; {
		changed = false
		for function, body := range functions {
			if ungated[function] {
				continue
			}
			for _, call := range body.calls {
				if !reaches(call, ungated) || call.gatedInline {
					continue
				}
				if body.confirmAt.IsValid() && body.confirmAt < call.at {
					continue
				}
				ungated[function] = true
				changed = true
				break
			}
		}
	}

	// Anti-vacuity, on fixtures this test owns rather than on the data it
	// judges: every listed method must still be called somewhere in the
	// package, and each command that gates must come out gated.
	found := map[string]bool{}
	for _, body := range functions {
		for _, call := range body.calls {
			if revokingServiceCalls[call.callee.FullName()] != "" {
				found[call.callee.FullName()] = true
			}
		}
	}
	for name, reason := range revokingServiceCalls {
		if !found[name] {
			t.Fatalf("no call to %s remains in this package (%s): drop the entry rather than leaving the guard listing a call it can never see", name, reason)
		}
	}
	lookup := func(name string) *types.Func {
		function, ok := scope.Lookup(name).(*types.Func)
		if !ok || functions[function] == nil {
			t.Fatalf("%s is not a function in the parsed package: this guard is not looking at what it claims", name)
		}
		return function
	}
	for _, gated := range []string{"runUsersDelete", "runResetPasswordCommand", "runWebhookSet", "runUsersSetEmail", "linkOIDCIdentity"} {
		if ungated[lookup(gated)] {
			t.Fatalf("%s reaches an operator revocation without confirming the restore fence first: a revocation recorded only inside the database is undone by restoring a backup taken before it", gated)
		}
	}
	// The load-bearing call sites, by name: the two session-only revocations
	// must be judged where they actually sit, not merely somewhere.
	for function, method := range map[string]string{
		"linkOIDCIdentity": "(*" + revocationGuardServicesPackage + ".OIDCLoginService).ConfirmAndLinkIdentity",
		"runUsersSetEmail": "(*" + revocationGuardServicesPackage + ".OperatorUserService).SetEmailByID",
	} {
		calledHere := false
		for _, call := range functions[lookup(function)].calls {
			calledHere = calledHere || call.callee.FullName() == method
		}
		if !calledHere {
			t.Fatalf("%s no longer calls %s itself: re-point this assertion at the function that does", function, method)
		}
	}

	// Every remaining ungated function must be a helper whose callers gate,
	// and it must actually HAVE such a caller — an ungated function nobody
	// calls is a revocation path reachable from the binary with no gate on it.
	callers := map[*types.Func][]*types.Func{}
	for function, body := range functions {
		for _, call := range body.calls {
			if functions[call.callee] != nil {
				callers[call.callee] = append(callers[call.callee], function)
			}
		}
	}
	var unguarded []string
	for function := range ungated {
		gatedBySomeCaller := len(callers[function]) > 0
		for _, caller := range callers[function] {
			if ungated[caller] {
				gatedBySomeCaller = false
				break
			}
		}
		if !gatedBySomeCaller {
			unguarded = append(unguarded, function.FullName())
		}
	}
	sort.Strings(unguarded)
	if len(unguarded) > 0 {
		t.Fatalf("these functions perform an operator revocation without confirmOperatorFeedRevocation anywhere ahead of the write, so it is recorded only inside the database and a restore undoes it silently: %v", unguarded)
	}
}

// cliCall is one outgoing call, resolved to the function it calls. Positions
// come from the AST, never from the body text — a doc comment beside the
// write that quoted the gate's name would otherwise move a text offset.
type cliCall struct {
	callee *types.Func
	at     token.Pos
	// gatedInline: the callee runs a callback before its write
	// (gateBeforeWriteCallbacks) and a function literal passed to this call
	// confirms the fence.
	gatedInline bool
}

type cliFunction struct {
	// confirmAt is the earliest confirmOperatorFeedRevocation call in the
	// body outside any function literal, or token.NoPos when there is none.
	confirmAt token.Pos
	calls     []cliCall
}

// loadCLIPackageFunctions type-checks this package's non-test sources and keys
// every function and method declaration by its *types.Func, with each call in
// its body resolved through types.Info to the declaration it calls.
func loadCLIPackageFunctions(t *testing.T) (*types.Scope, map[*types.Func]*cliFunction) {
	t.Helper()

	config := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
		Dir:   ".",
		Tests: false,
	}
	loaded, err := testpackages.Load(config, ".")
	if err != nil {
		t.Fatalf("type-check this package: %v", err)
	}
	if len(loaded) != 1 || len(loaded[0].Errors) > 0 || len(loaded[0].Syntax) == 0 {
		// Fail closed: an unresolved selector is exactly the evidence this
		// guard reads.
		t.Fatalf("this package did not load cleanly (%d packages): %v", len(loaded), packagesErrors(loaded))
	}
	pkg := loaded[0]
	gate, ok := pkg.Types.Scope().Lookup("confirmOperatorFeedRevocation").(*types.Func)
	if !ok {
		t.Fatal("confirmOperatorFeedRevocation is not declared in this package: the guard has no gate to look for")
	}

	resolve := func(call *ast.CallExpr) *types.Func {
		var name *ast.Ident
		switch callee := ast.Unparen(call.Fun).(type) {
		case *ast.Ident:
			name = callee
		case *ast.SelectorExpr:
			name = callee.Sel
		}
		if name == nil {
			return nil
		}
		function, _ := pkg.TypesInfo.Uses[name].(*types.Func)
		if function == nil {
			return nil
		}
		return function.Origin()
	}
	confirmsInside := func(node ast.Node) bool {
		confirms := false
		ast.Inspect(node, func(inner ast.Node) bool {
			if call, ok := inner.(*ast.CallExpr); ok && resolve(call) == gate {
				confirms = true
			}
			return !confirms
		})
		return confirms
	}

	functions := map[*types.Func]*cliFunction{}
	for _, file := range pkg.Syntax {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			object, ok := pkg.TypesInfo.Defs[function.Name].(*types.Func)
			if !ok {
				t.Fatalf("%s has no declaration object: the package was not type-checked", function.Name.Name)
			}
			var literals []*ast.FuncLit
			ast.Inspect(function.Body, func(node ast.Node) bool {
				if literal, ok := node.(*ast.FuncLit); ok {
					literals = append(literals, literal)
				}
				return true
			})
			insideLiteral := func(at token.Pos) bool {
				for _, literal := range literals {
					if literal.Pos() <= at && at < literal.End() {
						return true
					}
				}
				return false
			}

			collected := &cliFunction{}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				callee := resolve(call)
				if callee == nil {
					return true
				}
				if callee == gate {
					if !insideLiteral(call.Pos()) && (!collected.confirmAt.IsValid() || call.Pos() < collected.confirmAt) {
						collected.confirmAt = call.Pos()
					}
					return true
				}
				inline := false
				if gateBeforeWriteCallbacks[callee.FullName()] != "" {
					for _, argument := range call.Args {
						if literal, ok := argument.(*ast.FuncLit); ok && confirmsInside(literal) {
							inline = true
						}
					}
				}
				collected.calls = append(collected.calls, cliCall{callee: callee, at: call.Pos(), gatedInline: inline})
				return true
			})
			functions[object] = collected
		}
	}
	if len(functions) == 0 {
		t.Fatal("no function declarations were parsed: the scan is looking at the wrong directory")
	}
	return pkg.Types.Scope(), functions
}

func packagesErrors(loaded []*packages.Package) []string {
	var messages []string
	for _, pkg := range loaded {
		for _, packageError := range pkg.Errors {
			messages = append(messages, pkg.PkgPath+": "+packageError.Error())
		}
	}
	return messages
}
