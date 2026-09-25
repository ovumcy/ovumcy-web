package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/ovumcy/ovumcy-web/internal/httpx"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
)

// limiterImportPath is the package every rate limiter in the composition root
// is built from. The reader below keys on the import, not on the spelling
// `limiter`, so an aliased import is still read.
const limiterImportPath = "github.com/gofiber/fiber/v3/middleware/limiter"

// limiterMount is one limiter.New the composition root wires through a Use
// call: the prefix it is mounted on, how its Next filter scopes it, and the
// function its LimitReached is built by. The limiters are mounted in
// cmd/ovumcy, which internal/api cannot import, so they are read as source, the
// same way requireAPIRateLimitMountHasNoNextFilter reads the /api mount.
type limiterMount struct {
	source string
	prefix string
	// scoped is set when Next is a rateLimitOnlyFor call: the limiter then
	// counts exactly (method, path) — plus HEAD for a GET — and nothing else.
	scoped bool
	method string
	path   string
	// opaque is set when Next is anything else. Such a limiter may or may not
	// reach a given operation under its prefix, and this reader cannot tell.
	opaque bool
	// limitReachedBy names the package-level function whose call builds the
	// LimitReached handler, or is empty when LimitReached is anything else.
	limitReachedBy string
}

// covers reports whether the mount's limiter counts a request for the
// documented operation. The comparison goes through httpx.RoutingNormalizedPath,
// the normalization rateLimitOnlyFor itself applies, so the reader and the
// production predicate agree on what "the same path" means.
func (mount limiterMount) covers(method string, path string) (covered bool, undecidable bool) {
	normalizedPath := httpx.RoutingNormalizedPath(path)
	if mount.prefix != "" && mount.prefix != "/" {
		normalizedPrefix := httpx.RoutingNormalizedPath(mount.prefix)
		if normalizedPath != normalizedPrefix && !strings.HasPrefix(normalizedPath, normalizedPrefix+"/") {
			return false, false
		}
	}
	switch {
	case mount.opaque:
		return false, true
	case mount.scoped:
		methodMatches := method == mount.method || (mount.method == fiber.MethodGet && method == fiber.MethodHead)
		return methodMatches && normalizedPath == httpx.RoutingNormalizedPath(mount.path), false
	default:
		return true, false
	}
}

// parseNonTestGoFiles parses every non-test Go file in dir.
func parseNonTestGoFiles(t *testing.T, fileSet *token.FileSet, dir string) []*ast.File {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, parseErr := parser.ParseFile(fileSet, filepath.Join(dir, name), nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", name, parseErr)
		}
		files = append(files, parsed)
	}
	if len(files) == 0 {
		t.Fatalf("no non-test Go file parsed in %s; discovery broke", dir)
	}
	return files
}

// collectStringConstants records every package-level string constant declared
// with a literal value, keyed by qualifier+name, so a limiter's path written as
// a constant (api.LanguageSwitchPath, api.CalendarFeedRateLimitPrefix) resolves
// to the value the router sees without a lookup table kept here.
func collectStringConstants(files []*ast.File, qualifier string, into map[string]string) {
	for _, file := range files {
		for _, decl := range file.Decls {
			general, ok := decl.(*ast.GenDecl)
			if !ok || general.Tok != token.CONST {
				continue
			}
			for _, spec := range general.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for index, name := range valueSpec.Names {
					if index >= len(valueSpec.Values) {
						continue
					}
					literal, ok := valueSpec.Values[index].(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						continue
					}
					if value, err := strconv.Unquote(literal.Value); err == nil {
						into[qualifier+name.Name] = value
					}
				}
			}
		}
	}
}

func resolveStringExpr(expr ast.Expr, constants map[string]string) (string, bool) {
	switch node := expr.(type) {
	case *ast.BasicLit:
		if node.Kind != token.STRING {
			return "", false
		}
		value, err := strconv.Unquote(node.Value)
		return value, err == nil
	case *ast.Ident:
		value, ok := constants[node.Name]
		return value, ok
	case *ast.SelectorExpr:
		qualifier, ok := node.X.(*ast.Ident)
		if !ok {
			return "", false
		}
		value, ok := constants[qualifier.Name+"."+node.Sel.Name]
		return value, ok
	}
	return "", false
}

// resolveFiberMethod reads a fiber.Method* selector as the verb it names.
func resolveFiberMethod(expr ast.Expr) (string, bool) {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	qualifier, ok := selector.X.(*ast.Ident)
	if !ok || qualifier.Name != "fiber" || !strings.HasPrefix(selector.Sel.Name, "Method") {
		return "", false
	}
	return strings.ToUpper(strings.TrimPrefix(selector.Sel.Name, "Method")), true
}

