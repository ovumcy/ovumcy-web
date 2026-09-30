package api

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"golang.org/x/tools/go/packages"
)

// authInputKeys are the request members that are credentials, auth flags or
// security tokens. None of them may be read from the URL query string: a link
// is logged, cached, shared and prefetched, and a value planted in one must not
// stand in for — or shadow — what the request body carries.
var authInputKeys = map[string]bool{
	"password":         true,
	"code":             true,
	"remember_me":      true,
	"email":            true,
	"csrf_token":       true,
	"recovery_code":    true,
	"current_password": true,
	"new_password":     true,
	"confirm_password": true,
	"consent":          true,
}

// queryReadingLookups are the request-level lookups that consult the URL (its
// query string, its path parameters), alone or together with the body. Each
// takes the member name as its first argument as a method (c.Query("code")),
// and as its second as fiber's generic function (fiber.Query[string](c, "code")).
// fiberLookups resolves the names to their declarations in fiberPackagePath;
// lookupKey also takes a callee of one of these names declared anywhere else.
var queryReadingLookups = map[string]bool{"FormValue": true, "Query": true, "Params": true}

const fiberPackagePath = "github.com/gofiber/fiber/v3"

// wholeQueryReaders return the entire query string or every query member at
// once, so no key can clear them: nothing in the scanned source uses one, and a
// credential rides through them whatever it is named.
var wholeQueryReaders = map[string]bool{"Queries": true, "QueryString": true}

// queryArgsReaders are the methods of the request URI's query-argument set
// that take a member name.
var queryArgsReaders = map[string]bool{
	"Peek": true, "PeekBytes": true, "PeekMulti": true, "Has": true, "HasBytes": true,
	"GetBool": true, "GetUfloat": true, "GetUint": true, "GetUfloatOrZero": true, "GetUintOrZero": true,
}

// queryRead is one place a member is read from a source that includes the URL
// query string.
type queryRead struct {
	at   token.Pos
	key  string
	how  string
	bulk bool
	note string
	// unresolvedKey marks a lookup whose member name is not a string constant:
	// the only reads an exemption can clear.
	unresolvedKey bool
	keyArg        ast.Expr
}

func (read queryRead) describe() string {
	if read.bulk {
		return read.how + " (" + read.note + ")"
	}
	return read.how + "(" + strconv.Quote(read.key) + ")"
}

// lookupExemption names one production function allowed to look a member up
// under a name the guard cannot resolve, because that name is its keyParam.
// Exempting the lookup moves the guard to the function's callers:
// TestExemptLookupCallersPassOnlyDeclaredKeys resolves the function by
// declaration and requires every caller to pass a constant from keys.
type lookupExemption struct {
	file     string // path under the repository root
	receiver string // named receiver type; "" for a plain function
	function string
	keyParam string
	keys     []string
}

func (exemption lookupExemption) declKey() string {
	return exemption.file + " " + exemption.receiver + "." + exemption.function
}

// unresolvedKeyExemptions: oidcCallbackValue reads the provider's callback
// parameters from the URL under OIDC_RESPONSE_MODE=query. They are
// provider-originated, not user credentials, and the sealed one-time state
// cookie is what authorises the exchange. The exemption clears only a lookup
// inside that function whose key resolves to the keyParam variable itself — an
// auth member read by name there is still refused — and the sweep fails when an
// entry no longer clears anything, so it cannot outlive the code it excuses.
var unresolvedKeyExemptions = []lookupExemption{{
	file:     "internal/api/oidc_helpers.go",
	receiver: "Handler",
	function: "oidcCallbackValue",
	keyParam: "name",
	keys:     []string{"code", "state", "error"},
}}

// queryLookupAnchor is the production function whose c.Query(name) the sweep
// must resolve to a fiber lookup. A sweep that stopped recognising the lookups
// would find nothing to refuse, and pass.
var queryLookupAnchor = lookupExemption{file: "internal/api/oidc_helpers.go", receiver: "Handler", function: "oidcCallbackValue"}

// calleeSelector returns the selector a call goes through, looking past
// parentheses and the type-argument list of a generic call, as lookupCall does:
// fiber.Query[string](c, "code") has an index expression, not a selector, as its
// Fun, and (c.Queries)() a parenthesised one.
func calleeSelector(call *ast.CallExpr) (selector *ast.SelectorExpr, generic bool) {
	fun := ast.Unparen(call.Fun)
	switch f := fun.(type) {
	case *ast.IndexExpr:
		fun, generic = ast.Unparen(f.X), true
	case *ast.IndexListExpr:
		fun, generic = ast.Unparen(f.X), true
	}
	selector, _ = fun.(*ast.SelectorExpr)
	return selector, generic
}

