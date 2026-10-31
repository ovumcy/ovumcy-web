package db

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// TestEveryCalendarFeedDisarmClearsTheDetailedTitlesOptIn is WEB-389's
// completeness guard. A disarm is any update map that NULLs
// calendar_feed_selector; each one must also reset calendar_feed_detailed_titles,
// because a pre-v2 binary mints a link without writing that column and would
// otherwise hand the revoked link's opt-in to the new one after a re-upgrade.
// The set is read from the source, so a disarm added later is covered the
// moment it is written.
func TestEveryCalendarFeedDisarmClearsTheDetailedTitlesOptIn(t *testing.T) {
	const path = "user_repository.go"
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	disarms := 0
	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		values := map[string]ast.Expr{}
		for _, element := range literal.Elts {
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
			t.Errorf("%s: a calendar-feed disarm leaves calendar_feed_detailed_titles standing", fileSet.Position(literal.Pos()))
		}
		return true
	})
	// Seven disarms today: revoke, the two boot-time bulk disarms, recovery-code
	// rotation, operator reset, reset-token redeem and clear-data.
	if disarms < 7 {
		t.Fatalf("found %d calendar-feed disarm(s), expected at least 7 — the scan stopped matching them", disarms)
	}
}