// limiterPackageName returns the name a file refers to the limiter package by:
// its alias when the import carries one, `limiter` otherwise, and "" when the
// file does not import it. A dot or blank import fails: a limiter.New spelled
// without a qualifier cannot be told apart from any other New.
func limiterPackageName(t *testing.T, fileSet *token.FileSet, file *ast.File) string {
	t.Helper()
	for _, spec := range file.Imports {
		importPath, err := strconv.Unquote(spec.Path.Value)
		if err != nil || importPath != limiterImportPath {
			continue
		}
		if spec.Name == nil {
			return "limiter"
		}
		if spec.Name.Name == "." || spec.Name.Name == "_" {
			t.Fatalf("%s: the limiter package is imported as %q, so this guard cannot find the limiters built from it — import it by name",
				fileSet.Position(spec.Pos()), spec.Name.Name)
		}
		return spec.Name.Name
	}
	return ""
}

func isLimiterNewCall(expr ast.Expr, packageName string) (*ast.CallExpr, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok || packageName == "" {
		return nil, false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil, false
	}
	qualifier, ok := selector.X.(*ast.Ident)
	return call, ok && qualifier.Name == packageName && selector.Sel.Name == "New"
}

// fiberImportPath is the package the root app is built from.
const fiberImportPath = "github.com/gofiber/fiber/v3"

// compositionRootDir is the one directory whose non-test sources may build a
// rate limiter: the reader below reads mounts there and nowhere else.
var compositionRootDir = filepath.Join("..", "..", "cmd", "ovumcy")

// importedPackageName returns the name file refers to importPath by — its
// alias, or fallback when the import carries none — and "" when the file does
// not import it.
func importedPackageName(file *ast.File, importPath string, fallback string) string {
	for _, spec := range file.Imports {
		if path, err := strconv.Unquote(spec.Path.Value); err != nil || path != importPath {
			continue
		}
		if spec.Name != nil {
			return spec.Name.Name
		}
		return fallback
	}
	return ""
}

// compositionRoot is cmd/ovumcy type-checked: its non-test syntax, and the
// objects that provably hold the one root fiber app.
type compositionRoot struct {
	fileSet *token.FileSet
	files   []*ast.File
	info    *types.Info
	// rootApp holds the variable the single app constructor call is assigned
	// to, plus every parameter that receives exactly that value at every call
	// site of its function.
	rootApp  map[types.Object]bool
	rootName string
}

func isFiberAppPointer(typ types.Type) bool {
	pointer, ok := typ.(*types.Pointer)
	if !ok {
		return false
	}
	named, ok := pointer.Elem().(*types.Named)
	return ok && named.Obj().Name() == "App" && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == fiberImportPath
}

