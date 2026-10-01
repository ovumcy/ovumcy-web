package api

import (
	"go/ast"
	"go/token"
	"go/types"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// Login forgives a client's failed sign-ins only once the cookie that carries
// the sign-in onward has been issued. The behavioural test proves that for the
// session arm through its issuance fault; the TOTP-pending and forced-reset
// arms have no such fault to inject. This barrier holds every arm by
// declaration: the login budget's reset is referenced only inside Login, only
// as a direct call statement, and each such statement follows — in its own
// block — an `if err := <issuer>(…); err != nil { …; return … }` whose issuer
// is one of the three cookie setters.
func TestLoginResetsOnlyAfterEachArmIssuesItsCookie(t *testing.T) {
	root, err := moduleRootForBarrier()
	if err != nil {
		t.Fatalf("locate the module root: %v", err)
	}
	loaded, err := packages.Load(&packages.Config{
		Mode:  packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Dir:   root,
		Tests: false,
	}, "./internal/api")
	if err != nil {
		t.Fatalf("load internal/api: %v", err)
	}
	if len(loaded) != 1 || len(loaded[0].Errors) > 0 {
		t.Fatalf("internal/api did not type-check cleanly (%d package(s)): %v", len(loaded), settingsReauthLoadErrors(loaded))
	}
	pkg := loaded[0]
	info := pkg.TypesInfo

	login := settingsReauthMethod(t, pkg.Types, "Handler", "Login")
	reset := loginBudgetResetMethod(t, pkg.Types)
	issuers := map[*types.Func]string{}
	for _, name := range []string{"setResetPasswordCookie", "setTOTPPendingCookie", "setAuthCookie"} {
		issuers[settingsReauthMethod(t, pkg.Types, "Handler", name)] = name
	}

	var problems, pairings []string
	where := func(node ast.Node) string { return pkg.Fset.Position(node.Pos()).String() }
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			inLogin := info.Defs[function.Name] == login
			placed := map[*ast.SelectorExpr]bool{}
			if inLogin {
				ast.Inspect(function.Body, func(node ast.Node) bool {
					block, ok := node.(*ast.BlockStmt)
					if !ok {
						return true
					}
					for index, statement := range block.List {
						selector := loginResetCallStatement(info, statement, reset)
						if selector == nil {
							continue
						}
						placed[selector] = true
						issuer := loginPrecedingIssuer(info, block.List[:index], issuers)
						if issuer == "" {
							problems = append(problems, "ResetAttempts at "+where(selector)+" follows no issued cookie in its block")
							continue
						}
						pairings = append(pairings, issuer+" → ResetAttempts")
					}
					return true
				})
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				selector, ok := node.(*ast.SelectorExpr)
				if !ok || info.Uses[selector.Sel] != reset || placed[selector] {
					return true
				}
				if !inLogin {
					problems = append(problems, "ResetAttempts at "+where(selector)+" is reached from "+function.Name.Name+", outside Login")
				} else {
					problems = append(problems, "ResetAttempts at "+where(selector)+" is not a direct call statement")
				}
				return true
			})
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		t.Fatalf("the login budget is reset before its sign-in lands: %s. Call handler.loginService.ResetAttempts directly in Login, below the arm's cookie issuance and its error return", strings.Join(problems, "; "))
	}

	// Anti-vacuity by name: each arm's pairing, not a count of them.
	sort.Strings(pairings)
	want := []string{"setAuthCookie → ResetAttempts", "setResetPasswordCookie → ResetAttempts", "setTOTPPendingCookie → ResetAttempts"}
	if strings.Join(pairings, "; ") != strings.Join(want, "; ") {
		t.Fatalf("Login's resets paired as [%s], want [%s]: the sweep resolved the wrong objects or an arm changed shape", strings.Join(pairings, "; "), strings.Join(want, "; "))
	}
}

// loginBudgetResetMethod resolves ResetAttempts on the type of Handler's
// loginService field, so the object is the port method Login calls.
func loginBudgetResetMethod(t *testing.T, pkg *types.Package) *types.Func {
	t.Helper()
	handlerType, ok := pkg.Scope().Lookup("Handler").(*types.TypeName)
	if !ok {
		t.Fatalf("type Handler is not declared in %s", pkg.Path())
	}
	field, _, _ := types.LookupFieldOrMethod(handlerType.Type(), true, pkg, "loginService")
	fieldVar, ok := field.(*types.Var)
	if !ok {
		t.Fatalf("Handler has no loginService field")
	}
	method, _, _ := types.LookupFieldOrMethod(fieldVar.Type(), false, pkg, "ResetAttempts")
	reset, ok := method.(*types.Func)
	if !ok {
		t.Fatalf("loginService's type has no ResetAttempts method")
	}
	return reset
}

// loginResetCallStatement returns the selector of statement when statement is
// exactly a call of reset, and nil otherwise.
func loginResetCallStatement(info *types.Info, statement ast.Stmt, reset *types.Func) *ast.SelectorExpr {
	expression, ok := statement.(*ast.ExprStmt)
	if !ok {
		return nil
	}
	call, ok := expression.X.(*ast.CallExpr)
	if !ok || settingsReauthCallee(info, call) != reset {
		return nil
	}
	return call.Fun.(*ast.SelectorExpr)
}

// loginPrecedingIssuer names the nearest issuer among statements that is
// called as `if err := issuer(…); err != nil { …; return … }`.
func loginPrecedingIssuer(info *types.Info, statements []ast.Stmt, issuers map[*types.Func]string) string {
	for index := len(statements) - 1; index >= 0; index-- {
		guard, ok := statements[index].(*ast.IfStmt)
		if !ok || guard.Init == nil || len(guard.Body.List) == 0 {
			continue
		}
		init, ok := guard.Init.(*ast.AssignStmt)
		if !ok || len(init.Rhs) != 1 {
			continue
		}
		call, ok := init.Rhs[0].(*ast.CallExpr)
		if !ok {
			continue
		}
		name, ok := issuers[settingsReauthCallee(info, call)]
		if !ok {
			continue
		}
		condition, ok := guard.Cond.(*ast.BinaryExpr)
		if !ok || condition.Op != token.NEQ {
			continue
		}
		checked, ok := condition.X.(*ast.Ident)
		if !ok || !loginAssignDefines(info, init, info.Uses[checked]) {
			continue
		}
		if _, returns := guard.Body.List[len(guard.Body.List)-1].(*ast.ReturnStmt); returns {
			return name
		}
	}
	return ""
}

func loginAssignDefines(info *types.Info, assign *ast.AssignStmt, object types.Object) bool {
	for _, left := range assign.Lhs {
		if ident, ok := left.(*ast.Ident); ok && object != nil && info.Defs[ident] == object {
			return true
		}
	}
	return false
}