// receiverChainHasCall reports whether a call to name sits anywhere in the
// receiver chain of expr, so c.Bind().WithoutAutoHandling().Query(&in) is seen
// as reaching Bind() however many modifiers sit in between.
func receiverChainHasCall(expr ast.Expr, name string) bool {
	for {
		switch e := expr.(type) {
		case *ast.CallExpr:
			selector, _ := calleeSelector(e)
			if selector == nil {
				return false
			}
			if selector.Sel.Name == name {
				return true
			}
			expr = selector.X
		case *ast.SelectorExpr:
			expr = e.X
		case *ast.ParenExpr:
			expr = e.X
		default:
			return false
		}
	}
}

// fiberLookups resolves queryReadingLookups to their declarations: every fiber
// function, and every method in the method set of a fiber type, of one of those
// names whose member-name parameter is a string. (*fiber.Bind).Query fills a
// struct rather than naming a member, and is judged as a bind instead.
func fiberLookups(t *testing.T, imports fixtureImporter) map[*types.Func]bool {
	t.Helper()
	fiberPkg := imports[fiberPackagePath]
	if fiberPkg == nil {
		t.Fatalf("the shipped tree does not reach %s, so the sweep has no lookup to resolve", fiberPackagePath)
	}
	lookups := map[*types.Func]bool{}
	add := func(fn *types.Func) {
		params := fn.Signature().Params()
		index := lookupKeyIndex(fn)
		if queryReadingLookups[fn.Name()] && index < params.Len() && types.Identical(params.At(index).Type(), types.Typ[types.String]) {
			lookups[fn] = true
		}
	}
	scope := fiberPkg.Scope()
	for _, name := range scope.Names() {
		switch object := scope.Lookup(name).(type) {
		case *types.Func:
			add(object)
		case *types.TypeName:
			for _, typ := range []types.Type{object.Type(), types.NewPointer(object.Type())} {
				methods := types.NewMethodSet(typ)
				for i := range methods.Len() {
					add(methods.At(i).Obj().(*types.Func))
				}
			}
		}
	}
	return lookups
}

// lookupKey reports whether obj, the object a call or a value names, reads a
// member from the URL, and at which argument it takes the member name: a fiber
// lookup by declaration, or, failing closed, a callee of a lookup's name the
// sweep cannot see through. That is any method declared outside fiber — a call
// through the caller's own interface (a local one, a type assertion) resolves to
// that interface's method, not fiber's, and a type outside the tree has no body
// the sweep reads: fasthttp's (*RequestCtx).FormValue consults the query before
// the body — a func-typed variable or struct field, which may hold any of them,
// and a plain function declared outside both fiber and this module (one in the
// module is swept itself). Only a callee whose key parameter is a string names a
// member: pgx's Query(ctx, sql) is no lookup. A method takes the key first; a
// callee without a receiver first too, or second after the request, as fiber's
// generic functions do.
func lookupKey(lookups map[*types.Func]bool, obj types.Object) (keyIndex int, ok bool) {
	if obj == nil || !queryReadingLookups[obj.Name()] {
		return 0, false
	}
	var signature *types.Signature
	switch obj := obj.(type) {
	case *types.Func:
		if lookups[obj.Origin()] {
			return lookupKeyIndex(obj), true
		}
		pkg := ""
		if obj.Pkg() != nil {
			pkg = obj.Pkg().Path()
		}
		signature = obj.Signature()
		if pkg == fiberPackagePath || (signature.Recv() == nil && (pkg == modulePath || strings.HasPrefix(pkg, modulePath+"/"))) {
			return 0, false
		}
	case *types.Var:
		signature, _ = obj.Type().Underlying().(*types.Signature)
		if signature == nil {
			return 0, false
		}
	default:
		return 0, false
	}
	isString := func(index int) bool {
		params := signature.Params()
		return index < params.Len() && types.Identical(params.At(index).Type().Underlying(), types.Typ[types.String])
	}
	switch {
	case isString(0):
		return 0, true
	case signature.Recv() == nil && isString(1):
		return 1, true
	}
	return 0, false
}

// lookupKeyIndex is where a lookup takes the member name: first for a method,
// second for fiber's generic functions, which take the request first.
func lookupKeyIndex(fn *types.Func) int {
	if fn.Signature().Recv() == nil {
		return 1
	}
	return 0
}

// lookupCall resolves call to the lookup it calls (lookupKey), by declaration:
// an aliased import, a parenthesised callee and a method expression
// (fiber.Ctx.Query(c, "code"), which takes the receiver first) are all the same
// lookup. lookup is nil when call is not a lookup.
func lookupCall(info *types.Info, lookups map[*types.Func]bool, call *ast.CallExpr) (lookup types.Object, callee *ast.Ident, keyIndex int) {
	fun := ast.Unparen(call.Fun)
	switch f := fun.(type) {
	case *ast.IndexExpr:
		fun = ast.Unparen(f.X)
	case *ast.IndexListExpr:
		fun = ast.Unparen(f.X)
	}
	methodExpr := false
	switch f := fun.(type) {
	case *ast.Ident:
		callee = f
	case *ast.SelectorExpr:
		callee = f.Sel
		selection := info.Selections[f]
		methodExpr = selection != nil && selection.Kind() == types.MethodExpr
	default:
		return nil, nil, 0
	}
	lookup = info.Uses[callee]
	keyIndex, ok := lookupKey(lookups, lookup)
	if !ok {
		return nil, nil, 0
	}
	if methodExpr {
		keyIndex++
	}
	return lookup, callee, keyIndex
}

