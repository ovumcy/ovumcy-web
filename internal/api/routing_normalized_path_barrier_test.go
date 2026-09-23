package api

import (
	"fmt"
	"go/ast"
	"go/types"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// This file holds the routing-normalized path barrier. Fiber picks a route
// against a case-folded, trailing-slash-stripped copy of the path but hands
// handlers the untouched wire path, so any branch keyed on the raw path sends
// /API/v1/sessions, /LANG or /lang/ down a different answer than the lowercase
// spelling that reaches the very same handler. The barrier therefore requires
// every raw path read in a module package that imports fiber — today the
// transport layer (internal/api) and the composition root (cmd/ovumcy) — to
// pass straight into the routing normalization, or to sit in a function
// declared below as reading the raw bytes on purpose.
//
// It lives in this package because the declaration barrier beside it already
// type-checks the whole module once per test binary (loadTreeEvidence), and it
// reuses that load rather than paying for a second one.
//
// Everything is resolved by object through go/types, never by identifier
// text: the reader is fiber's own Path method (on the Ctx interface and on the
// concrete context alike), the sinks are the httpx functions themselves, and
// the enclosing function is its declaration's *types.Func. A text match would
// miss an aliased context, a renamed import or a method value, and would
// accept a local helper that merely shares a sink's name.

// rawPathReadersByDesign names, by the enclosing function's full name, each
// function that reads the raw request path on purpose, with the exact number
// of raw reads it holds and the reason. The count is what keeps an entry from
// exempting its whole function: a second raw read added beside the declared
// one fails the barrier until someone re-justifies it, and so does an entry
// whose read is gone.
var rawPathReadersByDesign = map[string]struct {
	reads  int
	reason string
}{
	"github.com/ovumcy/ovumcy-web/cmd/ovumcy.csrfMiddlewareConfig":      {1, "the OIDC callback's CSRF exemption matches the raw bytes on purpose: a case or slash variant gets no exemption, which is stricter than the route"},
	"github.com/ovumcy/ovumcy-web/cmd/ovumcy.securityHeadersMiddleware": {1, "the /static cache exemption matches the raw bytes on purpose: a variant spelling keeps no-store, which is the stricter answer"},
	"github.com/ovumcy/ovumcy-web/internal/api.SafeRequestLogPath":      {1, "writes the path into the request log line; nothing branches on it"},
	"github.com/ovumcy/ovumcy-web/internal/api.currentPathWithQuery":    {1, "echoes the address into the rendered layout; nothing branches on it"},
}

// routingNormalizedDecisionSites are the functions whose answer used to fork
// on the raw path. Each must still read the path through the normalization, so
// a rewrite that drops the read altogether cannot pass as clean.
var routingNormalizedDecisionSites = []string{
	"(*github.com/ovumcy/ovumcy-web/internal/api.Handler).AuthRequired",
	"(*github.com/ovumcy/ovumcy-web/internal/api.Handler).RespondAPIRateLimited",
	"(*github.com/ovumcy/ovumcy-web/internal/api.Handler).respondAuthError",
	"(*github.com/ovumcy/ovumcy-web/internal/api.Handler).respondSettingsError",
	"(*github.com/ovumcy/ovumcy-web/internal/api.Handler).NotFound",
	"github.com/ovumcy/ovumcy-web/cmd/ovumcy.rateLimitScope",
}

const (
	rawPathModulePath = "github.com/ovumcy/ovumcy-web"
	rawPathFiberPath  = "github.com/gofiber/fiber/v3"
	rawPathFastHTTP   = "github.com/valyala/fasthttp"
)

// rawPathRequiredPackages must be in the swept set by name: they hold every
// decision site, so a sweep that lost either would pass about nothing.
var rawPathRequiredPackages = []string{
	rawPathModulePath + "/internal/api",
	rawPathModulePath + "/cmd/ovumcy",
}

// rawPathSweptPackages derives the swept set from the loaded tree rather than
// listing it: every module package whose non-test files import fiber, with the
// tooling under scripts/ left out. A fixed list would let a new package that
// reads the fiber path escape the barrier. The tree is loaded without tests,
// so a package holding only test files imports nothing here and drops out.
func rawPathSweptPackages(evidence *treeEvidence) []*packages.Package {
	var swept []*packages.Package
	for _, pkg := range evidence.packages {
		if !strings.HasPrefix(pkg.PkgPath, rawPathModulePath+"/") || strings.HasPrefix(pkg.PkgPath, rawPathModulePath+"/scripts/") {
			continue
		}
		if _, importsFiber := pkg.Imports[rawPathFiberPath]; importsFiber {
			swept = append(swept, pkg)
		}
	}
	sort.Slice(swept, func(i, j int) bool { return swept[i].PkgPath < swept[j].PkgPath })
	return swept
}

type rawPathVerdict int

const (
	rawPathOffender rawPathVerdict = iota
	// rawPathNormalized: the read is the direct path argument of
	// httpx.RoutingNormalizedPath or httpx.HasRoutingPrefix.
	rawPathNormalized
	// rawPathCalendarFeed: the read is the direct path argument of
	// IsCalendarFeedRequest, which normalizes inside. That it agrees with the
	// router on every spelling is held by
	// TestIsCalendarFeedRequestMatchesWhatFiberActuallyDispatches in
	// cmd/ovumcy, not here.
	rawPathCalendarFeed
	rawPathAllowListed
)

type rawPathUse struct {
	location  string
	enclosing string
	reader    string
	verdict   rawPathVerdict
}

// rawPathSink is a function that takes a raw path and compares it in the
// router's own normalization; pathArgument is the index of that parameter.
type rawPathSink struct {
	pathArgument int
	verdict      rawPathVerdict
}

// TestEveryRawRequestPathReadIsRoutingNormalized is the barrier.
func TestEveryRawRequestPathReadIsRoutingNormalized(t *testing.T) {
	evidence := loadTreeEvidence(t)
	sinks := rawPathSinks(t, evidence)

	var uses []rawPathUse
	swept := map[string]bool{}
	var sweptPaths []string
	for _, pkg := range rawPathSweptPackages(evidence) {
		swept[pkg.PkgPath] = true
		sweptPaths = append(sweptPaths, pkg.PkgPath)
		uses = append(uses, collectRawPathUses(pkg, sinks)...)
	}
	for _, required := range rawPathRequiredPackages {
		if !swept[required] {
			t.Fatalf("the sweep did not select %s, which holds the decision sites; the package filter is wrong, so none of its path reads were judged", required)
		}
	}
	t.Logf("swept %d package(s) importing fiber: %s", len(sweptPaths), strings.Join(sweptPaths, ", "))

	var offenders []string
	for _, use := range uses {
		if use.verdict == rawPathOffender {
			offenders = append(offenders, fmt.Sprintf("%s in %s reads %s raw", use.location, describeEnclosing(use.enclosing), use.reader))
		}
	}
	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Fatalf("%d raw request-path read(s) decide behaviour on bytes the router does not compare:\n  %s\n"+
			"Pass the read straight into httpx.RoutingNormalizedPath (or httpx.HasRoutingPrefix) and branch on that. "+
			"Only when the raw bytes are deliberately STRICTER than the route, or feed no decision at all, declare the enclosing function in rawPathReadersByDesign with that reason.",
			len(offenders), strings.Join(offenders, "\n  "))
	}

	for _, site := range routingNormalizedDecisionSites {
		if countRawPathUses(uses, site, rawPathNormalized) == 0 {
			t.Errorf("%s no longer reads the request path through httpx.RoutingNormalizedPath; it is one of the decision sites this barrier exists for, so a rewrite must keep its comparison on the normalized path", site)
		}
	}
	for name, declared := range rawPathReadersByDesign {
		switch got := countRawPathUses(uses, name, rawPathAllowListed); {
		case got == 0:
			t.Errorf("rawPathReadersByDesign declares %s, which no longer reads the raw request path; the entry is stale and would exempt whatever raw read lands there next — remove it", name)
		case got != declared.reads:
			t.Errorf("%s holds %d raw request-path read(s), but rawPathReadersByDesign declares %d; a new raw read there is not covered by the declared reason (%s) — normalize it, or re-justify and update the count", name, got, declared.reads, declared.reason)
		}
	}
}