// loadCompositionRoot type-checks cmd/ovumcy and resolves its root app by
// declaration, not by name. A limiter's prefix is read as the path it counts
// only when it is mounted on that app: on a Group, or on a second app mounted
// under a prefix, the prefix is relative, and reading it as absolute drops
// every operation under it from the sweep.
//
// The app constructors are not listed here: every call whose result is a
// *fiber.App and whose callee is not a function of cmd/ovumcy itself counts
// (fiber.New, fiber.NewWithCustomCtx, a constructor in another package, a
// function value). There has to be exactly one, assigned to a plain variable
// that is never reassigned or addressed.
func loadCompositionRoot(t *testing.T) compositionRoot {
	t.Helper()
	loaded, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Dir: filepath.Join("..", ".."),
	}, "./cmd/ovumcy")
	if err != nil || len(loaded) != 1 {
		t.Fatalf("type-check cmd/ovumcy: %v (%d packages)", err, len(loaded))
	}
	pkg := loaded[0]
	if len(pkg.Errors) > 0 || len(pkg.Syntax) == 0 {
		t.Fatalf("type-check cmd/ovumcy: %v (%d files)", pkg.Errors, len(pkg.Syntax))
	}
	root := compositionRoot{fileSet: pkg.Fset, files: pkg.Syntax, info: pkg.TypesInfo, rootApp: make(map[types.Object]bool)}
	position := func(node ast.Node) string { return pkg.Fset.Position(node.Pos()).String() }

	// Each call expression's single-identifier assignment target, if any.
	assignedTo := make(map[ast.Expr]*ast.Ident)
	// Each identifier that is the callee of a call, mapped to that call.
	calledAs := make(map[*ast.Ident]*ast.CallExpr)
	var sites []string
	var rootIdent *ast.Ident
	for _, file := range pkg.Syntax {
		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.AssignStmt:
				if len(typed.Lhs) == 1 && len(typed.Rhs) == 1 {
					if ident, ok := typed.Lhs[0].(*ast.Ident); ok {
						assignedTo[typed.Rhs[0]] = ident
					}
				}
			case *ast.ValueSpec:
				if len(typed.Names) == 1 && len(typed.Values) == 1 {
					assignedTo[typed.Values[0]] = typed.Names[0]
				}
			case *ast.CallExpr:
				if ident, ok := typed.Fun.(*ast.Ident); ok {
					calledAs[ident] = typed
				}
				result, ok := pkg.TypesInfo.Types[typed]
				if !ok || !isFiberAppPointer(result.Type) {
					return true
				}
				if callee := typeutil.Callee(pkg.TypesInfo, typed); callee != nil && callee.Pkg() == pkg.Types {
					return true
				}
				sites = append(sites, position(typed))
				rootIdent = assignedTo[typed]
			}
			return true
		})
	}
	if len(sites) != 1 || rootIdent == nil {
		t.Fatalf("cmd/ovumcy must build exactly one fiber app, assigned to a plain variable, for this guard to tell the root app's limiter mounts from a group's or a sub-app's; a *fiber.App is built at:\n  %s",
			strings.Join(sites, "\n  "))
	}
	rootObject := pkg.TypesInfo.ObjectOf(rootIdent)
	if rootObject == nil {
		t.Fatalf("%s: the root app variable resolves to no object", position(rootIdent))
	}
	root.rootApp[rootObject] = true
	root.rootName = rootIdent.Name

	// A parameter holds the root app only when its function is used solely as
	// a direct callee and every call passes a root-app identifier in its slot.
	for changed := true; changed; {
		changed = false
		for _, file := range pkg.Syntax {
			for _, decl := range file.Decls {
				function, ok := decl.(*ast.FuncDecl)
				if !ok || function.Recv != nil {
					continue
				}
				changed = root.adoptRootParameters(function, calledAs) || changed
			}
		}
	}

	// A root-app variable that is reassigned, or whose address is taken, can
	// hold something else by the time a Use runs.
	for _, file := range pkg.Syntax {
		ast.Inspect(file, func(node ast.Node) bool {
			var target ast.Node
			switch typed := node.(type) {
			case *ast.AssignStmt:
				if typed.Tok == token.DEFINE {
					return true
				}
				for _, lhs := range typed.Lhs {
					if ident, ok := lhs.(*ast.Ident); ok && root.rootApp[pkg.TypesInfo.Uses[ident]] {
						target = ident
					}
				}
			case *ast.UnaryExpr:
				if ident, ok := typed.X.(*ast.Ident); ok && typed.Op == token.AND && root.rootApp[pkg.TypesInfo.Uses[ident]] {
					target = ident
				}
			}
			if target != nil {
				t.Fatalf("%s: the root app variable is reassigned or addressed, so this guard cannot tell which app a limiter mounted through it lands on", position(target))
			}
			return true
		})
	}
	return root
}

// adoptRootParameters adds function's parameters that receive the root app at
// every call site, and reports whether it added any.
func (root compositionRoot) adoptRootParameters(function *ast.FuncDecl, calledAs map[*ast.Ident]*ast.CallExpr) bool {
	functionObject := root.info.Defs[function.Name]
	if functionObject == nil {
		return false
	}
	var calls []*ast.CallExpr
	for ident, object := range root.info.Uses {
		if object != functionObject {
			continue
		}
		call, ok := calledAs[ident]
		if !ok {
			return false
		}
		calls = append(calls, call)
	}
	if len(calls) == 0 {
		return false
	}
	added := false
	index := 0
	for _, field := range function.Type.Params.List {
		if len(field.Names) == 0 {
			index++
			continue
		}
		for _, name := range field.Names {
			parameter := root.info.Defs[name]
			if parameter != nil && !root.rootApp[parameter] && root.everyCallPassesRoot(calls, index) {
				root.rootApp[parameter] = true
				added = true
			}
			index++
		}
	}
	return added
}

func (root compositionRoot) everyCallPassesRoot(calls []*ast.CallExpr, index int) bool {
	for _, call := range calls {
		if call.Ellipsis.IsValid() || index >= len(call.Args) {
			return false
		}
		ident, ok := call.Args[index].(*ast.Ident)
		if !ok || !root.rootApp[root.info.Uses[ident]] {
			return false
		}
	}
	return true
}

