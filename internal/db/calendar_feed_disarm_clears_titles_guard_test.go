package db

import (
	"go/ast"
	"go/constant"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"
)

// calendarFeedDisarmWriters are the functions that disarm a feed today. Each is
// asserted by name, so a scan that resolves nothing cannot pass over an empty set.
var calendarFeedDisarmWriters = []string{
	"ClearCalendarFeedToken",
	"DisarmCalendarFeedTokensWithoutMAC",
	"DisarmAllCalendarFeedTokens",
	"UpdateRecoveryCodeHashAndRevokeSessions",
	"ForceResetPasswordAndRevokeSessions",
	"UpdatePasswordRecoveryCodeAndRevokeSessionsCAS",
	"ClearAllDataAndResetSettings",
}

// TestEveryCalendarFeedDisarmClearsTheDetailedTitlesOptIn is WEB-389's
// completeness guard. Every column map that writes calendar_feed_selector must
// also write calendar_feed_detailed_titles, and a disarm (any selector value
// that is not a non-empty string) must write it false: a pre-v2 binary mints a
// link without writing that column, so a flag left on a revoked row would carry
// over to that link after a re-upgrade.
//
// Columns are resolved by the type checker over every non-test file of this
// package, so a key spelled as a named constant counts. Only this package
// writes the column. Raw SQL (Raw or Exec) naming the selector is refused here;
// a single-column Update, a key added by index and a models.User field write
// are refused by TestEveryCalendarFeedSelectorWriterAlsoClearsTheLastPolledMark.
// Raw SQL is read through variables, concatenation and call arguments; SQL
// loaded from outside the source (the migration runner's files) is out of reach
// of any source scan, and migrations are pinned by their own tests.
func TestEveryCalendarFeedDisarmClearsTheDetailedTitlesOptIn(t *testing.T) {
	pkg := loadTypedDBPackage(t)
	disarms, problems := scanCalendarFeedTitleWrites(pkg.Fset, pkg.TypesInfo, pkg.Syntax)
	for _, problem := range problems {
		t.Error(problem)
	}
	for _, required := range calendarFeedDisarmWriters {
		if !disarms[required] {
			t.Errorf("the scan did not find %s among the calendar-feed disarms (%v): it is not measuring what it claims", required, sortedNamesOf(disarms))
		}
	}
}

// TestCalendarFeedTitleScanSeesEveryDisarmForm proves the scan is not blind to
// the forms a disarm can take: a literal or constant key, a nil, typed-nil,
// expression or empty selector, a missing or true flag, an arm that omits the
// flag, raw SQL naming the selector directly, through a variable or as a
// format argument, and an arm through a named string type.
func TestCalendarFeedTitleScanSeesEveryDisarmForm(t *testing.T) {
	const source = `package db
const selectorKey = "calendar_feed_selector"
func missing() any { return map[string]any{"calendar_feed_selector": nil} }
func constantKey() any { return map[string]any{selectorKey: nil} }
func empty() any { return map[string]any{"calendar_feed_selector": ""} }
func kept() any { return map[string]any{"calendar_feed_selector": nil, "calendar_feed_detailed_titles": true} }
func arm(s string) any { return map[string]any{"calendar_feed_selector": s} }
func cleared() any { return map[string]any{"calendar_feed_selector": nil, "calendar_feed_detailed_titles": false} }
func armed(s string, d bool) any { return map[string]any{"calendar_feed_selector": s, "calendar_feed_detailed_titles": d} }
func null() any { return nil }
func typedNil(d bool) any { return map[string]any{"calendar_feed_selector": (*string)(nil), "calendar_feed_detailed_titles": d} }
func expression(d bool) any { return map[string]any{"calendar_feed_selector": null(), "calendar_feed_detailed_titles": d} }
type query struct{}
func (query) Exec(string, ...any) {}
func raw() { query{}.Exec("UPDATE users SET calendar_feed_selector = NULL") }
func rawVariable() { stmt := "UPDATE users SET " + "calendar_feed_selector = NULL"; query{}.Exec(stmt) }
var packageStatement = "UPDATE users SET calendar_feed_selector = NULL"
func rawPackageVariable() { query{}.Exec(packageStatement) }
func rawBuilt() { query{}.Exec(sprintf("UPDATE users SET %s = NULL", "calendar_feed_selector")) }
func sprintf(format string, args ...any) string { return format }
type FeedSelector string
func namedArm(s FeedSelector, d bool) any { return map[string]any{"calendar_feed_selector": s, "calendar_feed_detailed_titles": d} }
`
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "synthetic.go", source, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	info := &types.Info{
		Types: map[ast.Expr]types.TypeAndValue{},
		Defs:  map[*ast.Ident]types.Object{},
		Uses:  map[*ast.Ident]types.Object{},
	}
	if _, err := (&types.Config{Importer: importer.Default()}).Check("db", fileSet, []*ast.File{file}, info); err != nil {
		t.Fatalf("type-check: %v", err)
	}

	disarms, problems := scanCalendarFeedTitleWrites(fileSet, info, []*ast.File{file})
	for _, name := range []string{"missing", "constantKey", "empty", "kept", "cleared", "typedNil", "expression"} {
		if !disarms[name] {
			t.Errorf("the scan did not classify %s as a disarm: %v", name, sortedNamesOf(disarms))
		}
	}
	if disarms["arm"] || disarms["armed"] || disarms["namedArm"] {
		t.Errorf("the scan classified an arm as a disarm: %v", sortedNamesOf(disarms))
	}
	wantProblems := []string{"missing at", "constantKey at", "empty at", "kept at", "arm at", "typedNil at", "expression at", "raw at", "rawVariable at", "rawPackageVariable at", "rawBuilt at"}
	if len(problems) != len(wantProblems) {
		t.Fatalf("expected %d problems, got %v", len(wantProblems), problems)
	}
	for _, want := range wantProblems {
		found := false
		for _, problem := range problems {
			found = found || strings.HasPrefix(problem, want)
		}
		if !found {
			t.Errorf("the scan missed %s: %v", strings.TrimSuffix(want, " at"), problems)
		}
	}
}