// rawPathSinks resolves the functions a raw path may flow into directly, by
// their declared objects.
func rawPathSinks(t *testing.T, evidence *treeEvidence) map[*types.Func]rawPathSink {
	t.Helper()

	lookup := func(pkgPath, name string) *types.Func {
		pkg := evidence.packageByPath(pkgPath)
		if pkg == nil {
			t.Fatalf("the sweep loaded no package %s, so the sink %s cannot be resolved", pkgPath, name)
		}
		fn, ok := pkg.Types.Scope().Lookup(name).(*types.Func)
		if !ok {
			t.Fatalf("%s.%s is not a function in the loaded tree; the barrier's sink list is out of date", pkgPath, name)
		}
		return fn
	}

	return map[*types.Func]rawPathSink{
		lookup(rawPathModulePath+"/internal/httpx", "RoutingNormalizedPath"): {pathArgument: 0, verdict: rawPathNormalized},
		lookup(rawPathModulePath+"/internal/httpx", "HasRoutingPrefix"):      {pathArgument: 0, verdict: rawPathNormalized},
		lookup(rawPathModulePath+"/internal/api", "IsCalendarFeedRequest"):   {pathArgument: 1, verdict: rawPathCalendarFeed},
	}
}

// collectRawPathUses finds every use — call, method value or method
// expression — of a raw path reader in pkg's files and judges it.
func collectRawPathUses(pkg *packages.Package, sinks map[*types.Func]rawPathSink) []rawPathUse {
	relativeDir := strings.TrimPrefix(strings.TrimPrefix(pkg.PkgPath, rawPathModulePath), "/")

	var uses []rawPathUse
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			enclosing := ""
			if function, ok := decl.(*ast.FuncDecl); ok {
				if object, ok := pkg.TypesInfo.Defs[function.Name].(*types.Func); ok {
					enclosing = object.FullName()
				}
			}

			var stack []ast.Node
			ast.Inspect(decl, func(node ast.Node) bool {
				if node == nil {
					stack = stack[:len(stack)-1]
					return true
				}
				stack = append(stack, node)

				selector, ok := node.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				reader := rawPathReaderName(pkg.TypesInfo, selector)
				if reader == "" {
					return true
				}
				position := pkg.Fset.Position(selector.Sel.Pos())
				uses = append(uses, rawPathUse{
					location:  fmt.Sprintf("%s/%s:%d", relativeDir, filepath.Base(position.Filename), position.Line),
					enclosing: enclosing,
					reader:    reader,
					verdict:   classifyRawPathUse(pkg.TypesInfo, stack, enclosing, sinks),
				})
				return true
			})
		}
	}
	return uses
}

