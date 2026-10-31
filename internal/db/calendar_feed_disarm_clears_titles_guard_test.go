package db

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// TestEveryCalendarFeedDisarmClearsTheDetailedTitlesOptIn is WEB-389's
// completeness guard. A disarm is any write that NULLs calendar_feed_selector;
// each one must also reset calendar_feed_detailed_titles, because a pre-v2
// binary mints a link without writing that column and would otherwise hand the
// revoked link's opt-in to the new one after a re-upgrade. The set is read from
// the source, so a disarm added later is covered the moment it is written.
func TestEveryCalendarFeedDisarmClearsTheDetailedTitlesOptIn(t *testing.T) {
	disarms, problems := scanCalendarFeedDisarms(t, "user_repository.go", nil)
	for _, problem := range problems {
		t.Error(problem)
	}
	// Seven disarms today: revoke, the two boot-time bulk disarms, recovery-code
	// rotation, operator reset, reset-token redeem and clear-data.
	if disarms < 7 {
		t.Fatalf("found %d calendar-feed disarm(s), expected at least 7 — the scan stopped matching them", disarms)
	}
}

// TestCalendarFeedDisarmScanSeesEveryWriteForm proves the scan is not blind to
// the forms a disarm can take: a map literal that forgets the flag, and a
// single-column Update/UpdateColumn of the selector, which by construction
// cannot clear the flag in the same statement.
func TestCalendarFeedDisarmScanSeesEveryWriteForm(t *testing.T) {
	const source = `package db
func a(q query) { q.Updates(map[string]any{"calendar_feed_selector": nil}) }
func b(q query) { q.Update("calendar_feed_selector", nil) }
func c(q query) { q.UpdateColumn("calendar_feed_selector", nil) }
func d(q query) { q.Updates(map[string]any{"calendar_feed_selector": nil, "calendar_feed_detailed_titles": false}) }
`
	disarms, problems := scanCalendarFeedDisarms(t, "synthetic.go", source)
	if disarms != 4 || len(problems) != 3 {
		t.Fatalf("expected 4 disarms and 3 problems (a, b, c), got %d and %v", disarms, problems)
	}
	for _, want := range []string{"synthetic.go:2", "synthetic.go:3", "synthetic.go:4"} {
		found := false
		for _, problem := range problems {
			found = found || strings.HasPrefix(problem, want)
		}
		if !found {
			t.Fatalf("the scan missed the disarm at %s: %v", want, problems)
		}
	}
}

func scanCalendarFeedDisarms(t *testing.T, path string, source any) (int, []string) {
	t.Helper()
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, source, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	disarms := 0
	var problems []string
	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.CallExpr:
			// A single-column write of the selector clears nothing beside it.
			callee, ok := typed.Fun.(*ast.SelectorExpr)
			if !ok || (callee.Sel.Name != "Update" && callee.Sel.Name != "UpdateColumn") || len(typed.Args) < 2 {
				return true
			}
			if column, ok := typed.Args[0].(*ast.BasicLit); ok && column.Value == `"calendar_feed_selector"` {
				disarms++
				problems = append(problems, fileSet.Position(typed.Pos()).String()+": a single-column calendar-feed disarm cannot clear calendar_feed_detailed_titles")
			}
		case *ast.CompositeLit:
			values := map[string]ast.Expr{}
			for _, element := range typed.Elts {
				pair, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := pair.Key.(*ast.BasicLit); ok && key.Kind == token.STRING {
					values[key.Value] = pair.Value
				}
			}
			selector, ok := values[`"calendar_feed_selector"`].(*ast.Ident)
			if !ok || selector.Name != "nil" {
				return true
			}
			disarms++
			cleared, ok := values[`"calendar_feed_detailed_titles"`].(*ast.Ident)
			if !ok || cleared.Name != "false" {
				problems = append(problems, fileSet.Position(typed.Pos()).String()+": a calendar-feed disarm leaves calendar_feed_detailed_titles standing")
			}
		}
		return true
	})
	return disarms, problems
}