func scanCalendarFeedTitleWrites(fileSet *token.FileSet, info *types.Info, files []*ast.File) (map[string]bool, []string) {
	const selectorColumn = "calendar_feed_selector"
	const titlesColumn = "calendar_feed_detailed_titles"

	columnOf := func(expr ast.Expr) string {
		value := info.Types[expr].Value
		if value == nil || value.Kind() != constant.String {
			return ""
		}
		return constant.StringVal(value)
	}
	isConstant := func(expr ast.Expr, want constant.Value) bool {
		value := info.Types[expr].Value
		return value != nil && value.Kind() == want.Kind() && constant.Compare(value, token.EQL, want)
	}

	// Every value a variable is given, anywhere in the files, so SQL held in a
	// variable is read through to the strings it was built from.
	assigned := map[types.Object][]ast.Expr{}
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.AssignStmt:
				if len(typed.Lhs) == len(typed.Rhs) {
					for index, target := range typed.Lhs {
						if ident, ok := target.(*ast.Ident); ok {
							if object := info.ObjectOf(ident); object != nil {
								assigned[object] = append(assigned[object], typed.Rhs[index])
							}
						}
					}
				}
			case *ast.ValueSpec:
				if len(typed.Names) == len(typed.Values) {
					for index, ident := range typed.Names {
						if object := info.ObjectOf(ident); object != nil {
							assigned[object] = append(assigned[object], typed.Values[index])
						}
					}
				}
			}
			return true
		})
	}
	visited := map[ast.Expr]bool{}
	var sqlNamesSelector func(ast.Expr) bool
	sqlNamesSelector = func(expr ast.Expr) bool {
		if visited[expr] {
			return false
		}
		visited[expr] = true
		if strings.Contains(columnOf(expr), selectorColumn) {
			return true
		}
		switch typed := expr.(type) {
		case *ast.ParenExpr:
			return sqlNamesSelector(typed.X)
		case *ast.BinaryExpr:
			return sqlNamesSelector(typed.X) || sqlNamesSelector(typed.Y)
		case *ast.CallExpr:
			// fmt.Sprintf and friends: any argument that names the column.
			for _, argument := range typed.Args {
				if sqlNamesSelector(argument) {
					return true
				}
			}
		case *ast.Ident:
			for _, value := range assigned[info.ObjectOf(typed)] {
				if sqlNamesSelector(value) {
					return true
				}
			}
		}
		return false
	}

	disarms := map[string]bool{}
	var problems []string
	for _, file := range files {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			name := function.Name.Name
			ast.Inspect(function.Body, func(node ast.Node) bool {
				if call, ok := node.(*ast.CallExpr); ok {
					callee, ok := call.Fun.(*ast.SelectorExpr)
					clear(visited)
					if ok && (callee.Sel.Name == "Raw" || callee.Sel.Name == "Exec") && len(call.Args) > 0 &&
						sqlNamesSelector(call.Args[0]) {
						problems = append(problems, name+" at "+fileSet.Position(call.Pos()).String()+": raw SQL names calendar_feed_selector; write it through a column map beside calendar_feed_detailed_titles")
					}
					return true
				}
				literal, ok := node.(*ast.CompositeLit)
				if !ok {
					return true
				}
				literalType := info.TypeOf(literal)
				if literalType == nil {
					return true
				}
				if _, isMap := literalType.Underlying().(*types.Map); !isMap {
					return true
				}
				var selector, titles ast.Expr
				for _, element := range literal.Elts {
					keyValue, ok := element.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					switch columnOf(keyValue.Key) {
					case selectorColumn:
						selector = keyValue.Value
					case titlesColumn:
						titles = keyValue.Value
					}
				}
				if selector == nil {
					return true
				}
				position := fileSet.Position(literal.Pos()).String()
				// Only a string (named or not) can arm a link; anything else — nil,
				// a typed nil, gorm.Expr("NULL") — is read as a disarm.
				arms := false
				if selectorType := info.TypeOf(selector); selectorType != nil {
					basic, isBasic := selectorType.Underlying().(*types.Basic)
					arms = isBasic && basic.Info()&types.IsString != 0 && !isConstant(selector, constant.MakeString(""))
				}
				disarm := !arms
				switch {
				case disarm:
					disarms[name] = true
					if titles == nil || !isConstant(titles, constant.MakeBool(false)) {
						problems = append(problems, name+" at "+position+": a calendar-feed disarm leaves calendar_feed_detailed_titles standing")
					}
				case titles == nil:
					problems = append(problems, name+" at "+position+": a calendar_feed_selector write does not decide calendar_feed_detailed_titles for the link it arms")
				}
				return true
			})
		}
	}
	return disarms, problems
}
