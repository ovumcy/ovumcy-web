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

// A correct password forgives the client's failed sign-ins only once the
// cookie that carries the sign-in onward has been issued. The behavioural test
// proves that for Login's session arm through its issuance fault; the
// TOTP-pending and forced-reset arms have no such fault to inject. This barrier
// starts from the obligation, not from the reset: it resolves the login port's
// Authenticate by declaration and holds every production function that calls
// it. In each, every return after the first Authenticate call either sits in
// the body of an `if <error> != nil` guard — the sign-in did not land — or is
// reached only through a reset statement that itself follows, in its own
// block, an `if err := <issuer>(…); err != nil { …; return … }` whose issuer
// reaches a fiber cookie write. A new arm that answers without that reset, or a
// second caller that never resets, fails here; so does a reset anywhere else.
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
	sweep := loginResetSweep{
		info:         pkg.TypesInfo,
		fset:         pkg.Fset,
		authenticate: loginServiceMethod(t, pkg.Types, "Authenticate"),
		reset:        loginServiceMethod(t, pkg.Types, "ResetAttempts"),
		issuers:      loginCookieWriters(t, pkg),
	}
	login := settingsReauthMethod(t, pkg.Types, "Handler", "Login")

	exits := map[string][]string{}
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			name := function.Name.Name
			if object, ok := sweep.info.Defs[function.Name].(*types.Func); ok && object == login {
				name = "Handler.Login"
			}
			sweep.function(function, name, exits)
		}
	}
	if len(sweep.problems) > 0 {
		sort.Strings(sweep.problems)
		t.Fatalf("the login budget is not reset after each sign-in lands: %s. Call handler.loginService.ResetAttempts as a statement directly below the arm's cookie issuance and its error return, on every arm that answers a correct password", strings.Join(sweep.problems, "; "))
	}

	// Anti-vacuity by name: Login is an Authenticate caller, and each of its
	// arms answers through the issuer that paid for its reset.
	arms, ok := exits["Handler.Login"]
	if !ok {
		t.Fatalf("the sweep never reached Handler.Login's Authenticate call; it resolved the wrong object or read the wrong files")
	}
	want := []string{"setAuthCookie", "setResetPasswordCookie", "setTOTPPendingCookie"}
	if strings.Join(arms, ", ") != strings.Join(want, ", ") {
		t.Fatalf("Handler.Login's success exits were paid for by [%s], want [%s]: an arm changed shape or the sweep resolved the wrong objects", strings.Join(arms, ", "), strings.Join(want, ", "))
	}
}

type loginResetSweep struct {
	info         *types.Info
	fset         *token.FileSet
	authenticate *types.Func
	reset        *types.Func
	issuers      map[*types.Func]string
	problems     []string
}

// function checks one declaration and records, under name, the sorted issuers
// whose resets pay for its success exits.
func (sweep *loginResetSweep) function(function *ast.FuncDecl, name string, exits map[string][]string) {
	var first *ast.CallExpr
	ast.Inspect(function.Body, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok && first == nil && loginStaticCallee(sweep.info, call) == sweep.authenticate {
			first = call
		}
		return true
	})
	placed := map[*ast.SelectorExpr]bool{}
	if first != nil {
		paidBy := map[string]bool{}
		sweep.block(function.Body.List, first.Pos(), "", false, name, placed, paidBy)
		var names []string
		for issuer := range paidBy {
			names = append(names, issuer)
		}
		sort.Strings(names)
		exits[name] = names
	}
	ast.Inspect(function.Body, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok || sweep.info.Uses[selector.Sel] != sweep.reset || placed[selector] {
			return true
		}
		if first == nil {
			sweep.problems = append(sweep.problems, "ResetAttempts at "+sweep.where(selector)+" is reached from "+name+", which calls no Authenticate")
		} else {
			sweep.problems = append(sweep.problems, "ResetAttempts at "+sweep.where(selector)+" in "+name+" is not a direct call statement")
		}
		return true
	})
}

// block walks statements on one path. paid names the issuer whose reset has
// already run on this path ("" for none); failing marks the body of an error
// guard, whose returns owe nothing.
func (sweep *loginResetSweep) block(statements []ast.Stmt, after token.Pos, paid string, failing bool, name string, placed map[*ast.SelectorExpr]bool, paidBy map[string]bool) {
	for index, statement := range statements {
		if selector := loginResetCallStatement(sweep.info, statement, sweep.reset); selector != nil {
			placed[selector] = true
			issuer := loginPrecedingIssuer(sweep.info, statements[:index], sweep.issuers)
			if issuer == "" {
				sweep.problems = append(sweep.problems, "ResetAttempts at "+sweep.where(selector)+" in "+name+" follows no issued cookie in its block")
				continue
			}
			paid = issuer
			continue
		}
		switch statement := statement.(type) {
		case *ast.ReturnStmt:
			if statement.Pos() < after || failing {
				continue
			}
			if paid == "" {
				sweep.problems = append(sweep.problems, name+" answers at "+sweep.where(statement)+" after Authenticate without resetting the budget behind an issued cookie")
				continue
			}
			paidBy[paid] = true
		case *ast.IfStmt:
			sweep.block(statement.Body.List, after, paid, failing || loginErrorGuard(sweep.info, statement.Cond), name, placed, paidBy)
			if statement.Else != nil {
				sweep.block([]ast.Stmt{statement.Else}, after, paid, failing, name, placed, paidBy)
			}
		case *ast.BlockStmt:
			sweep.block(statement.List, after, paid, failing, name, placed, paidBy)
		case *ast.LabeledStmt:
			sweep.block([]ast.Stmt{statement.Stmt}, after, paid, failing, name, placed, paidBy)
		case *ast.ForStmt:
			sweep.block(statement.Body.List, after, paid, failing, name, placed, paidBy)
		case *ast.RangeStmt:
			sweep.block(statement.Body.List, after, paid, failing, name, placed, paidBy)
		case *ast.SwitchStmt:
			sweep.clauses(statement.Body, after, paid, failing, name, placed, paidBy)
		case *ast.TypeSwitchStmt:
			sweep.clauses(statement.Body, after, paid, failing, name, placed, paidBy)
		case *ast.SelectStmt:
			sweep.clauses(statement.Body, after, paid, failing, name, placed, paidBy)
		}
	}
}

