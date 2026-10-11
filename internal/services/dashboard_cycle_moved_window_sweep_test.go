package services

import (
	"go/ast"
	"go/types"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

const movedWindowServicesPath = "github.com/ovumcy/ovumcy-web/internal/services"

// BuildDashboardCycleContext leaves DashboardCycleContext.MovedWindow
// unreconciled; a page that builds the context and publishes its stats from
// ConfirmedAndOverviewStats sets it from that call's verdict. A page that
// forgets the assignment publishes a status read against the rolled window
// beside a context that says nothing moved.
//
// The sweep resolves the two functions and the field by declaration
// (go/packages + types.Info), never by the spelling of an identifier, and
// follows the production call graph: a "page builder" is a function that
// reaches BOTH functions and has no callee that already does, so a helper that
// builds the context or publishes the stats cannot move a page out of its reach.
// Each page builder must reach an assignment whose target is that field. There
// is no list of pages to keep current.
//
// Two limits, stated rather than hidden. Dynamic dispatch (an interface method
// call) is not followed, so a builder reaching the pair only through one is
// outside this walk; the runtime test below covers the dashboard page. And the
// assignment is found anywhere in the builder's reach, so one that landed on the
// wrong context value of the right type is not distinguished here.
//
// The calendar builds the context for its prediction explanation but publishes no
// overview copy and renders no hero, so it never reaches the second function and
// is outside the class by construction; the sweep pins that by name, so the day
// the calendar starts publishing it becomes a page builder and owes the
// assignment.
func TestEveryPageBuildingTheDashboardCycleContextSetsItsMovedWindow(t *testing.T) {
	tree := loadMovedWindowTree(t)

	reachesContext := tree.callersOf(tree.buildContext)
	reachesOverview := tree.callersOf(tree.publishOverview)
	pageBuilders := map[string]*movedWindowFunction{}
	for fn, function := range tree.functions {
		if !reachesContext[fn] || !reachesOverview[fn] {
			continue
		}
		hasInnerPage := false
		for callee := range function.callees {
			if callee != fn && reachesContext[callee] && reachesOverview[callee] {
				hasInnerPage = true
			}
		}
		if hasInnerPage {
			continue
		}
		pageBuilders[function.label()] = function
		if !tree.reachesAssignment(fn) {
			t.Errorf("%s (%s) reaches BuildDashboardCycleContext and ConfirmedAndOverviewStats and never sets DashboardCycleContext.MovedWindow", function.label(), function.position)
		}
	}

	// The sweep must have reached the two owner pages, by name, or it measured
	// nothing.
	for _, page := range []string{"BuildDashboardViewData", "BuildStatsPageViewData"} {
		if _, reached := pageBuilders[page]; !reached {
			t.Fatalf("the sweep did not reach %s as a page builder (found %s); it is vacuous", page, strings.Join(sortedKeys(pageBuilders), ", "))
		}
	}

	// The calendar exemption holds only while the calendar stays outside the pair.
	calendar := tree.named("BuildCalendarPageViewData")
	if calendar == nil {
		t.Fatal("the calendar page builder is gone; the exemption this sweep names no longer points at anything")
	}
	if !reachesContext[calendar.fn] || reachesOverview[calendar.fn] {
		t.Errorf("BuildCalendarPageViewData builds the context = %t and publishes the overview = %t; the exemption holds only for a builder that does the first and not the second", reachesContext[calendar.fn], reachesOverview[calendar.fn])
	}
}

type movedWindowFunction struct {
	fn       *types.Func
	position string
	assigns  bool
	callees  map[*types.Func]bool
}

func (function *movedWindowFunction) label() string { return function.fn.Name() }

type movedWindowTree struct {
	functions       map[*types.Func]*movedWindowFunction
	buildContext    *types.Func
	publishOverview *types.Func
	field           *types.Var
}

func (tree *movedWindowTree) named(name string) *movedWindowFunction {
	for fn, function := range tree.functions {
		if fn.Name() == name && fn.Pkg() != nil && fn.Pkg().Path() == movedWindowServicesPath {
			return function
		}
	}
	return nil
}

// callersOf is every function whose call graph reaches target, target included.
func (tree *movedWindowTree) callersOf(target *types.Func) map[*types.Func]bool {
	callers := map[*types.Func][]*types.Func{}
	for fn, function := range tree.functions {
		for callee := range function.callees {
			callers[callee] = append(callers[callee], fn)
		}
	}
	reached := map[*types.Func]bool{target: true}
	queue := []*types.Func{target}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, caller := range callers[current] {
			if !reached[caller] {
				reached[caller] = true
				queue = append(queue, caller)
			}
		}
	}
	return reached
}

