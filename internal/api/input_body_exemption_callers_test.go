package api

import (
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// TestExemptLookupCallersPassOnlyDeclaredKeys closes what an exemption in
// unresolvedKeyExemptions opens: the exempted function is itself a lookup whose
// member name is a parameter, so a caller passing "password" would read it from
// the URL unseen by the query-read sweep. Each exempted function is resolved by
// declaration in the type-checked package, and every use of it must be a direct
// call whose key argument is a constant from the exemption's keys, and the
// function must look up the very value it was passed (judgeExemptBody), or the
// callers' keys vouch for nothing.
func TestExemptLookupCallersPassOnlyDeclaredKeys(t *testing.T) {
	evidence := loadTreeEvidence(t)
	imports := importableTypes(evidence)
	lookups := fiberLookups(t, imports)
	assertExemptCallerJudgeAnswersBothWays(t)
	assertExemptBodyJudgeAnswersBothWays(t, imports, lookups)

	for _, exemption := range unresolvedKeyExemptions {
		pkg := exemptionPackage(t, evidence, exemption)
		fn := resolveExemptFunction(t, pkg.Types, exemption)
		if at := filepath.ToSlash(pkg.Fset.Position(fn.Pos()).Filename); !strings.HasSuffix(at, "/"+exemption.file) {
			t.Fatalf("%s is declared in %s, not %s", exemption.declKey(), at, exemption.file)
		}
		seen, violations := judgeExemptCallers(pkg.Fset, pkg.Syntax, pkg.TypesInfo, fn, exemption)
		for _, key := range exemption.keys {
			if !seen[key] {
				t.Errorf("no caller of %s passes %q: drop the key from the exemption, or the judge has stopped seeing the calls", exemption.declKey(), key)
			}
		}
		violations = append(violations, judgeExemptBody(pkg.Fset, pkg.Syntax, pkg.TypesInfo, fn, exemption, lookups)...)
		for _, violation := range violations {
			t.Errorf("%s", violation)
		}
	}
}

// exemptionPackage returns the type-checked package that declares exemption.
func exemptionPackage(t *testing.T, evidence *treeEvidence, exemption lookupExemption) *packages.Package {
	t.Helper()
	pkg := evidence.packageByPath(modulePath + "/" + path.Dir(exemption.file))
	if pkg == nil {
		t.Fatalf("%s: the type-checked tree has no package %s", exemption.declKey(), path.Dir(exemption.file))
	}
	return pkg
}

// exemptKeyParam resolves the parameter an exempted function's lookup is keyed
// by, and its index.
func exemptKeyParam(fn *types.Func, name string) (*types.Var, int) {
	params := fn.Signature().Params()
	for i := range params.Len() {
		if params.At(i).Name() == name {
			return params.At(i), i
		}
	}
	return nil, -1
}

func resolveExemptFunction(t *testing.T, pkg *types.Package, exemption lookupExemption) *types.Func {
	t.Helper()
	if exemption.receiver == "" {
		fn, ok := pkg.Scope().Lookup(exemption.function).(*types.Func)
		if !ok {
			t.Fatalf("%s: no function %s in package %s", exemption.declKey(), exemption.function, pkg.Path())
		}
		return fn
	}
	typeName, ok := pkg.Scope().Lookup(exemption.receiver).(*types.TypeName)
	if !ok {
		t.Fatalf("%s: no type %s in package %s", exemption.declKey(), exemption.receiver, pkg.Path())
	}
	named, ok := typeName.Type().(*types.Named)
	if !ok {
		t.Fatalf("%s: %s is not a named type", exemption.declKey(), exemption.receiver)
	}
	for method := range named.Methods() {
		if method.Name() == exemption.function {
			return method
		}
	}
	t.Fatalf("%s: %s has no method %s", exemption.declKey(), exemption.receiver, exemption.function)
	return nil
}

// judgeExemptCallers returns the keys seen at fn's call sites and one violation
// per use that is not a direct call passing an allowed constant key: a
// non-constant key, a key outside the exemption, or fn taken as a value, whose
// later calls cannot be traced.
func judgeExemptCallers(fset *token.FileSet, files []*ast.File, info *types.Info, fn *types.Func, exemption lookupExemption) (map[string]bool, []string) {
	_, keyIndex := exemptKeyParam(fn, exemption.keyParam)
	if keyIndex < 0 {
		return nil, []string{exemption.declKey() + " has no parameter " + exemption.keyParam}
	}
	allowed := map[string]bool{}
	for _, key := range exemption.keys {
		allowed[key] = true
	}

	seen := map[string]bool{}
	called := map[*ast.Ident]bool{}
	var violations []string
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			var ident *ast.Ident
			index := keyIndex
			switch fun := ast.Unparen(call.Fun).(type) {
			case *ast.Ident:
				ident = fun
			case *ast.SelectorExpr:
				ident = fun.Sel
				// (*Handler).lookup(h, c, "code") passes the receiver first.
				if selection := info.Selections[fun]; selection != nil && selection.Kind() == types.MethodExpr {
					index++
				}
			}
			if ident == nil || info.Uses[ident] != fn {
				return true
			}
			called[ident] = true
			at := fset.Position(call.Pos()).String()
			if index >= len(call.Args) {
				violations = append(violations, at+": "+exemption.function+" called without its "+exemption.keyParam+" argument")
				return true
			}
			value := info.Types[call.Args[index]].Value
			if value == nil || value.Kind() != constant.String {
				violations = append(violations, at+": "+exemption.function+" is passed a key that is not a string constant, so it can read any member from the URL")
				return true
			}
			key := constant.StringVal(value)
			if !allowed[key] {
				violations = append(violations, at+": "+exemption.function+" is passed "+strconv.Quote(key)+", outside the keys its exemption allows")
				return true
			}
			seen[key] = true
			return true
		})
	}
	for ident, object := range info.Uses {
		if object == fn && !called[ident] {
			violations = append(violations, fset.Position(ident.Pos()).String()+": "+exemption.function+" is used other than as a direct call, so the keys it is passed cannot be traced")
		}
	}
	return seen, violations
}