// rawPathReaderName names the raw path reader selector resolves to, or "" when
// it is none: fiber's Path and OriginalURL (whatever the receiver — the Ctx
// interface or the concrete context), and fasthttp's URI.Path and
// URI.PathOriginal.
func rawPathReaderName(info *types.Info, selector *ast.SelectorExpr) string {
	var object types.Object
	if selection, ok := info.Selections[selector]; ok {
		object = selection.Obj()
	} else {
		object = info.Uses[selector.Sel]
	}
	function, ok := object.(*types.Func)
	if !ok || function.Pkg() == nil {
		return ""
	}
	function = function.Origin()

	switch function.Pkg().Path() {
	case rawPathFiberPath:
		if function.Name() == "Path" || function.Name() == "OriginalURL" {
			return function.FullName()
		}
	case rawPathFastHTTP:
		if (function.Name() == "Path" || function.Name() == "PathOriginal") && receiverTypeName(function) == "URI" {
			return function.FullName()
		}
	}
	return ""
}

func receiverTypeName(function *types.Func) string {
	signature, ok := function.Type().(*types.Signature)
	if !ok || signature.Recv() == nil {
		return ""
	}
	receiver := signature.Recv().Type()
	if pointer, ok := receiver.(*types.Pointer); ok {
		receiver = pointer.Elem()
	}
	if named, ok := types.Unalias(receiver).(*types.Named); ok {
		return named.Obj().Name()
	}
	return ""
}

// classifyRawPathUse judges one reader use. stack ends at the reader's
// selector. Only a CALL that is itself the direct path argument of a sink is
// accepted on its own merits; an alias, a method value or a wrapped expression
// is an offender unless its enclosing function is declared.
func classifyRawPathUse(info *types.Info, stack []ast.Node, enclosing string, sinks map[*types.Func]rawPathSink) rawPathVerdict {
	if len(stack) >= 3 {
		call, isCall := stack[len(stack)-2].(*ast.CallExpr)
		outer, isOuterCall := stack[len(stack)-3].(*ast.CallExpr)
		if isCall && isOuterCall && call.Fun == stack[len(stack)-1] {
			if sink, ok := sinks[calleeFunction(info, outer)]; ok &&
				sink.pathArgument < len(outer.Args) && outer.Args[sink.pathArgument] == ast.Expr(call) {
				return sink.verdict
			}
		}
	}
	if _, declared := rawPathReadersByDesign[enclosing]; declared && enclosing != "" {
		return rawPathAllowListed
	}
	return rawPathOffender
}

func calleeFunction(info *types.Info, call *ast.CallExpr) *types.Func {
	var identifier *ast.Ident
	switch callee := call.Fun.(type) {
	case *ast.Ident:
		identifier = callee
	case *ast.SelectorExpr:
		identifier = callee.Sel
	default:
		return nil
	}
	function, _ := info.Uses[identifier].(*types.Func)
	return function
}

func countRawPathUses(uses []rawPathUse, enclosing string, verdict rawPathVerdict) int {
	count := 0
	for _, use := range uses {
		if use.enclosing == enclosing && use.verdict == verdict {
			count++
		}
	}
	return count
}

func describeEnclosing(enclosing string) string {
	if enclosing == "" {
		return "a package-level declaration"
	}
	return enclosing
}