// describeLookup names a lookup in a report: fiber's generic functions apart
// from the methods of the same name.
func describeLookup(lookup types.Object) string {
	if fn, ok := lookup.(*types.Func); ok && fn.Signature().Recv() == nil && fn.Pkg() != nil && fn.Pkg().Path() == fiberPackagePath {
		return "generic " + fn.Name()
	}
	return lookup.Name()
}

// findQueryReads returns every read in node that takes an auth member from the
// URL. A lookup is recognised by the declaration it calls (lookupCall), and its
// key by the string constant the argument denotes — a literal, a constant from
// any package, a constant expression — so a key spelled through any of them is
// judged like the literal. A key that is not a constant is
// reported as untraceable, as a wrapper such as
// func field(c fiber.Ctx, name string) string { return c.FormValue(name) }
// would otherwise hand a password past the guard, and so is a lookup taken as a
// value, whose later key cannot be traced. Reads that name no member are
// reported whatever the struct or key, because they hand back a password as
// readily as anything else: a Query or All bind whose receiver chain contains
// Bind() (Bind() itself is refused when held in a variable, where its later
// calls cannot be traced), Queries(), QueryString(), and a QueryArgs() that is
// not used as the direct receiver of one member read with a key that is not an
// auth member (held in a variable, ranged over, passed on). These bulk shapes
// are matched by method name, which over-approximates and so fails closed.
func findQueryReads(node ast.Node, info *types.Info, lookups map[*types.Func]bool) []queryRead {
	var reads []queryRead
	var stack []ast.Node
	called := map[*ast.Ident]bool{}
	ast.Inspect(node, func(n ast.Node) bool {
		if n == nil {
			stack = stack[:len(stack)-1]
			return true
		}
		ancestors := stack
		stack = append(stack, n)

		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if lookup, callee, index := lookupCall(info, lookups, call); lookup != nil {
			called[callee] = true
			how := describeLookup(lookup)
			key, resolved := memberKey(info, call, index)
			switch {
			case resolved && authInputKeys[key]:
				reads = append(reads, queryRead{at: call.Pos(), key: key, how: how})
			case !resolved && len(call.Args) > index:
				reads = append(reads, queryRead{at: call.Pos(), how: how, bulk: true, unresolvedKey: true, keyArg: call.Args[index],
					note: "its member name is not a string constant, so a credential can be read through it unseen"})
			}
			return true
		}
		selector, generic := calleeSelector(call)
		if selector == nil {
			return true
		}
		name := selector.Sel.Name

		switch {
		case !generic && (name == "Query" || name == "All") && receiverChainHasCall(selector.X, "Bind"):
			reads = append(reads, queryRead{at: call.Pos(), how: "Bind()..." + name, bulk: true,
				note: "fills every field of its target, credentials included"})
		case !generic && name == "Bind" && len(call.Args) == 0 && !isDirectReceiver(ancestors, call):
			reads = append(reads, queryRead{at: call.Pos(), how: "Bind() held outside a call chain", bulk: true,
				note: "its Query and All cannot be traced from here"})
		case wholeQueryReaders[name]:
			reads = append(reads, queryRead{at: call.Pos(), how: name + "()", bulk: true,
				note: "returns every query member, credentials included"})
		case !generic && name == "QueryArgs":
			if read, flagged := queryArgsRead(ancestors, call, info); flagged {
				reads = append(reads, read)
			}
		}
		return true
	})
	ast.Inspect(node, func(n ast.Node) bool {
		ident, ok := n.(*ast.Ident)
		if !ok || called[ident] {
			return true
		}
		if lookup := info.Uses[ident]; lookup != nil {
			if _, ok := lookupKey(lookups, lookup); !ok {
				return true
			}
			reads = append(reads, queryRead{at: ident.Pos(), how: lookup.Name() + " taken as a value", bulk: true,
				note: "the member it is later asked for cannot be traced from here"})
		}
		return true
	})
	return reads
}

// isDirectReceiver reports whether call is the receiver of a selector that is
// itself called, given the ancestors that lead to it: c.Bind().Body(..) yes,
// b := c.Bind() no.
func isDirectReceiver(ancestors []ast.Node, call *ast.CallExpr) bool {
	if len(ancestors) == 0 {
		return false
	}
	parent, ok := ancestors[len(ancestors)-1].(*ast.SelectorExpr)
	return ok && parent.X == call
}