// requireNoLimiterBuiltOutsideCompositionRoot fails on every non-test Go file
// outside cmd/ovumcy that imports the limiter package. A limiter built there —
// returned by a constructor and mounted as app.Use(pkg.NewX(...)) — is no
// limiter.New call in cmd/ovumcy, so it would land in neither the mounts this
// reader returns nor the list it refuses, and its 429 would never be asked of
// the spec.
//
// The walk skips exactly what the go tool ignores: directories named with a
// leading dot or underscore, and testdata. node_modules is not among them, so
// a Go file under one would build unread; it fails here instead. A symlink or
// other irregular entry (a junction on Windows) is followed when it names a
// file and fails when it names a directory or cannot be resolved, since
// WalkDir would otherwise pass over it in silence.
func requireNoLimiterBuiltOutsideCompositionRoot(t *testing.T) {
	t.Helper()
	root := filepath.Join("..", "..")
	rootCmd := filepath.Clean(compositionRootDir)
	var offenders []string
	walkErr := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if entry.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			target, statErr := os.Stat(path)
			if statErr != nil || target.IsDir() {
				offenders = append(offenders, filepath.ToSlash(path)+" (a link or irregular entry this walk cannot read as a file)")
				return nil
			}
		} else if entry.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		if slashed := filepath.ToSlash(path); strings.Contains(slashed, "/node_modules/") {
			offenders = append(offenders, slashed+" (a Go file under node_modules)")
			return nil
		}
		if strings.HasSuffix(name, "_test.go") || filepath.Dir(path) == rootCmd {
			return nil
		}
		parsed, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return fmt.Errorf("parse %s: %w", path, parseErr)
		}
		if importedPackageName(parsed, limiterImportPath, "limiter") != "" {
			offenders = append(offenders, filepath.ToSlash(path))
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk the module for limiter imports: %v", walkErr)
	}
	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Fatalf("a file outside cmd/ovumcy may build a limiter this guard does not read: it imports the limiter package, or the walk cannot see inside it. Build limiters in cmd/ovumcy as app.Use([prefix,] limiter.New(limiter.Config{...})), and keep Go files out of node_modules and out of linked directories:\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

// discoverLimiterMounts returns every limiter the composition root builds, read
// from cmd/ovumcy's non-test sources, and fails closed: EVERY limiter.New call
// site in the package has to be a direct argument of a Use call on the root
// app, the one shape whose reach this reader can state. A limiter built
// anywhere else — assigned to a variable first, returned by a helper, passed to
// a route registration, mounted on a group, built outside cmd/ovumcy — fails
// here by position instead of being skipped, because a limiter this reader
// drops is a 429 the sweep never asks the spec about.
func discoverLimiterMounts(t *testing.T) []limiterMount {
	t.Helper()
	requireNoLimiterBuiltOutsideCompositionRoot(t)
	root := loadCompositionRoot(t)
	fileSet, cmdFiles := root.fileSet, root.files
	constants := make(map[string]string)
	collectStringConstants(cmdFiles, "", constants)
	collectStringConstants(parseNonTestGoFiles(t, token.NewFileSet(), "."), "api.", constants)

	var mounts []limiterMount
	var unreadable []string
	for _, file := range cmdFiles {
		packageName := limiterPackageName(t, fileSet, file)
		if packageName == "" {
			continue
		}
		mounted := make(map[*ast.CallExpr]bool)
		ast.Inspect(file, func(node ast.Node) bool {
			use, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := use.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Use" {
				return true
			}
			for argIndex, arg := range use.Args {
				newCall, ok := isLimiterNewCall(arg, packageName)
				if !ok {
					continue
				}
				mounted[newCall] = true
				mount := limiterMount{source: fileSet.Position(newCall.Pos()).String()}
				if receiver, isIdent := selector.X.(*ast.Ident); !isIdent || !root.rootApp[root.info.Uses[receiver]] {
					t.Fatalf("%s: the limiter is mounted through a Use whose receiver is not provably the root app %q (the variable its one constructor call is assigned to, or a parameter every caller passes exactly that variable), so its prefix may be relative to a group or sub-app and the operations under it would drop out of the sweep — mount it on the root app with the full prefix",
						mount.source, root.rootName)
				}
				if argIndex > 0 {
					prefix, resolved := resolveStringExpr(use.Args[0], constants)
					if !resolved {
						t.Fatalf("%s: the limiter's mount prefix is not a string literal or a package-level string constant; this guard cannot tell which operations it reaches", mount.source)
					}
					mount.prefix = prefix
				}
				mount.readConfig(t, newCall, constants)
				mounts = append(mounts, mount)
			}
			return true
		})
		ast.Inspect(file, func(node ast.Node) bool {
			if call, ok := isLimiterNewCall(asExpr(node), packageName); ok && !mounted[call] {
				unreadable = append(unreadable, fileSet.Position(call.Pos()).String())
			}
			return true
		})
	}
	if len(unreadable) > 0 {
		sort.Strings(unreadable)
		t.Fatalf("limiter.New is called outside a direct app.Use([prefix,] limiter.New(limiter.Config{...})) argument, so this guard cannot tell which operations the limiter counts — mount it in that shape:\n  %s",
			strings.Join(unreadable, "\n  "))
	}
	return mounts
}

