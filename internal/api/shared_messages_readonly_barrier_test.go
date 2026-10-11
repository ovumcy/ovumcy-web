package api

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strings"
	"testing"
)

// The i18n manager hands out ONE catalogue map per language, shared by every
// request in the process. A write to that map — an index assignment, delete,
// clear, or a maps helper with the map as its destination — does not change one
// response, it changes every later response for every owner, and under
// concurrent requests it is a data race on a Go map (a fatal error, not a
// wrong value). So production code may only READ a map obtained from
// (*i18n.Manager).Messages or from currentMessages.
//
// The sources are resolved by declaration through go/types, never by the
// spelling of an identifier: a local helper that happens to be named Messages
// is not a source, and a renamed import still is.
//
// # What is covered
//
//   - A write whose map operand is a call to a source, or a variable that was
//     assigned or declared from one (aliases follow `b := a` as well).
//   - A write through a parameter of a function in the swept packages when any
//     call site passes a tainted map for that parameter.
//   - A write to the result of a function that returns a tainted map (such a
//     function becomes a source itself).
//   - Write shapes: m[k] = v, m[k] op= v, m[k]++ , delete, clear, and
//     maps.Copy / maps.Insert / maps.DeleteFunc with the map as the first
//     argument. maps.Clone and copying FROM the shared map are reads.
//
// # What is NOT covered
//
// The taint is a flow-insensitive, per-variable approximation, so these escape
// it: a map stored in a struct field, slice, map value, channel or interface{}
// and written after being read back; a map reaching a write through a closure
// variable captured from another package function's result tuple; a map from a
// multi-value assignment (`m, ok := f()`); range variables; a write through a
// function value (not a declared function) that received the map; reflection;
// an unsafe or cgo write; and any package outside cmd, internal, migrations,
// scripts and web. Test files are not swept. The fiber locals slot that
// carries the map is read back through currentMessages, which is a source, but
// a direct c.Locals(...) type assertion elsewhere would not be.

// typedUnit is one type-checked package, in the shape both the live tree and
// the self-test fixture can supply.
type typedUnit struct {
	fset  *token.FileSet
	files []*ast.File
	info  *types.Info
}

type messagesWriteFinding struct {
	position  token.Position
	enclosing string
	shape     string
}

type messagesScan struct {
	findings []messagesWriteFinding
	// sourceCalls records "<package>.<enclosing func> -> <callee name>" for every
	// call to a ROOT source (not a derived one), so the live sweep can assert
	// that known production call sites were really seen.
	sourceCalls map[string]bool
	// sources is the final set, roots plus derived (functions returning a
	// shared map).
	sources map[*types.Func]bool
}

// sharedMessagesRoots resolves the root sources from declarations: the Manager
// method, every interface method that *Manager implements and that is named
// like it, and the extra functions the caller names.
func sharedMessagesRoots(manager *types.Named, scopes []*types.Scope, extra []*types.Func) map[*types.Func]bool {
	roots := map[*types.Func]bool{}
	for index := 0; index < manager.NumMethods(); index++ {
		if method := manager.Method(index); method.Name() == "Messages" {
			roots[method] = true
		}
	}
	pointer := types.NewPointer(manager)
	for _, scope := range scopes {
		for _, name := range scope.Names() {
			typeName, ok := scope.Lookup(name).(*types.TypeName)
			if !ok {
				continue
			}
			iface, ok := typeName.Type().Underlying().(*types.Interface)
			if !ok || !types.Implements(pointer, iface) {
				continue
			}
			for index := 0; index < iface.NumMethods(); index++ {
				if method := iface.Method(index); method.Name() == "Messages" {
					roots[method] = true
				}
			}
		}
	}
	for _, function := range extra {
		roots[function] = true
	}
	return roots
}

// calleeObject resolves what a call invokes through types.Info.Uses.
func calleeObject(info *types.Info, call *ast.CallExpr) types.Object {
	function := unparen(call.Fun)
	switch typed := function.(type) {
	case *ast.IndexExpr:
		function = unparen(typed.X)
	case *ast.IndexListExpr:
		function = unparen(typed.X)
	}
	switch typed := function.(type) {
	case *ast.Ident:
		return info.Uses[typed]
	case *ast.SelectorExpr:
		return info.Uses[typed.Sel]
	}
	return nil
}

func calleeFunc(info *types.Info, call *ast.CallExpr) *types.Func {
	function, _ := calleeObject(info, call).(*types.Func)
	if function == nil {
		return nil
	}
	return function.Origin()
}