// queryArgsRead judges a QueryArgs() call. It is cleared only as the direct
// receiver of a member read whose key is a known non-auth member; a read of an
// auth member names it, and any other use is reported as an untraceable one.
func queryArgsRead(ancestors []ast.Node, call *ast.CallExpr, info *types.Info) (queryRead, bool) {
	if isDirectReceiver(ancestors, call) && len(ancestors) >= 2 {
		parent, _ := ancestors[len(ancestors)-1].(*ast.SelectorExpr)
		if outer, ok := ancestors[len(ancestors)-2].(*ast.CallExpr); ok && outer.Fun == parent && queryArgsReaders[parent.Sel.Name] {
			if key, ok := memberKey(info, outer, 0); ok {
				if authInputKeys[key] {
					return queryRead{at: outer.Pos(), key: key, how: "QueryArgs()." + parent.Sel.Name}, true
				}
				return queryRead{}, false
			}
		}
	}
	return queryRead{at: call.Pos(), how: "QueryArgs() outside a direct member read", bulk: true,
		note: "the member it is asked for cannot be traced from here"}, true
}

// memberKey resolves the argument at index of a call to the string constant it
// denotes, whatever spells it.
func memberKey(info *types.Info, call *ast.CallExpr, index int) (string, bool) {
	if len(call.Args) <= index {
		return "", false
	}
	value := info.Types[call.Args[index]].Value
	if value == nil || value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(value), true
}

// isVarUse reports whether expr is an identifier that resolves to v.
func isVarUse(info *types.Info, expr ast.Expr, v *types.Var) bool {
	ident, ok := ast.Unparen(expr).(*ast.Ident)
	return ok && info.Uses[ident] == v
}

// TestAuthFieldsAreNeverReadFromTheQueryString derives every read of an auth
// member from the production source and refuses any that reaches the URL.
// fiber's FormValue searches the query BEFORE the body, so a `?remember_me=1`
// outranked the body's own value; the class is closed here at every site — a
// handler added later is judged by the same rule, and reads its input through
// bindRequestBody. The one exemption is unresolvedKeyExemptions: a single named
// function may look a member up under a name the guard cannot resolve, because
// a lookup it cannot resolve is otherwise refused as untraceable.
//
// Scope: every package the declaration barrier type-checks (loadTreeEvidence),
// not only this one. Nothing outside internal/api reads the request today; the
// wider net costs nothing, since the tree is loaded once per test binary. The
// load sees only the host's build, so every other non-test file under internal/
// and cmd/ is judged by spelling (filesOutsideTheLoad, untypedQueryReads).
func TestAuthFieldsAreNeverReadFromTheQueryString(t *testing.T) {
	evidence := loadTreeEvidence(t)
	imports := importableTypes(evidence)
	lookups := fiberLookups(t, imports)
	assertQueryReadSweepAnswersBothWays(t, imports, lookups)

	exempted := make([]*types.Func, len(unresolvedKeyExemptions))
	exemptKeys := map[*types.Func]*types.Var{}
	for i, exemption := range unresolvedKeyExemptions {
		exempted[i] = resolveExemptFunction(t, exemptionPackage(t, evidence, exemption).Types, exemption)
		keyVar, _ := exemptKeyParam(exempted[i], exemption.keyParam)
		if keyVar == nil {
			t.Fatalf("%s has no parameter %s", exemption.declKey(), exemption.keyParam)
		}
		exemptKeys[exempted[i]] = keyVar
	}
	anchor := resolveExemptFunction(t, exemptionPackage(t, evidence, queryLookupAnchor).Types, queryLookupAnchor)

	sawAnchor := false
	exemptionsUsed := map[*types.Func]bool{}
	var violations []string
	for _, pkg := range evidence.packages {
		info := pkg.TypesInfo
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				var declared *types.Func
				if fn, ok := decl.(*ast.FuncDecl); ok {
					declared, _ = info.Defs[fn.Name].(*types.Func)
				}
				if declared != nil && declared == anchor {
					sawAnchor = containsLookup(decl, info, lookups)
				}
				for _, read := range findQueryReads(decl, info, lookups) {
					if keyVar := exemptKeys[declared]; keyVar != nil && read.unresolvedKey && isVarUse(info, read.keyArg, keyVar) {
						exemptionsUsed[declared] = true
						continue
					}
					violations = append(violations, relativePosition(t, pkg.Fset.Position(read.at))+": "+read.describe())
				}
			}
		}
	}
	if !sawAnchor {
		t.Fatalf("the sweep resolved no fiber lookup inside %s, which reads c.Query(name): it is not reading the code it claims to police", queryLookupAnchor.declKey())
	}
	for i, exemption := range unresolvedKeyExemptions {
		if !exemptionsUsed[exempted[i]] {
			t.Errorf("unresolvedKeyExemptions names %q, which no longer holds a lookup keyed by %s: remove the entry", exemption.declKey(), exemption.keyParam)
		}
	}
	sort.Strings(violations)
	for _, violation := range violations {
		t.Errorf("%s reads an auth input from a source that includes the URL — read it from the body with bindRequestBody, which never consults it", violation)
	}

	assertUntypedSweepAnswersBothWays(t)
	root, outside := filesOutsideTheLoad(t, evidence)
	for _, relative := range outside {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filepath.Join(root, relative), nil, 0)
		if err != nil {
			t.Fatalf("parsing %s, which the load left out: %v", relative, err)
		}
		for _, ident := range untypedQueryReads(file) {
			t.Errorf("%s:%d: %s is spelled in a file build constraints keep out of the type-checked tree, so the sweep cannot tell whether it reads the URL — make the file build on the host, or move the read into one that does",
				relative, fset.Position(ident.Pos()).Line, ident.Name)
		}
	}
}