// assertExemptCallerJudgeAnswersBothWays type-checks a fixture package whose
// allowed calls must pass and whose other uses must each be reported.
func assertExemptCallerJudgeAnswersBothWays(t *testing.T) {
	t.Helper()
	const source = `package fixture

type Handler struct{}

func (h *Handler) lookup(c any, name string) string { return name }

const stateKey = "state"

func use(h *Handler, c any, name string) {
	_ = h.lookup(c, "code")
	_ = h.lookup(c, stateKey)
	_ = (h.lookup)(c, "error")
	_ = h.lookup(c, "password")
	_ = h.lookup(c, name)
	read := h.lookup
	_ = read(c, "code")
	_ = (*Handler).lookup(h, c, "code")
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	pkg, err := (&types.Config{}).Check("fixture", fset, []*ast.File{file}, info)
	if err != nil {
		t.Fatal(err)
	}
	exemption := lookupExemption{file: "fixture.go", receiver: "Handler", function: "lookup", keyParam: "name", keys: []string{"code", "state", "error"}}
	seen, violations := judgeExemptCallers(fset, []*ast.File{file}, info, resolveExemptFunction(t, pkg, exemption), exemption)

	for _, key := range exemption.keys {
		if !seen[key] {
			t.Errorf("the judge must accept the fixture's call passing %q", key)
		}
	}
	for _, line := range []string{"fixture.go:13:", "fixture.go:14:", "fixture.go:15:"} {
		found := false
		for _, violation := range violations {
			found = found || strings.HasPrefix(violation, line)
		}
		if !found {
			t.Errorf("the judge must report the use at %s; got %v", line, violations)
		}
	}
	if len(violations) != 3 {
		t.Errorf("the judge must report only the three bad uses; got %v", violations)
	}
}

// judgeExemptBody holds the exempted function to what its exemption assumes:
// every lookup inside it whose key is not a constant is keyed by keyParam, and
// keyParam still holds what the caller passed — nothing shadows it, assigns to
// it or takes its address. The parameter is resolved by declaration, so a
// variable that merely shares its name is not it.
func judgeExemptBody(fset *token.FileSet, files []*ast.File, info *types.Info, fn *types.Func, exemption lookupExemption, lookups map[*types.Func]bool) []string {
	keyVar, _ := exemptKeyParam(fn, exemption.keyParam)
	if keyVar == nil {
		return []string{exemption.declKey() + " has no parameter " + exemption.keyParam}
	}
	var body *ast.BlockStmt
	for _, file := range files {
		for _, decl := range file.Decls {
			if declaration, ok := decl.(*ast.FuncDecl); ok && info.Defs[declaration.Name] == fn {
				body = declaration.Body
			}
		}
	}
	if body == nil {
		return []string{exemption.declKey() + " has no body to judge"}
	}

	var violations []string
	report := func(node ast.Node, what string) {
		violations = append(violations, fset.Position(node.Pos()).String()+": "+exemption.function+" "+what+", so the key its callers pass is not the key it reads")
	}
	keyed := false
	ast.Inspect(body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.Ident:
			object := info.Defs[node]
			if object == nil {
				object = info.Uses[node]
			}
			if v, ok := object.(*types.Var); ok && v != keyVar && !v.IsField() && node.Name == exemption.keyParam {
				report(node, "shadows "+exemption.keyParam)
			}
		case *ast.AssignStmt:
			for _, lhs := range node.Lhs {
				if isVarUse(info, lhs, keyVar) {
					report(lhs, "assigns to "+exemption.keyParam)
				}
			}
		case *ast.RangeStmt:
			if node.Tok == token.ASSIGN && ((node.Key != nil && isVarUse(info, node.Key, keyVar)) || (node.Value != nil && isVarUse(info, node.Value, keyVar))) {
				report(node, "assigns to "+exemption.keyParam)
			}
		case *ast.UnaryExpr:
			if node.Op == token.AND && isVarUse(info, node.X, keyVar) {
				report(node, "takes the address of "+exemption.keyParam)
			}
		case *ast.CallExpr:
			if lookup, _, index := lookupCall(info, lookups, node); lookup != nil && index < len(node.Args) && info.Types[node.Args[index]].Value == nil {
				if isVarUse(info, node.Args[index], keyVar) {
					keyed = true
				} else {
					report(node, "looks a member up by something other than "+exemption.keyParam)
				}
			}
		}
		return true
	})
	if !keyed {
		violations = append(violations, exemption.declKey()+" holds no lookup keyed by "+exemption.keyParam+": the exemption clears nothing")
	}
	return violations
}

// assertExemptBodyJudgeAnswersBothWays type-checks one function the body judge
// must accept and one per shape it must refuse.
func assertExemptBodyJudgeAnswersBothWays(t *testing.T, imports fixtureImporter, lookups map[*types.Func]bool) {
	t.Helper()
	const source = `package fixture

import "github.com/gofiber/fiber/v3"

type Handler struct{ name string }

func (h *Handler) direct(c fiber.Ctx, name string) string {
	if h.name != "" {
		return string(c.Request().PostArgs().Peek(name))
	}
	return c.Query(name) + c.Query("day")
}

func (h *Handler) reassigned(c fiber.Ctx, name string) string {
	name = "password"
	return c.Query(name)
}

func (h *Handler) shadowed(c fiber.Ctx, name string) string {
	if name := "password"; name != "" {
		return c.Query(name)
	}
	return c.Query(name)
}

func (h *Handler) addressed(c fiber.Ctx, name string) string {
	*(&name) = "password"
	return c.Query(name)
}

func (h *Handler) rekeyed(c fiber.Ctx, name string) string {
	key := name + "_hash"
	return c.Query(name) + c.Query(key)
}

func (h *Handler) unkeyed(c fiber.Ctx, name string) string {
	return name + c.Query("day")
}

func (h *Handler) foreign(c fiber.Ctx, name string) string {
	key := name + "_hash"
	return c.Query(name) + string(c.RequestCtx().FormValue(key))
}
`
	fset := token.NewFileSet()
	file, pkg, info := typeCheckFixture(t, fset, "fixture", imports, source)
	for function, want := range map[string]string{
		"direct":     "",
		"reassigned": "assigns to name",
		"shadowed":   "shadows name",
		"addressed":  "takes the address of name",
		"rekeyed":    "looks a member up by something other than name",
		"unkeyed":    "holds no lookup keyed by name",
		// fasthttp's FormValue is no fiber lookup; isLookup takes it by name.
		"foreign": "looks a member up by something other than name",
	} {
		exemption := lookupExemption{file: "fixture.go", receiver: "Handler", function: function, keyParam: "name"}
		violations := judgeExemptBody(fset, []*ast.File{file}, info, resolveExemptFunction(t, pkg, exemption), exemption, lookups)
		found := false
		for _, violation := range violations {
			found = found || (want != "" && strings.Contains(violation, want))
		}
		switch {
		case want == "" && len(violations) != 0:
			t.Errorf("the body judge must accept %s; got %v", function, violations)
		case want != "" && !found:
			t.Errorf("the body judge must report %q in %s; got %v", want, function, violations)
		}
	}
}
