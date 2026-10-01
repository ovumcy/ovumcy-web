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
// Authenticate by declaration (and any method implementing it) and holds every
// production function body that calls it, function literals included. In each,
// every return after the first Authenticate call either sits in the body of an
// `if <error> != nil` guard — the sign-in did not land — or is reached only
// through a reset statement that itself follows, in its own block, an
// `if err := <issuer>(…); err != nil { …; return … }` whose issuer is one of
// the sign-in cookie setters. A new arm that answers without that reset, a
// second caller that never resets, Authenticate taken as a value, or a reset
// anywhere else fails here.
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
	port := loginServicePort(t, pkg.Types)
	sweep := loginResetSweep{
		info:         pkg.TypesInfo,
		fset:         pkg.Fset,
		port:         port,
		authenticate: loginPortMethod(t, port, pkg.Types, "Authenticate"),
		reset:        loginPortMethod(t, port, pkg.Types, "ResetAttempts"),
		issuers:      map[*types.Func]string{},
		exits:        map[string][]string{},
	}
	// The sign-in issuers are named, and each must still write a cookie: a
	// fourth sign-in cookie joins this list deliberately, never by default.
	writers := loginCookieWriters(t, pkg)
	for _, name := range []string{"setResetPasswordCookie", "setTOTPPendingCookie", "setAuthCookie"} {
		issuer := settingsReauthMethod(t, pkg.Types, "Handler", name)
		if _, ok := writers[issuer]; !ok {
			t.Fatalf("%s reaches no call taking a *fiber.Cookie; the sweep's cookie model no longer matches the code", name)
		}
		sweep.issuers[issuer] = name
	}
	login := settingsReauthMethod(t, pkg.Types, "Handler", "Login")

	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			owner := "package-level declaration"
			if function, ok := decl.(*ast.FuncDecl); ok {
				owner = function.Name.Name
				if object, ok := sweep.info.Defs[function.Name].(*types.Func); ok && object == login {
					owner = "Handler.Login"
				}
				if function.Body != nil {
					sweep.body(function.Body, owner)
				}
			}
			ast.Inspect(decl, func(node ast.Node) bool {
				if literal, ok := node.(*ast.FuncLit); ok {
					sweep.body(literal.Body, "a func literal in "+owner+" at "+sweep.where(literal))
				}
				return true
			})
		}
	}
	if len(sweep.problems) > 0 {
		sort.Strings(sweep.problems)
		t.Fatalf("the login budget is not reset after each sign-in lands: %s. Call handler.loginService.Authenticate directly, and handler.loginService.ResetAttempts as a statement directly below the arm's sign-in cookie issuance and its error return, on every arm that answers a correct password", strings.Join(sweep.problems, "; "))
	}

	// Anti-vacuity by name: Login is an Authenticate caller, and each of its
	// arms answers through the issuer that paid for its reset.
	arms, ok := sweep.exits["Handler.Login"]
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
	port         *types.Interface
	authenticate *types.Func
	reset        *types.Func
	issuers      map[*types.Func]string
	exits        map[string][]string
	problems     []string
}

// isAuthenticate reports whether function is the port's Authenticate or a
// method that implements it, so a caller holding a concrete service counts.
func (sweep *loginResetSweep) isAuthenticate(function *types.Func) bool {
	if function == nil {
		return false
	}
	if function == sweep.authenticate {
		return true
	}
	signature, ok := function.Type().(*types.Signature)
	if !ok || signature.Recv() == nil || function.Name() != sweep.authenticate.Name() {
		return false
	}
	return types.Implements(signature.Recv().Type(), sweep.port)
}

// body checks one function body, not the function literals nested in it
// (each is a body of its own), and records under name the sorted issuers whose
// resets pay for its success exits.
func (sweep *loginResetSweep) body(body *ast.BlockStmt, name string) {
	var first *ast.CallExpr
	called := map[*ast.Ident]bool{}
	loginInspectOwn(body, func(node ast.Node) {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return
		}
		switch fun := ast.Unparen(call.Fun).(type) {
		case *ast.SelectorExpr:
			called[fun.Sel] = true
		case *ast.Ident:
			called[fun] = true
		}
		if first == nil && sweep.isAuthenticate(loginStaticCallee(sweep.info, call)) {
			first = call
		}
	})
	loginInspectOwn(body, func(node ast.Node) {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok || called[selector.Sel] {
			return
		}
		if function, ok := sweep.info.Uses[selector.Sel].(*types.Func); ok && sweep.isAuthenticate(function.Origin()) {
			sweep.problems = append(sweep.problems, "Authenticate at "+sweep.where(selector)+" in "+name+" is taken as a value, so the exits of whatever calls it cannot be held")
		}
	})

	walk := loginExitWalk{sweep: sweep, name: name, placed: map[*ast.SelectorExpr]bool{}, paidBy: map[string]bool{}}
	if first != nil {
		walk.after = first.Pos()
		walk.block(body.List, "", false)
		var names []string
		for issuer := range walk.paidBy {
			names = append(names, issuer)
		}
		sort.Strings(names)
		sweep.exits[name] = names
	}
	loginInspectOwn(body, func(node ast.Node) {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok || sweep.info.Uses[selector.Sel] != sweep.reset || walk.placed[selector] {
			return
		}
		if first == nil {
			sweep.problems = append(sweep.problems, "ResetAttempts at "+sweep.where(selector)+" is reached from "+name+", which calls no Authenticate")
		} else {
			sweep.problems = append(sweep.problems, "ResetAttempts at "+sweep.where(selector)+" in "+name+" is not a direct call statement")
		}
	})
}