// reachesAssignment says whether start, or anything it calls, assigns the field.
func (tree *movedWindowTree) reachesAssignment(start *types.Func) bool {
	seen := map[*types.Func]bool{start: true}
	queue := []*types.Func{start}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		function := tree.functions[current]
		if function == nil {
			continue
		}
		if function.assigns {
			return true
		}
		for callee := range function.callees {
			if !seen[callee] {
				seen[callee] = true
				queue = append(queue, callee)
			}
		}
	}
	return false
}

func loadMovedWindowTree(t *testing.T) *movedWindowTree {
	t.Helper()

	config := &packages.Config{
		// NeedDeps keeps one *types.Object per declaration across the packages, so a
		// use in another package resolves to the very object the services package
		// declares.
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedImports | packages.NeedDeps,
		Dir: predictionSuppressionRepoRoot(t),
		// Production only: a test that calls the pair is not a page.
		Tests: false,
	}
	loaded, err := packages.Load(config, "./internal/...")
	if err != nil {
		t.Fatalf("load internal: %v", err)
	}
	var services *packages.Package
	for _, pkg := range loaded {
		if len(pkg.Errors) > 0 {
			t.Fatalf("%s did not type-check, so no declaration could be resolved: %v", pkg.PkgPath, pkg.Errors[0])
		}
		if pkg.PkgPath == movedWindowServicesPath {
			services = pkg
		}
	}
	if services == nil || services.Types == nil {
		t.Fatalf("%s was not loaded", movedWindowServicesPath)
	}

	tree := &movedWindowTree{functions: map[*types.Func]*movedWindowFunction{}}
	var ok bool
	if tree.buildContext, ok = services.Types.Scope().Lookup("BuildDashboardCycleContext").(*types.Func); !ok {
		t.Fatal("BuildDashboardCycleContext is gone; the sweep's subject moved")
	}
	if tree.publishOverview, ok = services.Types.Scope().Lookup("ConfirmedAndOverviewStats").(*types.Func); !ok {
		t.Fatal("ConfirmedAndOverviewStats is gone; the sweep's subject moved")
	}
	contextType, ok := services.Types.Scope().Lookup("DashboardCycleContext").(*types.TypeName)
	if !ok {
		t.Fatal("DashboardCycleContext is gone; the sweep's subject moved")
	}
	contextStruct, ok := contextType.Type().Underlying().(*types.Struct)
	if !ok {
		t.Fatal("DashboardCycleContext is not a struct")
	}
	for index := 0; index < contextStruct.NumFields(); index++ {
		if contextStruct.Field(index).Name() == "MovedWindow" {
			tree.field = contextStruct.Field(index)
		}
	}
	if tree.field == nil {
		t.Fatal("DashboardCycleContext has no MovedWindow field; the sweep's subject moved")
	}

	for _, pkg := range loaded {
		for _, file := range pkg.Syntax {
			for _, declaration := range file.Decls {
				decl, isFunction := declaration.(*ast.FuncDecl)
				if !isFunction || decl.Body == nil {
					continue
				}
				fn, isFunc := pkg.TypesInfo.Defs[decl.Name].(*types.Func)
				if !isFunc {
					continue
				}
				function := &movedWindowFunction{
					fn:       fn,
					position: pkg.Fset.Position(decl.Pos()).String(),
					callees:  map[*types.Func]bool{},
				}
				ast.Inspect(decl.Body, func(node ast.Node) bool {
					switch typed := node.(type) {
					case *ast.Ident:
						if callee, isCallee := pkg.TypesInfo.Uses[typed].(*types.Func); isCallee {
							function.callees[callee.Origin()] = true
						}
					case *ast.AssignStmt:
						for _, target := range typed.Lhs {
							selector, isSelector := ast.Unparen(target).(*ast.SelectorExpr)
							if !isSelector {
								continue
							}
							if selection := pkg.TypesInfo.Selections[selector]; selection != nil && selection.Kind() == types.FieldVal && selection.Obj() == tree.field {
								function.assigns = true
							}
						}
					}
					return true
				})
				tree.functions[fn] = function
			}
		}
	}
	return tree
}
