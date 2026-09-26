package httpx

import (
	"fmt"
	"go/ast"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// SEC-L8 claims exactly one non-test conversion to html/template.HTML exists
// in the module: the one inside trustedEscapedHTML in this file
// (markup.go), #nosec G203, which the doc comment on that function argues is
// safe because its input is always already escaped. Every other call site
// that hands the template engine a value typed html/template.HTML bypasses
// html/template's contextual auto-escaping for whatever string it converts,
// so the invariant SEC-L8 asks for — "no template.HTML over an assembled
// string outside the builder/choke point" — is worth exactly nothing without
// a guard that resolves conversions BY DECLARATION rather than by grepping
// for the spelling "template.HTML(".
//
// Scope: html/template.HTML only. SEC-L8's own text ("`template.HTML` over
// assembled strings ... use a typed builder, or prove the invariant in one
// place") and its acceptance criterion ("no template.HTML over an assembled
// string outside the builder/choke point") name only that one type; the
// sibling typed strings (template.HTMLAttr, template.JS, template.JSStr,
// template.CSS, template.URL, template.Srcset) are not part of this claim,
// and docs/SECURITY_INVARIANTS.md's mention of template.JS(...) is already
// the subject of a different regression (TestTemplateToJSONEscapesAttributeContext).
// A future SEC item that widens the claim widens this guard's target set.
//
// What this guard does NOT see: a conversion reached through an
// interface{}/any value type-asserted back to template.HTML, through
// unsafe.Pointer, through reflection (reflect.ValueOf(...).Convert(...)), or
// emitted by code generation the loader does not walk as Go syntax (e.g. a
// //go:generate step whose output is committed but whose generator lives
// outside the loaded packages), or through a generic function's own type
// parameter (`func conv[T ~string](s string) T { return T(s) }` instantiated
// with html/template.HTML): go/types attaches the *types.TypeParam to that
// conversion, never the instantiated *types.Named, so it resolves to nothing
// this sweep looks for. A true type ALIAS to template.HTML
// (`type X = template.HTML`) is caught: go/types.Unalias resolves it to the
// same *types.Named the escaper checks for; a DEFINED type merely built on
// top of template.HTML's underlying type is not the same named type and
// does not bypass html/template's escaper either, so it is correctly out of
// scope rather than missed.
func TestTemplateHTMLConversionIsConfinedToTheMarkupChokePoint(t *testing.T) {
	sites := loadTemplateHTMLConversionSites(t)

	// Anti-vacuity floor: a sweep that resolved nothing would report a clean
	// tree it never looked at.
	if len(sites) == 0 {
		t.Fatalf("the sweep resolved zero conversions to html/template.HTML anywhere in the module; " +
			"it is loading the wrong tree or resolving nothing, not reporting a clean one")
	}

	var chokePoint *conversionSite
	var stray []conversionSite
	for index := range sites {
		site := sites[index]
		if site.isTheDeclaredChokePoint() {
			if chokePoint != nil {
				t.Fatalf("more than one conversion resolved inside the declared choke point (%s and %s); "+
					"the anti-vacuity check below can no longer tell 'found by name' from 'found by accident'",
					chokePoint.String(), site.String())
			}
			found := site
			chokePoint = &found
			continue
		}
		stray = append(stray, site)
	}

	// Anti-vacuity: the load-bearing site is asserted BY NAME, never by a
	// population count. A guard whose allowed-site lookup emptied out (a
	// rename, a move, a typo in the constants below) would otherwise let
	// every other site in the module go unnoticed by matching nothing at all.
	if chokePoint == nil {
		t.Fatalf("the sweep never resolved a conversion to html/template.HTML inside %s.%s (%s); "+
			"either the choke point moved, or this guard's allowed-site lookup is broken and would wave through anything",
			chokePointPackage, chokePointFunction, chokePointFile)
	}

	if len(stray) == 0 {
		return
	}

	sort.Slice(stray, func(i, j int) bool { return stray[i].String() < stray[j].String() })
	var described []string
	for _, site := range stray {
		described = append(described, "  "+site.String())
	}
	t.Fatalf("%d conversion(s) to html/template.HTML outside the markup choke point (%s.%s):\n%s\n"+
		"Each one bypasses html/template's contextual auto-escaping for whatever string it converts. "+
		"Route the value through httpx.trustedEscapedHTML (or its exported wrappers) instead, or move the "+
		"choke point and update chokePointPackage/chokePointFunction/chokePointFile in this test.",
		len(stray), chokePointPackage+"."+chokePointFunction, chokePointFile, strings.Join(described, "\n"))
}

// chokePointPackage, chokePointFunction and chokePointFile name the one site
// SEC-L8 allows, declared rather than inferred so a rename of the function
// or a move of the file is a loud diff here too.
const (
	chokePointPackage  = "github.com/ovumcy/ovumcy-web/internal/httpx"
	chokePointFunction = "trustedEscapedHTML"
	chokePointFile     = "markup.go"
)

// conversionSite is one resolved conversion of some expression to
// html/template.HTML, together with the top-level function it is lexically
// inside (empty for a package-level declaration, which is never the choke
// point).
type conversionSite struct {
	pkgPath       string
	file          string
	line          int
	enclosingFunc string
	isMethod      bool
}

func (s conversionSite) isTheDeclaredChokePoint() bool {
	return !s.isMethod &&
		s.pkgPath == chokePointPackage &&
		s.enclosingFunc == chokePointFunction &&
		filepath.Base(s.file) == chokePointFile
}

func (s conversionSite) String() string {
	fn := s.enclosingFunc
	if fn == "" {
		fn = "(package scope)"
	}
	return fmt.Sprintf("%s:%d (%s, in %s)", s.file, s.line, s.pkgPath, fn)
}

// loadTemplateHTMLConversionSites type-checks the module's shipped, non-test
// packages and returns every call expression that go/types resolves to a
// conversion whose target type IS html/template.HTML — resolved by the
// *types.Named object the checker attaches to the call's function
// expression, never by matching the source text "template.HTML(". That is
// what makes an import alias (`ht "html/template"`, `ht.HTML(x)`) or a true
// type alias (`type X = template.HTML`) resolve to the same finding as the
// literal spelling: the identity being tested is the declaration, not the
// characters naming it.
func loadTemplateHTMLConversionSites(t *testing.T) []conversionSite {
	t.Helper()

	root, err := moduleRootForMarkupGuard()
	if err != nil {
		t.Fatalf("locating the module root: %v", err)
	}

	config := &packages.Config{
		// NeedDeps is deliberately omitted: only the root packages
		// (./cmd/..., ./internal/..., etc.) are ever walked for call sites,
		// so type-checking their whole dependency graph too would just
		// spend time on packages this sweep never inspects.
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedImports,
		Dir: root,
		// Tests excluded on purpose: SEC-L8 claims a NON-TEST conversion
		// count, and a fixture built for this very test would otherwise
		// count itself.
		Tests: false,
	}
	// The same package trees the repository's other Go steps scope
	// themselves to; ./... would also sweep a checkout's node_modules.
	loaded, err := packages.Load(config, "./cmd/...", "./internal/...", "./migrations/...", "./scripts/...", "./web/...")
	if err != nil {
		t.Fatalf("type-checking the shipped packages: %v", err)
	}

	var loadErrors []string
	for _, pkg := range loaded {
		for _, packageError := range pkg.Errors {
			loadErrors = append(loadErrors, pkg.PkgPath+": "+packageError.Error())
		}
	}
	if len(loadErrors) > 0 {
		t.Fatalf("the tree does not type-check, so no conversion could be identified:\n  %s", strings.Join(loadErrors, "\n  "))
	}

	var sites []conversionSite
	for _, pkg := range loaded {
		for _, file := range pkg.Syntax {
			relFile := relativeMarkupGuardPath(root, pkg.Fset.Position(file.Pos()).Filename)
			for _, decl := range file.Decls {
				enclosingFunc := ""
				isMethod := false
				var scan ast.Node = decl
				if fn, ok := decl.(*ast.FuncDecl); ok {
					enclosingFunc = fn.Name.Name
					isMethod = fn.Recv != nil
					scan = fn
				}
				ast.Inspect(scan, func(node ast.Node) bool {
					call, ok := node.(*ast.CallExpr)
					if !ok {
						return true
					}
					tv, ok := pkg.TypesInfo.Types[call.Fun]
					if !ok || !tv.IsType() {
						return true
					}
					// types.Unalias sees through a true type alias (`type X
					// = template.HTML`) to the same *types.Named the
					// escaper checks for; with go 1.24+'s gotypesalias
					// default the checker otherwise hands back a
					// *types.Alias here and the site goes unseen.
					named, ok := types.Unalias(tv.Type).(*types.Named)
					if !ok {
						return true
					}
					obj := named.Obj()
					if obj == nil || obj.Pkg() == nil {
						return true
					}
					if obj.Pkg().Path() != "html/template" || obj.Name() != "HTML" {
						return true
					}
					sites = append(sites, conversionSite{
						pkgPath:       pkg.PkgPath,
						file:          relFile,
						line:          pkg.Fset.Position(call.Pos()).Line,
						enclosingFunc: enclosingFunc,
						isMethod:      isMethod,
					})
					return true
				})
			}
		}
	}
	return sites
}

func relativeMarkupGuardPath(root string, absolute string) string {
	relative, err := filepath.Rel(root, absolute)
	if err != nil {
		return absolute
	}
	return filepath.ToSlash(relative)
}

// moduleRootForMarkupGuard walks up from the package directory to the module
// root, the same way declaration_reachability_barrier_test.go does for its
// own sweep; duplicated locally because that helper is unexported in
// another package.
func moduleRootForMarkupGuard() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolving the working directory: %w", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above %s; the sweep would measure nothing", dir)
		}
		dir = parent
	}
}