func enclosingName(info *types.Info, declaration *ast.FuncDecl) string {
	function, _ := info.Defs[declaration.Name].(*types.Func)
	if function == nil {
		return declaration.Name.Name
	}
	name := function.Name()
	if recv := function.Type().(*types.Signature).Recv(); recv != nil {
		receiver := recv.Type()
		if pointer, ok := receiver.(*types.Pointer); ok {
			receiver = pointer.Elem()
		}
		if named, ok := receiver.(*types.Named); ok {
			name = named.Obj().Name() + "." + name
		}
	}
	return function.Pkg().Name() + "." + name
}

func scanSharedMessagesWrites(units []typedUnit, roots map[*types.Func]bool) *messagesScan {
	scan := &messagesScan{sourceCalls: map[string]bool{}, sources: map[*types.Func]bool{}}
	for function := range roots {
		scan.sources[function] = true
	}
	tainted := map[types.Object]bool{}

	var isTainted func(info *types.Info, expression ast.Expr) bool
	isTainted = func(info *types.Info, expression ast.Expr) bool {
		switch typed := unparen(expression).(type) {
		case *ast.CallExpr:
			if function := calleeFunc(info, typed); function != nil && scan.sources[function] {
				return true
			}
			// A conversion map[string]string(x) keeps the same map.
			if conversion, ok := info.Types[typed.Fun]; ok && conversion.IsType() && len(typed.Args) == 1 {
				return isTainted(info, typed.Args[0])
			}
		case *ast.Ident:
			return tainted[info.Uses[typed]]
		}
		return false
	}

	taintTarget := func(info *types.Info, target ast.Expr) bool {
		identifier, ok := unparen(target).(*ast.Ident)
		if !ok || identifier.Name == "_" {
			return false
		}
		object := info.Defs[identifier]
		if object == nil {
			object = info.Uses[identifier]
		}
		if object == nil || tainted[object] {
			return false
		}
		tainted[object] = true
		return true
	}

	// propagate runs one flow-insensitive pass and reports whether it learned
	// anything; the caller iterates to a fixed point.
	propagate := func() bool {
		changed := false
		for _, unit := range units {
			info := unit.info
			var visit func(owner *types.Func) func(ast.Node) bool
			visit = func(owner *types.Func) func(ast.Node) bool {
				return func(node ast.Node) bool {
					switch typed := node.(type) {
					case *ast.FuncLit:
						ast.Inspect(typed.Body, visit(nil))
						return false
					case *ast.AssignStmt:
						if len(typed.Lhs) == len(typed.Rhs) {
							for index, right := range typed.Rhs {
								if isTainted(info, right) && taintTarget(info, typed.Lhs[index]) {
									changed = true
								}
							}
						}
					case *ast.ValueSpec:
						if len(typed.Names) == len(typed.Values) {
							for index, right := range typed.Values {
								if isTainted(info, right) && taintTarget(info, typed.Names[index]) {
									changed = true
								}
							}
						}
					case *ast.ReturnStmt:
						if owner != nil && !scan.sources[owner] {
							for _, result := range typed.Results {
								if isTainted(info, result) {
									scan.sources[owner] = true
									changed = true
									break
								}
							}
						}
					case *ast.CallExpr:
						function := calleeFunc(info, typed)
						if function == nil {
							break
						}
						params := function.Type().(*types.Signature).Params()
						for index, argument := range typed.Args {
							if index >= params.Len() || !isTainted(info, argument) {
								continue
							}
							if parameter := params.At(index); !tainted[parameter] {
								tainted[parameter] = true
								changed = true
							}
						}
					}
					return true
				}
			}
			for _, file := range unit.files {
				for _, declaration := range file.Decls {
					function, ok := declaration.(*ast.FuncDecl)
					if !ok || function.Body == nil {
						continue
					}
					owner, _ := info.Defs[function.Name].(*types.Func)
					ast.Inspect(function.Body, visit(owner))
				}
			}
		}
		return changed
	}
	for propagate() {
	}

	mapsWriters := map[string]bool{"Copy": true, "Insert": true, "DeleteFunc": true}
	for _, unit := range units {
		info := unit.info
		report := func(node ast.Node, enclosing string, shape string) {
			scan.findings = append(scan.findings, messagesWriteFinding{
				position:  unit.fset.Position(node.Pos()),
				enclosing: enclosing,
				shape:     shape,
			})
		}
		indexWrite := func(target ast.Expr, enclosing string, shape string) {
			if index, ok := unparen(target).(*ast.IndexExpr); ok && isTainted(info, index.X) {
				report(index, enclosing, shape)
			}
		}
		for _, file := range unit.files {
			for _, declaration := range file.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Body == nil {
					continue
				}
				enclosing := enclosingName(info, function)
				ast.Inspect(function.Body, func(node ast.Node) bool {
					switch typed := node.(type) {
					case *ast.AssignStmt:
						for _, left := range typed.Lhs {
							indexWrite(left, enclosing, "index assignment")
						}
					case *ast.IncDecStmt:
						indexWrite(typed.X, enclosing, "increment or decrement of an index")
					case *ast.CallExpr:
						if root := calleeFunc(info, typed); root != nil && roots[root] {
							scan.sourceCalls[enclosing+" -> "+root.Name()] = true
						} else if derived := calleeFunc(info, typed); derived != nil && scan.sources[derived] {
							scan.sourceCalls[enclosing+" -> "+derived.Name()] = true
						}
						if len(typed.Args) == 0 || !isTainted(info, typed.Args[0]) {
							break
						}
						switch callee := calleeObject(info, typed).(type) {
						case *types.Builtin:
							if callee.Name() == "delete" || callee.Name() == "clear" {
								report(typed, enclosing, callee.Name())
							}
						case *types.Func:
							if callee.Pkg() != nil && callee.Pkg().Path() == "maps" && mapsWriters[callee.Name()] {
								report(typed, enclosing, "maps."+callee.Name()+" destination")
							}
						}
					}
					return true
				})
			}
		}
	}
	return scan
}

