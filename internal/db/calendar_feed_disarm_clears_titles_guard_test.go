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
// writes the column. Raw SQL is refused by where the column name comes from,
// not by where it goes: every string constant naming the selector must be a
// column-map key, a const declaration (judged at each use), an
// argument of Where, or an argument of Select in a function that writes
// nothing. Any other spelling is refused however it reaches Raw or Exec —
// through a variable, a parameter, a (string, error) return, an interface or
// a generic call. A single-column Update, a key added by index and a
// models.User field write are refused by
// TestEveryCalendarFeedSelectorWriterAlsoClearsTheLastPolledMark. A name
// assembled at run time from fragments, and SQL loaded from outside the source
// (the migration runner's files), are out of reach of any source scan;
// migrations are pinned by their own tests.
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
// flag, raw SQL spelled inline, in a variable, as a format argument, in a
// helper's return (single or with an error), in a struct field, as a
// parameter, behind an interface or a generic, a package-level column map, and
// a Select feeding a write — while a Where, a read-only Select and an arm
// through a named string type pass.
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
func rawConstant() { query{}.Exec("UPDATE users SET " + selectorKey + " = NULL") }
func rawVariable() { stmt := "UPDATE users SET " + "calendar_feed_selector = NULL"; query{}.Exec(stmt) }
var packageStatement = "UPDATE users SET calendar_feed_selector = NULL"
func rawPackageVariable() { query{}.Exec(packageStatement) }
func rawBuilt() { query{}.Exec(sprintf("UPDATE users SET %s = NULL", "calendar_feed_selector")) }
func sprintf(format string, args ...any) string { return format }
func disarmSQL() string { return "UPDATE users SET calendar_feed_selector = NULL" }
func rawHelper() { query{}.Exec(disarmSQL()) }
func tupleSQL() (string, error) { return "UPDATE users SET calendar_feed_selector = NULL", nil }
type repository struct{ statement string }
func newRepository() repository { return repository{statement: "UPDATE users SET calendar_feed_selector = NULL"} }
func (r repository) rawField() { query{}.Exec(r.statement) }
func run(statement string) { query{}.Exec(statement) }
func rawParameter() { run("UPDATE users SET calendar_feed_selector = NULL") }
type source interface{ Statement() string }
type disarmSource struct{}
func (disarmSource) Statement() string { return "UPDATE users SET calendar_feed_selector = NULL" }
func rawInterface(s source) { query{}.Exec(s.Statement()) }
func pick[T any](v T) T { return v }
func rawGeneric() { query{}.Exec(pick("UPDATE users SET calendar_feed_selector = NULL")) }
var packageColumns = map[string]any{"calendar_feed_selector": nil}
type chain struct{}
func (chain) Where(string, ...any) chain { return chain{} }
func (chain) Select(...string) chain { return chain{} }
func (chain) First(any) chain { return chain{} }
func (chain) Updates(any) chain { return chain{} }
func read() { chain{}.Where("calendar_feed_selector = ?", "x").Select("calendar_feed_selector").First(nil) }
func whereWrite() { chain{}.Where("calendar_feed_selector = ?", "x").Updates(nil) }
func selectWrite() { chain{}.Select("calendar_feed_selector").Updates(nil) }
type FeedSelector string
func namedArm(s FeedSelector, d bool) any { return map[string]any{"calendar_feed_selector": s, "calendar_feed_detailed_titles": d} }
`
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "synthetic.go", source, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	info := &types.Info{
		Types:     map[ast.Expr]types.TypeAndValue{},
		Defs:      map[*ast.Ident]types.Object{},
		Uses:      map[*ast.Ident]types.Object{},
		Instances: map[*ast.Ident]types.Instance{},
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
	wantProblems := []string{
		"missing at", "constantKey at", "empty at", "kept at", "arm at", "typedNil at", "expression at",
		"raw at", "rawConstant at", "rawVariable at", "packageStatement at", "rawBuilt at", "disarmSQL at",
		"tupleSQL at", "newRepository at", "rawParameter at", "Statement at", "rawGeneric at",
		"packageColumns at", "selectWrite at",
	}
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
	writeMethods := map[string]bool{
		"Update": true, "Updates": true, "UpdateColumn": true, "UpdateColumns": true,
		"Save": true, "Create": true, "Delete": true, "Exec": true, "Raw": true,
	}

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
	methodOf := func(call *ast.CallExpr) string {
		if callee, ok := call.Fun.(*ast.SelectorExpr); ok {
			return callee.Sel.Name
		}
		return ""
	}

	disarms := map[string]bool{}
	var problems []string
	// The places a constant naming the selector may sit; any other is refused.
	allowed := map[ast.Expr]bool{}
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			if constants, ok := node.(*ast.GenDecl); ok && constants.Tok == token.CONST {
				for _, spec := range constants.Specs {
					for _, value := range spec.(*ast.ValueSpec).Values {
						allowed[value] = true
					}
				}
			}
			return true
		})
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			name := function.Name.Name
			writes := false
			ast.Inspect(function.Body, func(node ast.Node) bool {
				if call, ok := node.(*ast.CallExpr); ok && writeMethods[methodOf(call)] {
					writes = true
				}
				return !writes
			})
			ast.Inspect(function.Body, func(node ast.Node) bool {
				if call, ok := node.(*ast.CallExpr); ok {
					if method := methodOf(call); method == "Where" || (method == "Select" && !writes) {
						for _, argument := range call.Args {
							allowed[argument] = true
						}
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
					allowed[keyValue.Key] = true
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

	refuseStrayNames := func(name string, node ast.Node) {
		ast.Inspect(node, func(inner ast.Node) bool {
			expr, ok := inner.(ast.Expr)
			if !ok || !strings.Contains(columnOf(expr), selectorColumn) {
				return true
			}
			if !allowed[expr] {
				problems = append(problems, name+" at "+fileSet.Position(expr.Pos()).String()+": calendar_feed_selector is spelled outside a column map, a Where or a read-only Select; raw SQL cannot be checked for calendar_feed_detailed_titles")
			}
			return false
		})
	}
	for _, file := range files {
		for _, declaration := range file.Decls {
			switch typed := declaration.(type) {
			case *ast.FuncDecl:
				refuseStrayNames(typed.Name.Name, typed)
			case *ast.GenDecl:
				for _, spec := range typed.Specs {
					switch named := spec.(type) {
					case *ast.ValueSpec:
						refuseStrayNames(named.Names[0].Name, named)
					case *ast.TypeSpec:
						refuseStrayNames(named.Name.Name, named)
					}
				}
			}
		}
	}
	return disarms, problems
}