func (sweep *loginResetSweep) where(node ast.Node) string {
	return sweep.fset.Position(node.Pos()).String()
}

// loginExitWalk follows one body's statements path by path from its first
// Authenticate call.
type loginExitWalk struct {
	sweep  *loginResetSweep
	name   string
	after  token.Pos
	placed map[*ast.SelectorExpr]bool
	paidBy map[string]bool
}

// block walks statements on one path. paid names the issuer whose reset has
// already run on this path ("" for none); failing marks the body of an error
// guard, whose returns owe nothing.
func (walk *loginExitWalk) block(statements []ast.Stmt, paid string, failing bool) {
	sweep := walk.sweep
	for index, statement := range statements {
		if selector := loginResetCallStatement(sweep.info, statement, sweep.reset); selector != nil {
			walk.placed[selector] = true
			issuer := loginPrecedingIssuer(sweep.info, statements[:index], sweep.issuers)
			if issuer == "" {
				sweep.problems = append(sweep.problems, "ResetAttempts at "+sweep.where(selector)+" in "+walk.name+" follows no issued sign-in cookie in its block")
				continue
			}
			paid = issuer
			continue
		}
		switch statement := statement.(type) {
		case *ast.ReturnStmt:
			if statement.Pos() < walk.after || failing {
				continue
			}
			if paid == "" {
				sweep.problems = append(sweep.problems, walk.name+" answers at "+sweep.where(statement)+" after Authenticate without resetting the budget behind an issued sign-in cookie")
				continue
			}
			walk.paidBy[paid] = true
		case *ast.IfStmt:
			walk.block(statement.Body.List, paid, failing || loginErrorGuard(sweep.info, statement.Cond))
			if statement.Else != nil {
				walk.block([]ast.Stmt{statement.Else}, paid, failing)
			}
		case *ast.BlockStmt:
			walk.block(statement.List, paid, failing)
		case *ast.LabeledStmt:
			walk.block([]ast.Stmt{statement.Stmt}, paid, failing)
		case *ast.ForStmt:
			walk.block(statement.Body.List, paid, failing)
		case *ast.RangeStmt:
			walk.block(statement.Body.List, paid, failing)
		case *ast.SwitchStmt:
			walk.clauses(statement.Body, paid, failing)
		case *ast.TypeSwitchStmt:
			walk.clauses(statement.Body, paid, failing)
		case *ast.SelectStmt:
			walk.clauses(statement.Body, paid, failing)
		}
	}
}

func (walk *loginExitWalk) clauses(body *ast.BlockStmt, paid string, failing bool) {
	for _, clause := range body.List {
		switch clause := clause.(type) {
		case *ast.CaseClause:
			walk.block(clause.Body, paid, failing)
		case *ast.CommClause:
			walk.block(clause.Body, paid, failing)
		}
	}
}

// loginInspectOwn visits body's nodes without entering nested function literals.
func loginInspectOwn(body *ast.BlockStmt, visit func(ast.Node)) {
	ast.Inspect(body, func(node ast.Node) bool {
		if _, nested := node.(*ast.FuncLit); nested {
			return false
		}
		if node != nil {
			visit(node)
		}
		return true
	})
}

// loginServicePort is the interface type of Handler's loginService field.
func loginServicePort(t *testing.T, pkg *types.Package) *types.Interface {
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
	port, ok := fieldVar.Type().Underlying().(*types.Interface)
	if !ok {
		t.Fatalf("Handler.loginService is not an interface (%s)", fieldVar.Type())
	}
	return port
}

func loginPortMethod(t *testing.T, port *types.Interface, pkg *types.Package, name string) *types.Func {
	t.Helper()
	method, _, _ := types.LookupFieldOrMethod(port, false, pkg, name)
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

// loginErrorGuardOperand returns the checked expression when cond is
// `<value of type error> != nil`, and nil otherwise.
func loginErrorGuardOperand(info *types.Info, cond ast.Expr) ast.Expr {
	comparison, ok := ast.Unparen(cond).(*ast.BinaryExpr)
	if !ok || comparison.Op != token.NEQ {
		return nil
	}
	right, ok := ast.Unparen(comparison.Y).(*ast.Ident)
	if !ok || info.Uses[right] != types.Universe.Lookup("nil") {
		return nil
	}
	if !types.Identical(info.TypeOf(comparison.X), types.Universe.Lookup("error").Type()) {
		return nil
	}
	return ast.Unparen(comparison.X)
}

func loginErrorGuard(info *types.Info, cond ast.Expr) bool {
	return loginErrorGuardOperand(info, cond) != nil
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
	selector, _ := ast.Unparen(call.Fun).(*ast.SelectorExpr)
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
		if !ok {
			continue
		}
		checked, ok := loginErrorGuardOperand(info, guard.Cond).(*ast.Ident)
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
