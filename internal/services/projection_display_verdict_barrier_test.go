package services

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// Barrier for the "surface answers the display question on its own" class.
//
// The webhook and the .ics feed once read only PredictionsSuppressed while the
// dashboard answered the irregular thin-history tier and the range-or-date
// question itself, so both sent single dates the page withheld or widened into
// a window. The verdicts now live in two declarations every surface must reach:
//
//   - DashboardAwaitingIrregularHistory, the thin-history tier, reached by every
//     surface that names a projected date;
//   - ResolveProjectionRanges, the range-or-date answer, reached by every
//     surface that shapes the next projected period or ovulation.
//
// Reachability is resolved by DECLARATION (types.Info.Uses → *types.Func), not
// by matching an identifier's spelling: a local helper that happens to share a
// name is a different object and does not count, and a call routed through any
// number of helpers does. The anti-vacuity checks name the load-bearing edges
// rather than counting them.

// projectionVerdictSurfaces maps each projected-date surface to the verdicts it
// must reach.
var projectionVerdictSurfaces = map[string][]string{
	"decideDueReminders":              {"DashboardAwaitingIrregularHistory", "ResolveProjectionRanges"},
	"calendarFeedEvents":              {"DashboardAwaitingIrregularHistory", "ResolveProjectionRanges"},
	"buildDashboardPredictionDisplay": {"DashboardAwaitingIrregularHistory", "ResolveProjectionRanges"},
	"buildCalendarPredictionMaps":     {"DashboardAwaitingIrregularHistory", "ResolveProjectionRanges"},
	"BuildDashboardCycleHero":         {"ResolveProjectionRanges"},
	"PublishedStats":                  {"DashboardAwaitingIrregularHistory"},
}

// projectionVerdictLoadBearing are the edges the verdicts travel through by
// name: if one of them disappears while the surface still "reaches" the verdict
// through some other path, the reachability answer above is about a different
// route than the one this barrier was written for, and it says so.
var projectionVerdictLoadBearing = [][2]string{
	{"PredictionsSuppressed", "DashboardAwaitingIrregularHistory"},
	{"decideDueReminders", "ResolveProjectionRanges"},
	{"calendarFeedEvents", "ResolveProjectionRanges"},
	{"applyDashboardPredictionRanges", "ResolveProjectionRanges"},
	{"appendPredictedStartRange", "ResolveProjectionRanges"},
	{"dashboardCycleHeroStartWindow", "ResolveProjectionRanges"},
}

// projectionVerdictSoleReferrers pins a range builder to the one declaration
// allowed to call it: a second caller is a surface building a next-period or
// ovulation range beside the shared answer instead of through it.
var projectionVerdictSoleReferrers = map[string]string{
	"DashboardOvulationRange":  "ResolveProjectionRanges",
	"DashboardPredictionRange": "ResolveProjectionRanges",
}

func TestEveryProjectedDateSurfaceReachesTheSharedDisplayVerdict(t *testing.T) {
	graph := loadProjectionVerdictGraph(t)

	var failures []string
	for surface, verdicts := range projectionVerdictSurfaces {
		for _, verdict := range verdicts {
			reached, err := graph.reaches(surface, verdict)
			if err != nil {
				t.Fatal(err)
			}
			if !reached {
				failures = append(failures, surface+" never reaches "+verdict)
			}
		}
	}
	for _, edge := range projectionVerdictLoadBearing {
		direct, err := graph.references(edge[0], edge[1])
		if err != nil {
			t.Fatal(err)
		}
		if !direct {
			failures = append(failures, edge[0]+" no longer references "+edge[1]+" directly (load-bearing edge)")
		}
	}
	for callee, allowed := range projectionVerdictSoleReferrers {
		referrers, err := graph.referrers(callee)
		if err != nil {
			t.Fatal(err)
		}
		if len(referrers) == 0 {
			failures = append(failures, callee+" has no production referrer — the sole-referrer check reads nothing")
		}
		for _, referrer := range referrers {
			if referrer != allowed {
				failures = append(failures, callee+" is referenced by "+referrer+", not only by "+allowed)
			}
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		t.Fatalf("a projected-date surface answers the display question outside the shared verdict (DashboardAwaitingIrregularHistory / ResolveProjectionRanges):\n  %s", strings.Join(failures, "\n  "))
	}
}

// TestProjectionVerdictBarrierRecognisesItsOwnFixtures runs the same graph over
// a fixture: a surface reaching the verdict through a helper passes, a surface
// calling a same-named local that is a different object fails, and a method
// value passed as a callback counts as a reference.
func TestProjectionVerdictBarrierRecognisesItsOwnFixtures(t *testing.T) {
	const fixture = `package fixture

func Verdict() bool { return true }

func helper() bool { return Verdict() }

func viaHelper() bool { return helper() }

type shadow struct{}

func (shadow) Verdict() bool { return false }

func viaShadow() bool { return shadow{}.Verdict() }

func viaCallback() bool { return apply(Verdict) }

func apply(f func() bool) bool { return f() }
`
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "fixture.go", fixture, 0)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}, Uses: map[*ast.Ident]types.Object{}}
	pkg, err := (&types.Config{Importer: importer.Default()}).Check("fixture", fileSet, []*ast.File{file}, info)
	if err != nil {
		t.Fatalf("type-check fixture: %v", err)
	}
	graph := newProjectionVerdictGraph(pkg, []*ast.File{file}, info)

	for surface, want := range map[string]bool{"viaHelper": true, "viaShadow": false, "viaCallback": true} {
		reached, err := graph.reaches(surface, "Verdict")
		if err != nil {
			t.Fatal(err)
		}
		if reached != want {
			t.Fatalf("fixture %s reaches Verdict = %t, want %t", surface, reached, want)
		}
	}
	if direct, _ := graph.references("viaHelper", "Verdict"); direct {
		t.Fatal("fixture: viaHelper reaches Verdict only through helper, yet reads as a direct reference")
	}
	if _, err := graph.reaches("noSuchSurface", "Verdict"); err == nil {
		t.Fatal("fixture: an unknown surface must be an error, not a silent false")
	}
}