func asExpr(node ast.Node) ast.Expr {
	expr, _ := node.(ast.Expr)
	return expr
}

// readConfig reads the two fields of the limiter.Config literal the guards
// depend on: Next (the limiter's scope) and LimitReached (what it answers).
func (mount *limiterMount) readConfig(t *testing.T, newCall *ast.CallExpr, constants map[string]string) {
	t.Helper()
	if len(newCall.Args) == 0 {
		return
	}
	config, ok := newCall.Args[0].(*ast.CompositeLit)
	if !ok {
		t.Fatalf("%s: limiter.New's argument is not a limiter.Config literal; this guard cannot read its Next filter", mount.source)
	}
	for _, element := range config.Elts {
		field, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := field.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "Next":
			mount.readNextFilter(t, field.Value, constants)
		case "LimitReached":
			mount.limitReachedBy = packageFunctionCallee(field.Value)
		}
	}
}

func (mount *limiterMount) readNextFilter(t *testing.T, value ast.Expr, constants map[string]string) {
	t.Helper()
	scope, ok := value.(*ast.CallExpr)
	if !ok || packageFunctionCallee(scope) != "rateLimitOnlyFor" || len(scope.Args) != 2 {
		mount.opaque = true
		return
	}
	method, methodOK := resolveFiberMethod(scope.Args[0])
	path, pathOK := resolveStringExpr(scope.Args[1], constants)
	if !methodOK || !pathOK {
		t.Fatalf("%s: rateLimitOnlyFor's arguments are not a fiber.Method* selector and a string literal or constant; this guard cannot read the limiter's scope", mount.source)
	}
	mount.scoped, mount.method, mount.path = true, method, path
}

// packageFunctionCallee names the unqualified function expr calls, or "".
func packageFunctionCallee(expr ast.Expr) string {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return ""
	}
	ident, ok := call.Fun.(*ast.Ident)
	if !ok {
		return ""
	}
	return ident.Name
}

// limiterCoveredDocumentedOperationsOutsideV1 returns the operations
// docs/openapi.yaml documents outside /api/v1 that a mounted limiter counts,
// each mapped to the mount that covers it. /api/v1 is left to the route-table
// sweep in TestOpenAPIDeclaresRateLimitedOnEveryLimiterCoveredOperation; every
// other documented operation is decided here, from the mounts themselves, so a
// limiter wired later onto a documented path is swept with no edit to this file.
func limiterCoveredDocumentedOperationsOutsideV1(t *testing.T, declared map[int][]string) map[string]string {
	t.Helper()
	mounts := discoverLimiterMounts(t)
	if len(mounts) == 0 {
		t.Fatal("no limiter.New mount discovered in cmd/ovumcy; the reader, not the wiring, is broken")
	}

	documented := make(map[string]struct{})
	for _, operations := range declared {
		for _, operation := range operations {
			documented[operation] = struct{}{}
		}
	}

	covered := make(map[string]string)
	var undecidable []string
	for operation := range documented {
		method, path, ok := strings.Cut(operation, " ")
		if !ok || strings.HasPrefix(path, "/api/v1") {
			continue
		}
		for _, mount := range mounts {
			isCovered, cannotTell := mount.covers(method, path)
			if cannotTell {
				undecidable = append(undecidable, fmt.Sprintf("%s (limiter at %s)", operation, mount.source))
				continue
			}
			if isCovered {
				covered[operation] = mount.source
			}
		}
	}
	if len(undecidable) > 0 {
		sort.Strings(undecidable)
		t.Fatalf("a limiter whose Next filter is not a rateLimitOnlyFor call is mounted on a prefix that reaches a documented operation, so this guard cannot say whether its 429 is real there — scope the limiter with rateLimitOnlyFor:\n  %s",
			strings.Join(undecidable, "\n  "))
	}
	return covered
}