func describeMessagesFindings(findings []messagesWriteFinding) string {
	lines := make([]string, 0, len(findings))
	for _, finding := range findings {
		lines = append(lines, fmt.Sprintf("%s:%d in %s: %s", finding.position.Filename, finding.position.Line, finding.enclosing, finding.shape))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n  ")
}

// TestNoProductionCodeWritesToTheSharedMessagesMap sweeps the shipped tree.
func TestNoProductionCodeWritesToTheSharedMessagesMap(t *testing.T) {
	evidence := loadTreeEvidence(t)

	i18nPackage := evidence.packageByPath(modulePath + "/internal/i18n")
	apiPackage := evidence.packageByPath(apiPackagePath)
	if i18nPackage == nil || apiPackage == nil {
		t.Fatalf("the sweep did not load the i18n and api packages; it would judge nothing")
	}
	manager, _ := i18nPackage.Types.Scope().Lookup("Manager").Type().(*types.Named)
	if manager == nil {
		t.Fatalf("i18n.Manager is not a declared type any more; the guard has lost its subject")
	}
	current, _ := apiPackage.Types.Scope().Lookup("currentMessages").(*types.Func)
	if current == nil {
		t.Fatalf("api.currentMessages is not a declared function any more; the guard has lost its subject")
	}

	var scopes []*types.Scope
	var units []typedUnit
	for _, pkg := range evidence.packages {
		scopes = append(scopes, pkg.Types.Scope())
		units = append(units, typedUnit{fset: pkg.Fset, files: pkg.Syntax, info: pkg.TypesInfo})
	}
	roots := sharedMessagesRoots(manager, scopes, []*types.Func{current})
	scan := scanSharedMessagesWrites(units, roots)

	// Anti-vacuity: known production call sites, by name. A sweep that stopped
	// seeing them would be green about a tree it no longer reads.
	for _, want := range []string{
		// (*i18n.Manager).Messages through a *Manager field.
		"bootstrap.i18nDisclaimerProvider.Message -> Messages",
		// currentMessages.
		"api.Handler.withTemplateDefaults -> currentMessages",
		// The interface method *Manager implements.
		"services.BuiltinSymptomReservedNames -> Messages",
	} {
		if !scan.sourceCalls[want] {
			t.Errorf("the sweep did not see the known call site %q; it is not looking at the tree it claims to judge", want)
		}
	}

	if len(scan.findings) > 0 {
		t.Fatalf("production code writes to the process-wide shared i18n catalogue map; copy it first (maps.Clone):\n  %s", describeMessagesFindings(scan.findings))
	}
}

const sharedMessagesFixture = `package fixture

import "maps"

type Manager struct{}

func (*Manager) Messages(language string) map[string]string { return nil }

type ctx struct{}

func currentMessages(c ctx) map[string]string { return nil }

func wrapped(c ctx) map[string]string { return currentMessages(c) }

func writeThrough(m map[string]string) { m["k"] = "v" }

func WriteIndexDirect(m *Manager)    { m.Messages("en")["k"] = "v" }
func WriteIndexCurrent(c ctx)         { currentMessages(c)["k"] = "v" }
func WriteOpAssign(c ctx)             { currentMessages(c)["k"] += "v" }
func WriteDelete(m *Manager)          { delete(m.Messages("en"), "k") }
func WriteClear(c ctx)                { clear(currentMessages(c)) }
func WriteMapsCopy(c ctx)             { maps.Copy(currentMessages(c), map[string]string{}) }
func WriteMapsInsert(c ctx)           { maps.Insert(currentMessages(c), maps.All(map[string]string{})) }
func WriteMapsDeleteFunc(c ctx)       { maps.DeleteFunc(currentMessages(c), func(string, string) bool { return true }) }
func WriteAlias(c ctx)                { a := currentMessages(c); b := a; b["k"] = "v" }
func WriteVarDecl(m *Manager)         { var a = m.Messages("en"); a["k"] = "v" }
func WriteParam(c ctx)                { writeThrough(currentMessages(c)) }
func WriteReturned(c ctx)             { wrapped(c)["k"] = "v" }
func WriteConverted(c ctx)            { map[string]string(currentMessages(c))["k"] = "v" }
func WriteInClosure(c ctx)            { a := currentMessages(c); func() { a["k"] = "v" }() }

func ReadOnly(c ctx, m *Manager) string {
	a := currentMessages(c)
	_ = len(a)
	for key := range m.Messages("en") {
		_ = key
	}
	return a["k"] + wrapped(c)["k"]
}

func CopyFromShared(c ctx) map[string]string {
	local := map[string]string{}
	maps.Copy(local, currentMessages(c))
	local["k"] = "v"
	return local
}

func CloneThenWrite(c ctx) {
	local := maps.Clone(currentMessages(c))
	local["k"] = "v"
	delete(local, "k")
}

func UnrelatedLocal() {
	local := map[string]string{}
	local["k"] = "v"
}
`

// TestSharedMessagesGuardRecognisesItsOwnFixtures anchors the guard on source
// it owns: every write shape must be flagged in its own function, and none of
// the read or copy-first shapes may be.
func TestSharedMessagesGuardRecognisesItsOwnFixtures(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture.go", sharedMessagesFixture, 0)
	if err != nil {
		t.Fatalf("parsing the fixture: %v", err)
	}
	info := &types.Info{
		Defs:  map[*ast.Ident]types.Object{},
		Uses:  map[*ast.Ident]types.Object{},
		Types: map[ast.Expr]types.TypeAndValue{},
	}
	config := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}
	pkg, err := config.Check("fixture", fset, []*ast.File{file}, info)
	if err != nil {
		t.Fatalf("type-checking the fixture: %v", err)
	}

	manager := pkg.Scope().Lookup("Manager").Type().(*types.Named)
	current := pkg.Scope().Lookup("currentMessages").(*types.Func)
	roots := sharedMessagesRoots(manager, []*types.Scope{pkg.Scope()}, []*types.Func{current})
	scan := scanSharedMessagesWrites([]typedUnit{{fset: fset, files: []*ast.File{file}, info: info}}, roots)

	flagged := map[string]int{}
	for _, finding := range scan.findings {
		flagged[finding.enclosing]++
	}

	wantFlagged := []string{
		"WriteIndexDirect", "WriteIndexCurrent", "WriteOpAssign", "WriteDelete", "WriteClear",
		"WriteMapsCopy", "WriteMapsInsert", "WriteMapsDeleteFunc", "WriteAlias", "WriteVarDecl",
		"WriteConverted", "WriteInClosure",
		// The write itself sits in writeThrough, reached through a tainted argument.
		"writeThrough",
		// The write sits in the caller of a function that returns the shared map.
		"WriteReturned",
	}
	for _, name := range wantFlagged {
		if flagged["fixture."+name] != 1 {
			t.Errorf("the guard flagged %q %d time(s), want 1; findings:\n  %s", name, flagged["fixture."+name], describeMessagesFindings(scan.findings))
		}
	}
	for _, name := range []string{"ReadOnly", "CopyFromShared", "CloneThenWrite", "UnrelatedLocal", "WriteParam", "wrapped", "currentMessages"} {
		if flagged["fixture."+name] != 0 {
			t.Errorf("the guard flagged the innocent %q; findings:\n  %s", name, describeMessagesFindings(scan.findings))
		}
	}
	if len(scan.findings) != len(wantFlagged) {
		t.Errorf("the guard produced %d finding(s), want %d:\n  %s", len(scan.findings), len(wantFlagged), describeMessagesFindings(scan.findings))
	}
	if !scan.sourceCalls["fixture.WriteIndexDirect -> Messages"] || !scan.sourceCalls["fixture.WriteIndexCurrent -> currentMessages"] {
		t.Errorf("the guard did not record the fixture's source calls: %v", scan.sourceCalls)
	}
}