// projectionVerdictGraph is the package's reference graph between package-level
// functions, keyed by declaration.
type projectionVerdictGraph struct {
	scope *types.Scope
	edges map[*types.Func]map[*types.Func]bool
	names map[*types.Func]string
}

func loadProjectionVerdictGraph(t *testing.T) *projectionVerdictGraph {
	t.Helper()
	config := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Dir:  predictionSuppressionRepoRoot(t),
		// Production only: a test that calls the verdict is not a surface that does.
		Tests: false,
	}
	loaded, err := packages.Load(config, "./internal/services")
	if err != nil {
		t.Fatalf("load internal/services: %v", err)
	}
	if len(loaded) != 1 || len(loaded[0].Errors) > 0 || loaded[0].Types == nil {
		t.Fatalf("internal/services did not type-check, so no reference could be resolved: %v", loaded)
	}
	if len(loaded[0].Syntax) < 50 {
		t.Fatalf("parsed only %d file(s) of internal/services; the sweep read the wrong tree", len(loaded[0].Syntax))
	}
	return newProjectionVerdictGraph(loaded[0].Types, loaded[0].Syntax, loaded[0].TypesInfo)
}

func newProjectionVerdictGraph(pkg *types.Package, files []*ast.File, info *types.Info) *projectionVerdictGraph {
	graph := &projectionVerdictGraph{
		scope: pkg.Scope(),
		edges: map[*types.Func]map[*types.Func]bool{},
		names: map[*types.Func]string{},
	}
	for _, file := range files {
		for _, decl := range file.Decls {
			function, isFunction := decl.(*ast.FuncDecl)
			if !isFunction || function.Body == nil {
				continue
			}
			caller, isFunc := info.Defs[function.Name].(*types.Func)
			if !isFunc {
				continue
			}
			graph.names[caller] = function.Name.Name
			targets := map[*types.Func]bool{}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				if ident, isIdent := node.(*ast.Ident); isIdent {
					if callee, isFunc := info.Uses[ident].(*types.Func); isFunc && callee.Pkg() == pkg {
						targets[callee] = true
					}
				}
				return true
			})
			graph.edges[caller] = targets
		}
	}
	return graph
}

func (graph *projectionVerdictGraph) lookup(name string) (*types.Func, error) {
	function, isFunc := graph.scope.Lookup(name).(*types.Func)
	if !isFunc {
		return nil, &projectionVerdictLookupError{name: name}
	}
	return function, nil
}

type projectionVerdictLookupError struct{ name string }

func (err *projectionVerdictLookupError) Error() string {
	return "no package-level function " + err.name + " — the barrier names a declaration the tree no longer has"
}

func (graph *projectionVerdictGraph) reaches(from string, to string) (bool, error) {
	source, err := graph.lookup(from)
	if err != nil {
		return false, err
	}
	target, err := graph.lookup(to)
	if err != nil {
		return false, err
	}
	seen := map[*types.Func]bool{source: true}
	queue := []*types.Func{source}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for next := range graph.edges[current] {
			if next == target {
				return true, nil
			}
			if !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	return false, nil
}

func (graph *projectionVerdictGraph) references(from string, to string) (bool, error) {
	source, err := graph.lookup(from)
	if err != nil {
		return false, err
	}
	target, err := graph.lookup(to)
	if err != nil {
		return false, err
	}
	return graph.edges[source][target], nil
}

func (graph *projectionVerdictGraph) referrers(to string) ([]string, error) {
	target, err := graph.lookup(to)
	if err != nil {
		return nil, err
	}
	var names []string
	for caller, targets := range graph.edges {
		if targets[target] {
			names = append(names, graph.names[caller])
		}
	}
	sort.Strings(names)
	return names, nil
}