// requireLanguageSwitchLimiterAnswersThroughRespondAPIRateLimited ties the
// limiter the behavioural test builds to the one the composition root mounts:
// every limiter covering POST /lang must build its LimitReached with a function
// of cmd/ovumcy that returns handler.RespondAPIRateLimited — the responder the
// test's own limiter answers with. Swapping that constructor on the real mount
// reddens here, rather than leaving the test proving a responder production no
// longer uses.
func requireLanguageSwitchLimiterAnswersThroughRespondAPIRateLimited(t *testing.T) {
	t.Helper()
	fileSet := token.NewFileSet()
	cmdFiles := parseNonTestGoFiles(t, fileSet, filepath.Join("..", "..", "cmd", "ovumcy"))
	returnsRespondAPIRateLimited := make(map[string]bool)
	for _, file := range cmdFiles {
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || function.Recv != nil || function.Body == nil {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				ret, ok := node.(*ast.ReturnStmt)
				if !ok {
					return true
				}
				for _, result := range ret.Results {
					call, ok := result.(*ast.CallExpr)
					if !ok {
						continue
					}
					if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "RespondAPIRateLimited" {
						returnsRespondAPIRateLimited[function.Name.Name] = true
					}
				}
				return true
			})
		}
	}

	var covering int
	for _, mount := range discoverLimiterMounts(t) {
		if covered, _ := mount.covers(fiber.MethodPost, LanguageSwitchPath); !covered {
			continue
		}
		covering++
		if !returnsRespondAPIRateLimited[mount.limitReachedBy] {
			t.Errorf("%s: the limiter covering POST %s builds its LimitReached with %q, which does not return handler.RespondAPIRateLimited — the responder the spec's 429 for this route is pinned against. Wire it through newAPIRateLimitHandler, or re-pin the spec against the new responder",
				mount.source, LanguageSwitchPath, mount.limitReachedBy)
		}
	}
	if covering == 0 {
		t.Fatalf("no limiter mounted in cmd/ovumcy covers POST %s; the 429 this test pins has no producer", LanguageSwitchPath)
	}
}

// openAPIYAMLBlock returns the lines nested under the key path given, one key
// per level at two-space steps from column 0 (for example "paths", "/lang",
// "post", "responses", "'400'"). It fails when a level is missing, so a caller
// asking for a response the spec does not declare reddens by name.
func openAPIYAMLBlock(t *testing.T, spec string, keys ...string) []string {
	t.Helper()
	level := 0
	var block []string
	for _, raw := range strings.Split(spec, "\n") {
		line := strings.TrimRight(raw, "\r")
		text := strings.TrimSpace(line)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if level == len(keys) {
			if indent <= 2*(level-1) {
				return block
			}
			block = append(block, text)
			continue
		}
		if indent < 2*level {
			break
		}
		if indent == 2*level && text == keys[level]+":" {
			level++
		}
	}
	if level == len(keys) {
		return block
	}
	t.Fatalf("docs/openapi.yaml declares no %s", strings.Join(keys, " → "))
	return nil
}

func requireSpecLine(t *testing.T, block []string, want string, where string) {
	t.Helper()
	for _, line := range block {
		if line == want {
			return
		}
	}
	t.Errorf("%s: docs/openapi.yaml does not carry %q — the spec no longer describes what the server answers there:\n  %s",
		where, want, strings.Join(block, "\n  "))
}

func requireSpecLinePrefix(t *testing.T, block []string, prefix string, where string) {
	t.Helper()
	for _, line := range block {
		if strings.HasPrefix(line, prefix) {
			return
		}
	}
	t.Errorf("%s: docs/openapi.yaml carries no line starting %q — the spec no longer describes what the server answers there:\n  %s",
		where, prefix, strings.Join(block, "\n  "))
}

// requireSpecMentions requires a phrase in a response's text, joined across
// lines so a reflowed description still matches.
func requireSpecMentions(t *testing.T, block []string, phrase string, where string) {
	t.Helper()
	if strings.Contains(strings.Join(block, " "), phrase) {
		return
	}
	t.Errorf("%s: docs/openapi.yaml never mentions %q — the server answers that way and the description has to say so:\n  %s",
		where, phrase, strings.Join(block, "\n  "))
}

// languageSwitchClient is one way a caller reaches POST /lang; the route answers
// each a different carrier, so each is driven separately.
type languageSwitchClient struct {
	name    string
	headers map[string]string
	// body replaces the form-encoded blank `lang` when set, for a caller whose
	// Content-Type is not a form.
	body string
}

type languageSwitchAnswer struct {
	status      int
	contentType string
	retryAfter  string
	body        []byte
}

