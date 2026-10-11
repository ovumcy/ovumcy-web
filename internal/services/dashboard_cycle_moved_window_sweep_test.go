package services

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// BuildDashboardCycleContext leaves DashboardCycleContext.MovedWindow
// unreconciled; a page that builds the context and publishes its stats from
// ConfirmedAndOverviewStats sets it from that call's verdict. A page that
// forgets the assignment publishes a status read against the rolled window
// beside a context that says nothing moved. The sweep reads the production
// source under internal/ for every function calling both, with no list of pages
// to keep current, and asks each for the assignment. The calendar builds the
// context but publishes no overview copy and renders no hero, so it calls
// neither the second function nor reads the field.
func TestEveryPageBuildingTheDashboardCycleContextSetsItsMovedWindow(t *testing.T) {
	callers := map[string]bool{}
	fileSet := token.NewFileSet()
	err := filepath.WalkDir("..", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return walkErr
		}
		file, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return err
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			buildsContext, publishesOverview, assigns := false, false, false
			ast.Inspect(function.Body, func(node ast.Node) bool {
				switch typed := node.(type) {
				case *ast.CallExpr:
					name := ""
					switch callee := typed.Fun.(type) {
					case *ast.Ident:
						name = callee.Name
					case *ast.SelectorExpr:
						name = callee.Sel.Name
					}
					buildsContext = buildsContext || name == "BuildDashboardCycleContext"
					publishesOverview = publishesOverview || name == "ConfirmedAndOverviewStats"
				case *ast.AssignStmt:
					for _, target := range typed.Lhs {
						if selector, ok := target.(*ast.SelectorExpr); ok && selector.Sel.Name == "MovedWindow" {
							assigns = true
						}
					}
				}
				return true
			})
			if !buildsContext || !publishesOverview {
				continue
			}
			callers[function.Name.Name] = true
			if !assigns {
				t.Errorf("%s (%s) builds a DashboardCycleContext beside ConfirmedAndOverviewStats and never sets its MovedWindow", function.Name.Name, filepath.ToSlash(path))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal: %v", err)
	}
	for _, page := range []string{"BuildDashboardViewData", "BuildStatsPageViewData"} {
		if !callers[page] {
			t.Fatalf("the sweep did not reach %s; it is vacuous", page)
		}
	}
}