// filesOutsideTheLoad returns the module root and every non-test Go file under
// internal/ and cmd/ that the type-checked tree does not hold, relative to that
// root: a file a build constraint keeps out of the host's build (another GOOS, a
// tag), or one in a directory the load does not reach. The sweep above judges
// only what the load type-checked, so these are judged by spelling instead. A
// type-checked file the walk did not find fails the test: the walk and the load
// must describe the same tree, or the remainder means nothing.
func filesOutsideTheLoad(t *testing.T, evidence *treeEvidence) (string, []string) {
	t.Helper()
	root, err := moduleRootForBarrier()
	if err != nil {
		t.Fatal(err)
	}
	typed := map[string]bool{}
	for _, pkg := range evidence.packages {
		for _, file := range pkg.Syntax {
			if relative, err := filepath.Rel(root, pkg.Fset.Position(file.Package).Filename); err == nil {
				typed[filepath.ToSlash(relative)] = true
			}
		}
	}
	walked := map[string]bool{}
	var outside []string
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			walked[relative] = true
			if !typed[relative] {
				outside = append(outside, relative)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", dir, err)
		}
	}
	var unwalked []string
	for relative := range typed {
		if (strings.HasPrefix(relative, "internal/") || strings.HasPrefix(relative, "cmd/")) && !walked[relative] {
			unwalked = append(unwalked, relative)
		}
	}
	if len(unwalked) > 0 {
		sort.Strings(unwalked)
		t.Fatalf("the load type-checked %d file(s) the walk of internal/ and cmd/ did not find, first %s: the two do not describe the same tree", len(unwalked), unwalked[0])
	}
	sort.Strings(outside)
	return root, outside
}

// untypedQueryReads is the sweep for a file the load did not type-check. With
// no types a lookup cannot be told from any other method of its name, so every
// identifier spelled as one of the readers findQueryReads judges is reported,
// failing closed.
func untypedQueryReads(file *ast.File) []*ast.Ident {
	var idents []*ast.Ident
	ast.Inspect(file, func(n ast.Node) bool {
		if ident, ok := n.(*ast.Ident); ok {
			if name := ident.Name; queryReadingLookups[name] || wholeQueryReaders[name] || name == "QueryArgs" || name == "Bind" {
				idents = append(idents, ident)
			}
		}
		return true
	})
	return idents
}

// assertUntypedSweepAnswersBothWays parses a fixture whose reader spellings must
// each be reported, and whose other declarations must not.
func assertUntypedSweepAnswersBothWays(t *testing.T) {
	t.Helper()
	const source = `package fixture

func read(c interface{ FormValue(string) string }) string { return c.FormValue("password") }

func bulk(c ctx) { _ = c.Queries(); _ = c.Request().URI().QueryArgs(); _ = c.Bind() }

func clean(name string) string { return name }
`
	file, err := parser.ParseFile(token.NewFileSet(), "fixture.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, ident := range untypedQueryReads(file) {
		got = append(got, ident.Name)
	}
	if want := "FormValue FormValue Queries QueryArgs Bind"; strings.Join(got, " ") != want {
		t.Errorf("the untyped sweep must report exactly %s; got %v", want, got)
	}
}

func containsLookup(node ast.Node, info *types.Info, lookups map[*types.Func]bool) bool {
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if lookup, _, _ := lookupCall(info, lookups, call); lookup != nil {
				found = true
			}
		}
		return !found
	})
	return found
}

// fixtureImporter resolves a fixture's imports to the packages the shipped tree
// type-checked, so a fixture's fiber calls resolve to the declarations
// fiberLookups collected.
type fixtureImporter map[string]*types.Package

func (imports fixtureImporter) Import(path string) (*types.Package, error) {
	if pkg := imports[path]; pkg != nil {
		return pkg, nil
	}
	return nil, fmt.Errorf("the shipped tree does not reach %s", path)
}

func importableTypes(evidence *treeEvidence) fixtureImporter {
	imports := fixtureImporter{}
	packages.Visit(evidence.packages, nil, func(pkg *packages.Package) {
		imports[pkg.PkgPath] = pkg.Types
	})
	return imports
}

func typeCheckFixture(t *testing.T, fset *token.FileSet, path string, imports types.Importer, source string) (*ast.File, *types.Package, *types.Info) {
	t.Helper()
	file, err := parser.ParseFile(fset, path+".go", source, 0)
	if err != nil {
		t.Fatalf("parsing fixture %s: %v", path, err)
	}
	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	pkg, err := (&types.Config{Importer: imports}).Check(path, fset, []*ast.File{file}, info)
	if err != nil {
		t.Fatalf("type-checking fixture %s: %v", path, err)
	}
	return file, pkg, info
}

