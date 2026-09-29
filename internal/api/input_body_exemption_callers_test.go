package api

import (
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
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
// call whose key argument is a constant from the exemption's keys.
func TestExemptLookupCallersPassOnlyDeclaredKeys(t *testing.T) {
	assertExemptCallerJudgeAnswersBothWays(t)

	root, err := moduleRootForBarrier()
	if err != nil {
		t.Fatal(err)
	}
	for _, exemption := range unresolvedKeyExemptions {
		config := &packages.Config{
			Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
				packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
			Dir: root,
		}
		loaded, err := packages.Load(config, "./"+filepath.ToSlash(filepath.Dir(exemption.file)))
		if err != nil {
			t.Fatalf("type-checking %s: %v", exemption.file, err)
		}
		if len(loaded) != 1 {
			t.Fatalf("type-checking %s: want one package, got %d", exemption.file, len(loaded))
		}
		if errs := loaded[0].Errors; len(errs) > 0 {
			t.Fatalf("type-checking %s: %v", exemption.file, errs)
		}
		pkg := loaded[0]

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
		for _, violation := range violations {
			t.Errorf("%s", violation)
		}
	}
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
	signature := fn.Type().(*types.Signature)
	keyIndex := -1
	for i := range signature.Params().Len() {
		if signature.Params().At(i).Name() == exemption.keyParam {
			keyIndex = i
		}
	}
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