func (sweep *loginResetSweep) clauses(body *ast.BlockStmt, after token.Pos, paid string, failing bool, name string, placed map[*ast.SelectorExpr]bool, paidBy map[string]bool) {
	for _, clause := range body.List {
		switch clause := clause.(type) {
		case *ast.CaseClause:
			sweep.block(clause.Body, after, paid, failing, name, placed, paidBy)
		case *ast.CommClause:
			sweep.block(clause.Body, after, paid, failing, name, placed, paidBy)
		}
	}
}

func (sweep *loginResetSweep) where(node ast.Node) string {
	return sweep.fset.Position(node.Pos()).String()
}

// loginServiceMethod resolves name on the type of Handler's loginService
// field, so the object is the port method Login calls.
func loginServiceMethod(t *testing.T, pkg *types.Package, name string) *types.Func {
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
	method, _, _ := types.LookupFieldOrMethod(fieldVar.Type(), false, pkg, name)
	function, ok := method.(*types.Func)
	if !ok {
		t.Fatalf("loginService's type has no %s method", name)
	}
	return function
}

// loginCookieWriters names every function declared in pkg that reaches, through
// its own static calls, a call taking a *fiber.Cookie.
func loginCookieWriters(t *testing.T, pkg *packages.Package) map[*types.Func]string {
	t.Helper()
	var cookieType types.Type
	for _, imported := range pkg.Types.Imports() {
		if imported.Path() == "github.com/gofiber/fiber/v3" {
			if object, ok := imported.Scope().Lookup("Cookie").(*types.TypeName); ok {
				cookieType = types.NewPointer(object.Type())
			}
		}
	}
	if cookieType == nil {
		t.Fatalf("internal/api does not import fiber's Cookie type")
	}
	writesCookie := func(callee *types.Func) bool {
		signature, ok := callee.Type().(*types.Signature)
		if !ok {
			return false
		}
		for parameter := range signature.Params().Variables() {
			if types.Identical(parameter.Type(), cookieType) {
				return true
			}
		}
		return false
	}

	callees := map[*types.Func][]*types.Func{}
	writers := map[*types.Func]string{}
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			object, ok := pkg.TypesInfo.Defs[function.Name].(*types.Func)
			if !ok {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				callee := loginStaticCallee(pkg.TypesInfo, call)
				if callee == nil {
					return true
				}
				if callee.Pkg() != pkg.Types && writesCookie(callee) {
					writers[object] = object.Name()
				}
				callees[object] = append(callees[object], callee)
				return true
			})
		}
	}
	for grew := true; grew; {
		grew = false
		for caller, called := range callees {
			if _, known := writers[caller]; known {
				continue
			}
			for _, callee := range called {
				if _, reaches := writers[callee]; reaches {
					writers[caller] = caller.Name()
					grew = true
					break
				}
			}
		}
	}
	return writers
}

func loginStaticCallee(info *types.Info, call *ast.CallExpr) *types.Func {
	var ident *ast.Ident
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		ident = fun
	case *ast.SelectorExpr:
		ident = fun.Sel
	default:
		return nil
	}
	function, _ := info.Uses[ident].(*types.Func)
	if function == nil {
		return nil
	}
	return function.Origin()
}

// loginErrorGuard reports whether cond is `<value of type error> != nil`.
func loginErrorGuard(info *types.Info, cond ast.Expr) bool {
	comparison, ok := ast.Unparen(cond).(*ast.BinaryExpr)
	if !ok || comparison.Op != token.NEQ {
		return false
	}
	right, ok := ast.Unparen(comparison.Y).(*ast.Ident)
	if !ok || info.Uses[right] != types.Universe.Lookup("nil") {
		return false
	}
	return types.Identical(info.TypeOf(comparison.X), types.Universe.Lookup("error").Type())
}

// loginResetCallStatement returns the selector of statement when statement is
// exactly a call of reset, and nil otherwise.
func loginResetCallStatement(info *types.Info, statement ast.Stmt, reset *types.Func) *ast.SelectorExpr {
	expression, ok := statement.(*ast.ExprStmt)
	if !ok {
		return nil
	}
	call, ok := expression.X.(*ast.CallExpr)
	if !ok || loginStaticCallee(info, call) != reset {
		return nil
	}
	selector, _ := call.Fun.(*ast.SelectorExpr)
	return selector
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
		name, ok := issuers[loginStaticCallee(info, call)]
		if !ok || !loginErrorGuard(info, guard.Cond) {
			continue
		}
		checked, ok := ast.Unparen(guard.Cond.(*ast.BinaryExpr).X).(*ast.Ident)
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