// queryReadFixtureHeader opens the fixture package every sweep fixture is
// type-checked in: fiber imported both plainly and under an alias, and keys
// spelled as constants of this package, of another, and as constant expressions.
const queryReadFixtureHeader = `package fixture

import (
	"context"
	"net/url"
	"strings"

	"fixture/routes"
	"github.com/gofiber/fiber/v3"
	fb "github.com/gofiber/fiber/v3"
)

const csrfFieldName = "csrf_token"

const prefix = "pass"

const (
	passwordKey = prefix + "word"
	dayKey      = "d" + "ay"
)

func lookupName() string { return "" }

type former interface{ FormValue(key string, def ...string) string }

type reader struct{ FormValue func(key string) string }

type store struct{}

func (store) Query(ctx context.Context, sql string) error { return nil }

func bindRequestBody(c fiber.Ctx, out any) error { return nil }
`

const queryReadFixtureRoutes = `package routes

const IDParam = "id"

const CodeParam = "co" + "de"

func Params(key string) string { return key }
`

// assertQueryReadSweepAnswersBothWays anchors the sweep on fixtures it owns: one
// body per read shape that must be flagged and bodies that must not, each
// type-checked against the tree's own fiber, so a finder that stopped
// recognising a shape cannot report success over a package it no longer
// understands. A flagged fixture names the start of what the sweep must report,
// so a key that stops resolving shows up as a changed report, not a pass.
func assertQueryReadSweepAnswersBothWays(t *testing.T, imports fixtureImporter, lookups map[*types.Func]bool) {
	t.Helper()

	flagged := []struct{ source, want string }{
		{`_ = c.FormValue("password")`, `FormValue("password")`},
		{`_ = c.Query("code")`, `Query("code")`},
		{`_ = c.Req().FormValue("password")`, `FormValue("password")`},
		{`_ = c.Request().URI().QueryArgs().Peek("email")`, `QueryArgs().Peek("email")`},
		{`_ = c.Request().URI().QueryArgs().Has("remember_me")`, `QueryArgs().Has("remember_me")`},
		{`_ = c.Bind().Query(&in)`, "Bind()...Query"},
		{`_ = c.Bind().All(&in)`, "Bind()...All"},
		{`_ = c.Bind().WithoutAutoHandling().Query(&in)`, "Bind()...Query"},
		{`_ = c.Bind().SkipValidation(true).All(&in)`, "Bind()...All"},
		{`b := c.Bind(); _ = b.Query(&in)`, "Bind() held"},
		{`_ = fiber.Query[string](c, "password")`, `generic Query("password")`},
		{`_ = fiber.Query[string](c, csrfFieldName)`, `generic Query("csrf_token")`},
		{`_ = fiber.Params[string](c, "code")`, `generic Params("code")`},
		{`_ = c.Params("password")`, `Params("password")`},
		{`_ = c.Queries()["code"]`, "Queries()"},
		{`_ = c.Req().Queries()`, "Queries()"},
		{`_ = c.Request().URI().QueryString()`, "QueryString()"},
		{`args := c.Request().URI().QueryArgs(); _ = args.Peek("password")`, "QueryArgs() outside"},
		{`c.Request().URI().QueryArgs().VisitAll(func(k, v []byte) {})`, "QueryArgs() outside"},
		{`_ = c.Request().URI().QueryArgs().Peek(name)`, "QueryArgs() outside"},
		{`_ = c.FormValue(csrfFieldName)`, `FormValue("csrf_token")`},
		{`_ = c.FormValue("csrf_token")`, `FormValue("csrf_token")`},
		{`_ = strings.TrimSpace(c.FormValue("consent"))`, `FormValue("consent")`},
		{`_ = c.Request().URI().QueryArgs().Peek("recovery_code")`, `QueryArgs().Peek("recovery_code")`},
		{`_ = c.FormValue("current_password")`, `FormValue("current_password")`},
		{`_ = c.FormValue("new_password") + c.FormValue("confirm_password")`, `FormValue("new_password")`},
		{`key := "password"; _ = c.FormValue(key)`, "FormValue (its member name is not a string constant"},
		{`field := func(c fiber.Ctx, name string) string { return c.FormValue(name) }; _ = field(c, "password")`, "FormValue (its member name"},
		{`_ = c.Query("co" + "de")`, `Query("code")`},
		{`_ = c.Params(lookupName())`, "Params (its member name"},
		{`_ = fiber.Query[string](c, name)`, "generic Query (its member name"},
		{`_ = fiber.Query(c, "password", "")`, `generic Query("password")`},
		{`_ = fb.Query(c, "password", "")`, `generic Query("password")`},
		{`_ = fb.Params[string](c, "code")`, `generic Params("code")`},
		{`_ = fb.Ctx.Query(c, "password")`, `Query("password")`},
		{`_ = (c.Query)("code")`, `Query("code")`},
		{`_ = c.Query(routes.CodeParam)`, `Query("code")`},
		{`_ = c.FormValue(passwordKey)`, `FormValue("password")`},
		{`query := c.Query; _ = query("day")`, "Query taken as a value"},
		{`_ = c.RequestCtx().FormValue("password")`, `FormValue("password")`},
		{`var f former = c; _ = f.FormValue("password")`, `FormValue("password")`},
		{`_ = c.(interface{ Query(string, ...string) string }).Query("code")`, `Query("code")`},
		{`form := c.RequestCtx().FormValue; _ = form("lang")`, "FormValue taken as a value"},
		{`_ = (c.Queries)()`, "Queries()"},
		{`_ = (c.Request().URI().QueryString)()`, "QueryString()"},
		{`_ = (c.Bind().Query)(&in)`, "Bind()...Query"},
		{`_ = ((c.Bind)().All)(&in)`, "Bind()...All"},
		{`b := (c.Bind)(); _ = b.Query(&in)`, "Bind() held"},
		{`_ = (c.Request().URI().QueryArgs)().Peek("password")`, `QueryArgs().Peek("password")`},
		{`_ = reader{}.FormValue("password")`, `FormValue("password")`},
		{`Query := func(key string) string { return key }; _ = Query("code")`, `Query("code")`},
		{`_ = routes.Params("password")`, `Params("password")`},
	}
	unflagged := []string{
		`_ = c.FormValue("lang")`,
		`_ = c.Query("day")`,
		`_ = c.Request().URI().QueryArgs().Has("age_group")`,
		`_ = c.Request().PostArgs().Peek("password")`,
		`_ = bindRequestBody(c, &in)`,
		`_ = logoutURL.Query()`,
		`_ = logoutURL.Query().Get("password")`,
		`_ = fiber.Query[string](c, "day")`,
		`_ = fiber.Query(c, "day", 0)`,
		`_ = fiber.Params[string](c, "id")`,
		`_ = c.Params("id")`,
		`_ = c.Bind().Body(&in)`,
		`_ = c.Bind().WithoutAutoHandling().Body(&in)`,
		`_ = c.Bind().Header(&in)`,
		`_ = fb.Query(c, "day", 0)`,
		`_ = fb.Params[string](c, "id")`,
		`_ = c.Params(routes.IDParam)`,
		`_ = c.Query(dayKey)`,
		`_ = c.RequestCtx().FormValue("lang")`,
		`_ = store{}.Query(context.Background(), "select 1")`,
	}

	source := strings.Builder{}
	source.WriteString(queryReadFixtureHeader)
	fixtures := map[string]string{}
	want := map[string]string{}
	addFixture := func(name, body, wantPrefix string) {
		fixtures[name], want[name] = body, wantPrefix
		fmt.Fprintf(&source, "\nfunc %s(c fiber.Ctx, name string, in struct{ Field string }, logoutURL *url.URL) {\n\t%s\n}\n", name, body)
	}
	for i, fixture := range flagged {
		addFixture("flagged"+strconv.Itoa(i), fixture.source, fixture.want)
	}
	for i, body := range unflagged {
		addFixture("unflagged"+strconv.Itoa(i), body, "")
	}

	fset := token.NewFileSet()
	_, routes, _ := typeCheckFixture(t, fset, "fixture/routes", imports, queryReadFixtureRoutes)
	withRoutes := fixtureImporter{"fixture/routes": routes}
	for path, pkg := range imports {
		withRoutes[path] = pkg
	}
	file, _, info := typeCheckFixture(t, fset, "fixture", withRoutes, source.String())

	judged := 0
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fixtures[fn.Name.Name] == "" {
			continue
		}
		judged++
		body, wantPrefix := fixtures[fn.Name.Name], want[fn.Name.Name]
		reads := findQueryReads(fn, info, lookups)
		switch {
		case wantPrefix == "" && len(reads) != 0:
			t.Errorf("the sweep must not flag %s, got %s", body, reads[0].describe())
		case wantPrefix != "" && len(reads) == 0:
			t.Errorf("the sweep must flag %s", body)
		case wantPrefix != "" && !strings.HasPrefix(reads[0].describe(), wantPrefix):
			t.Errorf("the sweep flagged %s as %s, want %s", body, reads[0].describe(), wantPrefix)
		}
	}
	if judged != len(fixtures) {
		t.Fatalf("the sweep judged %d of %d fixtures", judged, len(fixtures))
	}
}

