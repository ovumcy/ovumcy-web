package api

import (
	"go/ast"
	"go/types"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// validateSettingsActionPassword never clears settings.reauth: it hands its
// caller a settingsReauth whose resetBudget the caller owes once the write the
// password authorised has committed. Nothing in the type system makes a caller
// pay it, and a caller that forgets compiles and passes every other test while
// its route stops ever forgiving a typo. This barrier resolves both functions by
// declaration and refuses any production function that binds the result of a
// validateSettingsActionPassword call and never calls resetBudget on that same
// variable. Where the reset sits relative to the write is the behavioural tests'
// subject (settings_reauth_reset_after_write_test.go); this is the one that
// notices a new caller that has no reset at all.
func TestEverySettingsReauthCallerResetsTheBudgetItWasHanded(t *testing.T) {
	root, err := moduleRootForBarrier()
	if err != nil {
		t.Fatalf("locate the module root: %v", err)
	}
	loaded, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Dir:  root,
		// Production callers only: a test that validates and never resets is
		// measuring exactly that.
		Tests: false,
	}, "./internal/api")
	if err != nil {
		t.Fatalf("load internal/api: %v", err)
	}
	if len(loaded) != 1 || len(loaded[0].Errors) > 0 {
		t.Fatalf("internal/api did not type-check cleanly (%d package(s)): %v", len(loaded), settingsReauthLoadErrors(loaded))
	}
	pkg := loaded[0]

	validate := settingsReauthMethod(t, pkg.Types, "Handler", "validateSettingsActionPassword")
	reset := settingsReauthMethod(t, pkg.Types, "settingsReauth", "resetBudget")

	checked := map[string]bool{}
	var missing []string
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			owed, unbound := settingsReauthBindings(pkg.TypesInfo, function.Body, validate)
			if len(owed) == 0 && unbound == 0 {
				continue
			}
			name := function.Name.Name
			checked[name] = true
			if unbound > 0 {
				missing = append(missing, name+" (a validateSettingsActionPassword result bound to no variable)")
			}
			paid := settingsReauthResets(pkg.TypesInfo, function.Body, reset)
			for variable := range owed {
				if !paid[variable] {
					missing = append(missing, name+" ("+variable.Name()+")")
				}
			}
		}
	}

	// Anti-vacuity by name: the wipe is the caller whose reset was most recently
	// moved, and the step-up start is the one caller whose reset follows a
	// redirect rather than a write. A sweep that saw neither saw nothing.
	for _, loadBearing := range []string{"ClearAllData", "StartOIDCIdentityLinkStepup"} {
		if !checked[loadBearing] {
			t.Fatalf("the sweep never reached %s's validateSettingsActionPassword call; it resolved the wrong object or read the wrong files", loadBearing)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("settings.reauth is verified and never reset in: %s. Call resetBudget on the settingsReauth once the write the password authorised has committed (or at once, and say why, if the action writes nothing)", strings.Join(missing, "; "))
	}
}

func settingsReauthMethod(t *testing.T, pkg *types.Package, typeName string, method string) *types.Func {
	t.Helper()
	typeObject, ok := pkg.Scope().Lookup(typeName).(*types.TypeName)
	if !ok {
		t.Fatalf("type %s is not declared in %s", typeName, pkg.Path())
	}
	object, _, _ := types.LookupFieldOrMethod(types.NewPointer(typeObject.Type()), true, pkg, method)
	function, ok := object.(*types.Func)
	if !ok {
		t.Fatalf("%s has no method %s", typeName, method)
	}
	return function
}

// settingsReauthBindings returns the variables body binds a validate call's
// first result to, and how many validate calls it makes whose first result is
// bound to nothing.
func settingsReauthBindings(info *types.Info, body *ast.BlockStmt, validate *types.Func) (map[*types.Var]bool, int) {
	owed := map[*types.Var]bool{}
	bound := map[*ast.CallExpr]bool{}
	calls := 0
	ast.Inspect(body, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.AssignStmt:
			if len(node.Rhs) != 1 || len(node.Lhs) == 0 {
				return true
			}
			call, ok := node.Rhs[0].(*ast.CallExpr)
			if !ok || settingsReauthCallee(info, call) != validate {
				return true
			}
			if ident, ok := node.Lhs[0].(*ast.Ident); ok {
				if variable, ok := settingsReauthIdentObject(info, ident).(*types.Var); ok {
					owed[variable] = true
					bound[call] = true
				}
			}
		case *ast.CallExpr:
			if settingsReauthCallee(info, node) == validate {
				calls++
			}
		}
		return true
	})
	return owed, calls - len(bound)
}

// settingsReauthResets returns the variables body calls reset on.
func settingsReauthResets(info *types.Info, body *ast.BlockStmt, reset *types.Func) map[*types.Var]bool {
	paid := map[*types.Var]bool{}
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || settingsReauthCallee(info, call) != reset {
			return true
		}
		selector := call.Fun.(*ast.SelectorExpr)
		if ident, ok := selector.X.(*ast.Ident); ok {
			if variable, ok := info.Uses[ident].(*types.Var); ok {
				paid[variable] = true
			}
		}
		return true
	})
	return paid
}

func settingsReauthCallee(info *types.Info, call *ast.CallExpr) *types.Func {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	function, _ := info.Uses[selector.Sel].(*types.Func)
	return function
}

func settingsReauthIdentObject(info *types.Info, ident *ast.Ident) types.Object {
	if object := info.Defs[ident]; object != nil {
		return object
	}
	return info.Uses[ident]
}

func settingsReauthLoadErrors(loaded []*packages.Package) string {
	var messages []string
	for _, pkg := range loaded {
		for _, err := range pkg.Errors {
			messages = append(messages, err.Error())
		}
	}
	return strings.Join(messages, "; ")
}