// TestOpenAPILanguageSwitchDeclaresTheRefusalsItAnswers pins the two refusals
// POST /lang answers against what docs/openapi.yaml declares for that
// operation. The spec published only the 200 and the 303, while the handler
// refuses a blank `lang` with 400 and the route carries a per-IP limiter of its
// own that refuses with 429.
//
// Both answers are produced, not restated: the 400 by SetLanguage's own blank
// check through RespondLanguageSwitchTransportError, the page-form-aware
// responder every other refusal on this route answers with
// (TestLanguageSwitchRejectionAnswersThroughTheEnvelope pins that wiring on the
// real stack), the 429 by a real fiber limiter answering with
// RespondAPIRateLimited — the responder the real /lang mount reaches, which
// requireLanguageSwitchLimiterAnswersThroughRespondAPIRateLimited reads out of
// cmd/ovumcy. The JSON caller's key, category and target are required verbatim
// in the spec's example; the form and HTMX callers, which get an HTML fragment
// instead, are required to be named as such in the response's description.
func TestOpenAPILanguageSwitchDeclaresTheRefusalsItAnswers(t *testing.T) {
	requireLanguageSwitchLimiterAnswersThroughRespondAPIRateLimited(t)

	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read openapi spec: %v", err)
	}
	spec := string(data)
	badRequest := openAPIYAMLBlock(t, spec, "paths", LanguageSwitchPath, "post", "responses", "'400'")
	rateLimited := openAPIYAMLBlock(t, spec, "paths", LanguageSwitchPath, "post", "responses", "'429'")
	component := openAPIYAMLBlock(t, spec, "components", "responses", "RateLimited")

	// A handler built by NewHandler, i18n included: the fragment arms resolve
	// the request's catalogue before they render.
	handler, _ := newEgressLedgerHandler(t, false)
	send := func(app *fiber.App, client languageSwitchClient) languageSwitchAnswer {
		payload := url.Values{"lang": {"  "}}.Encode()
		if client.body != "" {
			payload = client.body
		}
		request := httptest.NewRequest(http.MethodPost, LanguageSwitchPath, strings.NewReader(payload))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for name, value := range client.headers {
			request.Header.Set(name, value)
		}
		response, err := app.Test(request)
		if err != nil {
			t.Fatalf("%s: POST %s: %v", client.name, LanguageSwitchPath, err)
		}
		defer func() { _ = response.Body.Close() }()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatalf("%s: read body: %v", client.name, err)
		}
		return languageSwitchAnswer{
			status:      response.StatusCode,
			contentType: response.Header.Get(fiber.HeaderContentType),
			retryAfter:  response.Header.Get(fiber.HeaderRetryAfter),
			body:        body,
		}
	}
	// A fresh app per client: the limiter's budget of one is spent by that
	// client's own 400, so its second request is the 429.
	newApp := func() *fiber.App {
		app := fiber.New(fiber.Config{ErrorHandler: func(c fiber.Ctx, err error) error {
			var fiberErr *fiber.Error
			if errors.As(err, &fiberErr) {
				return RespondTransportError(c, fiberErr.Code)
			}
			return RespondTransportError(c, fiber.StatusInternalServerError)
		}})
		app.Use(limiter.New(limiter.Config{
			Max:          1,
			Expiration:   time.Minute,
			LimitReached: handler.RespondAPIRateLimited,
		}))
		app.Post(LanguageSwitchPath, handler.SetLanguage)
		return app
	}
	envelopeLines := func(where string, answer languageSwitchAnswer) ([]string, map[string]any) {
		if !strings.HasPrefix(answer.contentType, fiber.MIMEApplicationJSON) {
			t.Fatalf("%s: answered %d as %q, not application/json", where, answer.status, answer.contentType)
		}
		var body map[string]any
		if err := json.Unmarshal(answer.body, &body); err != nil {
			t.Fatalf("%s: answered %d with a body that is not JSON: %v", where, answer.status, err)
		}
		detail, _ := body["error_detail"].(map[string]any)
		key, _ := detail["key"].(string)
		category, _ := detail["category"].(string)
		target, _ := detail["target"].(string)
		if key == "" || category == "" || target == "" || body["error"] != key {
			t.Fatalf("%s: answered %d without the shared error envelope: %v", where, answer.status, body)
		}
		return []string{
			fmt.Sprintf("error: %q", key),
			fmt.Sprintf("error_detail: { key: %q, category: %q, target: %q }", key, category, target),
		}, body
	}
	requireHTMLFragment := func(where string, answer languageSwitchAnswer) {
		if !strings.HasPrefix(answer.contentType, fiber.MIMETextHTML) || json.Valid(answer.body) {
			t.Errorf("%s: answered %d as %q (%q), want the text/html status fragment the spec describes",
				where, answer.status, answer.contentType, answer.body)
		}
	}

	jsonClient := languageSwitchClient{name: "JSON caller", headers: map[string]string{"Accept": fiber.MIMEApplicationJSON}}
	formClient := languageSwitchClient{name: "form submission"}
	jsonBodyClient := languageSwitchClient{name: "JSON body", headers: map[string]string{"Content-Type": fiber.MIMEApplicationJSON}, body: `{"lang":"  "}`}
	htmxClient := languageSwitchClient{name: "HTMX request", headers: map[string]string{"HX-Request": "true", "Accept": fiber.MIMEApplicationJSON}}

	// JSON caller: the envelope, as the example declares it, on both statuses.
	app := newApp()
	answer := send(app, jsonClient)
	if answer.status != http.StatusBadRequest {
		t.Fatalf("JSON caller: a blank lang answered %d, want 400", answer.status)
	}
	requireSpecLine(t, badRequest, "schema: { $ref: '#/components/schemas/ApiError' }", "POST /lang 400")
	lines, _ := envelopeLines("JSON caller 400", answer)
	for _, line := range lines {
		requireSpecLine(t, badRequest, line, "POST /lang 400")
	}
	answer = send(app, jsonClient)
	if answer.status != http.StatusTooManyRequests {
		t.Fatalf("JSON caller: the request past the limiter's budget answered %d, want 429", answer.status)
	}
	if answer.retryAfter == "" {
		t.Error("JSON caller: the 429 carries no Retry-After header")
	}
	lines, body := envelopeLines("JSON caller 429", answer)
	if seconds, ok := body["retry_after_seconds"].(float64); !ok || seconds < 1 {
		t.Errorf("JSON caller: the 429 body carries no retry_after_seconds: %v", body)
	}
	requireSpecLine(t, rateLimited, "$ref: '#/components/responses/RateLimited'", "POST /lang 429")
	requireSpecLine(t, component, "Retry-After:", "components.responses.RateLimited")
	requireSpecLinePrefix(t, component, "retry_after_seconds:", "components.responses.RateLimited")
	requireSpecLine(t, component, "schema: { $ref: '#/components/schemas/ApiError' }", "components.responses.RateLimited")
	for _, line := range lines {
		requireSpecLine(t, component, line, "components.responses.RateLimited")
	}

	// Form submission: both the 400 and the 429 are the fragment — a plain
	// HTML navigation, this route's primary client — and both descriptions
	// have to name it.
	app = newApp()
	answer = send(app, formClient)
	if answer.status != http.StatusBadRequest {
		t.Fatalf("form submission: a blank lang answered %d, want 400", answer.status)
	}
	requireHTMLFragment("form submission 400", answer)
	requireSpecMentions(t, badRequest, "plain form submission", "POST /lang 400")
	answer = send(app, formClient)
	if answer.status != http.StatusTooManyRequests || answer.retryAfter == "" {
		t.Fatalf("form submission: over the budget answered %d with Retry-After %q, want 429 with the header", answer.status, answer.retryAfter)
	}
	requireHTMLFragment("form submission 429", answer)
	requireSpecMentions(t, rateLimited, "plain form submission", "POST /lang 429")

	// A JSON body with no Accept header: httpx.AcceptsJSON reads the
	// Content-Type as a request for JSON too, so this caller gets the envelope
	// on both statuses — and the 429's description has to name that signal, not
	// only the Accept header.
	app = newApp()
	answer = send(app, jsonBodyClient)
	if answer.status != http.StatusBadRequest {
		t.Fatalf("JSON body: a blank lang answered %d, want 400", answer.status)
	}
	envelopeLines("JSON body 400", answer)
	answer = send(app, jsonBodyClient)
	if answer.status != http.StatusTooManyRequests || answer.retryAfter == "" {
		t.Fatalf("JSON body: over the budget answered %d with Retry-After %q, want 429 with the header", answer.status, answer.retryAfter)
	}
	_, body = envelopeLines("JSON body 429", answer)
	if seconds, ok := body["retry_after_seconds"].(float64); !ok || seconds < 1 {
		t.Errorf("JSON body: the 429 body carries no retry_after_seconds: %v", body)
	}
	requireSpecMentions(t, rateLimited, "`Content-Type: application/json`", "POST /lang 429")

	// HTMX request, even one that also accepts JSON: the fragment on both.
	app = newApp()
	answer = send(app, htmxClient)
	if answer.status != http.StatusBadRequest {
		t.Fatalf("HTMX request: a blank lang answered %d, want 400", answer.status)
	}
	requireHTMLFragment("HTMX request 400", answer)
	answer = send(app, htmxClient)
	if answer.status != http.StatusTooManyRequests || answer.retryAfter == "" {
		t.Fatalf("HTMX request: over the budget answered %d with Retry-After %q, want 429 with the header", answer.status, answer.retryAfter)
	}
	requireHTMLFragment("HTMX request 429", answer)
	for _, block := range []struct {
		where string
		lines []string
	}{{"POST /lang 400", badRequest}, {"POST /lang 429", rateLimited}} {
		requireSpecMentions(t, block.lines, "`HX-Request: true`", block.where)
		requireSpecMentions(t, block.lines, "`text/html`", block.where)
	}
}