func isUnprocessable(err error) bool {
	var fiberErr *fiber.Error
	return errors.As(err, &fiberErr) && fiberErr.Code == fiber.StatusUnprocessableEntity
}

// TestBindRequestBodyReadsOnlyTheBody drives the helper through every body
// transport with a conflicting `?field=` in the query: the body's value wins
// where the transport has one, and the query's value is never the answer. A
// body type the helper does not accept for auth inputs (XML, CBOR, MsgPack, a
// vendor "+json") is refused with 422 and fills nothing, even where the binder
// underneath would have decoded it.
func TestBindRequestBodyReadsOnlyTheBody(t *testing.T) {
	type target struct {
		Field string `json:"field" form:"field"`
	}

	gzipped := func(payload string) []byte {
		var buffer bytes.Buffer
		writer := gzip.NewWriter(&buffer)
		if _, err := writer.Write([]byte(payload)); err != nil {
			t.Fatalf("gzip: %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("gzip close: %v", err)
		}
		return buffer.Bytes()
	}
	multipartBody := func() ([]byte, string) {
		var buffer bytes.Buffer
		writer := multipart.NewWriter(&buffer)
		if err := writer.WriteField("field", "from-body"); err != nil {
			t.Fatalf("multipart field: %v", err)
		}
		if err := writer.Close(); err != nil {
			t.Fatalf("multipart close: %v", err)
		}
		return buffer.Bytes(), writer.FormDataContentType()
	}
	multipartBytes, multipartType := multipartBody()

	cases := []struct {
		name        string
		contentType string
		encoding    string
		body        []byte
		want        string
		wantErr     func(error) bool
	}{
		{name: "json", contentType: "application/json", body: []byte(`{"field":"from-body"}`), want: "from-body"},
		{name: "json without the member", contentType: "application/json", body: []byte(`{}`), want: ""},
		{name: "urlencoded", contentType: "application/x-www-form-urlencoded", body: []byte("field=from-body"), want: "from-body"},
		{name: "urlencoded without the member", contentType: "application/x-www-form-urlencoded", body: []byte("other=1"), want: ""},
		{name: "multipart", contentType: multipartType, body: multipartBytes, want: "from-body"},
		{name: "mixed-case form content type", contentType: "Application/X-WWW-Form-Urlencoded", body: []byte("field=from-body"), want: "from-body"},
		{name: "mixed-case form content type without the member", contentType: "Application/X-WWW-Form-Urlencoded", body: []byte("other=1"), want: ""},
		{name: "gzip json", contentType: "application/json", encoding: "gzip", body: gzipped(`{"field":"from-body"}`), want: "from-body"},
		{name: "json with parameters", contentType: "application/json; charset=utf-8", body: []byte(`{"field":"from-body"}`), want: "from-body"},
		{name: "mixed-case json content type", contentType: "Application/JSON", body: []byte(`{"field":"from-body"}`), want: "from-body"},
		{name: "unknown content type", contentType: "text/plain", body: []byte("field=from-body"), wantErr: isUnprocessable},
		{name: "no content type", contentType: "", body: []byte("field=from-body"), wantErr: isUnprocessable},
		// The binder decodes these; the helper accepts none of them for an auth
		// input, and an XML decoder keeps what it read before a syntax error.
		{name: "application/xml", contentType: "application/xml", body: []byte(`<target><Field>from-body</Field></target>`), wantErr: isUnprocessable},
		{name: "text/xml", contentType: "text/xml", body: []byte(`<target><Field>from-body</Field></target>`), wantErr: isUnprocessable},
		{name: "mixed-case xml with parameters", contentType: "Application/XML; charset=utf-8", body: []byte(`<target><Field>from-body</Field></target>`), wantErr: isUnprocessable},
		{name: "partial xml", contentType: "application/xml", body: []byte(`<target><Field>from-body</Field><broken>`), wantErr: isUnprocessable},
		{name: "vendor json", contentType: "application/vnd.api+json", body: []byte(`{"field":"from-body"}`), wantErr: isUnprocessable},
		{name: "cbor", contentType: "application/cbor", body: []byte("\xa1efield\x69from-body"), wantErr: isUnprocessable},
		{name: "msgpack", contentType: "application/msgpack", body: []byte("\x81\xa5field\xa9from-body"), wantErr: isUnprocessable},
		{name: "malformed json", contentType: "application/json", body: []byte(`{"field":`), wantErr: func(err error) bool { return err != nil }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			var got target
			var bindErr error
			app.Post("/bind", func(c fiber.Ctx) error {
				bindErr = bindRequestBody(c, &got)
				return c.SendStatus(http.StatusNoContent)
			})

			request := httptest.NewRequest(http.MethodPost, "/bind?"+url.Values{"field": {"from-query"}}.Encode(), bytes.NewReader(tc.body))
			request.Header.Set("Content-Type", tc.contentType)
			if tc.encoding != "" {
				request.Header.Set("Content-Encoding", tc.encoding)
			}
			response, err := app.Test(request, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("request: %v", err)
			}
			_ = response.Body.Close()

			if tc.wantErr != nil {
				if !tc.wantErr(bindErr) {
					t.Fatalf("bind error = %v, want the refusal the case names", bindErr)
				}
				if got.Field != "" {
					t.Fatalf("Field = %q after a refusal, want nothing filled (the query carried %q)", got.Field, "from-query")
				}
				return
			}
			if bindErr != nil {
				t.Fatalf("bind error = %v", bindErr)
			}
			if got.Field != tc.want {
				t.Fatalf("Field = %q, want %q (the query carried %q)", got.Field, tc.want, "from-query")
			}
		})
	}
}
