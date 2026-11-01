package db

import (
	"go/ast"
	"go/constant"
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

// calendarFeedSelectorReaders may name the selector in a gorm Select. A Select
// also picks the columns an Updates writes, so it is allowed only in these
// reads, and each is asserted by name so a stale entry cannot linger.
var calendarFeedSelectorReaders = []string{
	"LoadSettingsByID",
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
// writes the column. Raw SQL is refused by where the column name is spelled,
// not by where it goes: every string constant naming the selector must be a
// column-map key, a const declaration (judged at each use), an argument of a
// gorm Where, or an argument of a gorm Select in a calendarFeedSelectorReaders
// function. Any other spelling is refused however it reaches Raw or Exec —
// through a variable, a parameter, a (string, error) return, an interface or
// a generic call. A single-column Update, a key added by index and a
// models.User field write are refused by
// TestEveryCalendarFeedSelectorWriterAlsoClearsTheLastPolledMark. A name
// assembled at run time from fragments, and SQL loaded from outside the source
// (the migration runner's files), are out of reach of any source scan;
// migrations are pinned by their own tests.
func TestEveryCalendarFeedDisarmClearsTheDetailedTitlesOptIn(t *testing.T) {
	pkg := loadTypedDBPackage(t)
	disarms, reads, problems := scanCalendarFeedTitleWrites(pkg.Fset, pkg.TypesInfo, pkg.Syntax)
	for _, problem := range problems {
		t.Error(problem)
	}
	for _, required := range calendarFeedDisarmWriters {
		if !disarms[required] {
			t.Errorf("the scan did not find %s among the calendar-feed disarms (%v): it is not measuring what it claims", required, sortedNamesOf(disarms))
		}
	}
	for _, required := range calendarFeedSelectorReaders {
		if !reads[required] {
			t.Errorf("the scan found no Select of calendar_feed_selector in %s (%v): drop it from calendarFeedSelectorReaders", required, sortedNamesOf(reads))
		}
	}
}

// TestCalendarFeedTitleScanSeesEveryDisarmForm proves the scan is not blind to
// the forms a disarm can take: a literal or constant key, a nil, typed-nil,
// expression or empty selector, a missing or true flag, an arm that omits the
// flag, raw SQL spelled inline, in a variable, as a format argument, in a
// helper's return (single or with an error), in a struct field, as a
// parameter, behind an interface or a generic, a package-level column map, a
// Select outside the named reads, and a Where or Select on a type that is not
// gorm's — while a gorm Where, a named read's Select and an arm through a
// named string type pass.
func TestCalendarFeedTitleScanSeesEveryDisarmForm(t *testing.T) {
	const gormSource = `package gorm
type DB struct{}
func (*DB) Where(any, ...any) *DB { return nil }
func (*DB) Select(any, ...any) *DB { return nil }
func (*DB) First(any, ...any) *DB { return nil }
func (*DB) Updates(any) *DB { return nil }
`
	const source = `package db
import "gorm.io/gorm"
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
var other, packageStatement = "", "UPDATE users SET calendar_feed_selector = NULL"
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
func LoadSettingsByID(db *gorm.DB) { db.Where("calendar_feed_selector = ?", "x").Select("calendar_feed_selector").First(nil) }
func whereWrite(db *gorm.DB) { db.Where("calendar_feed_selector = ?", "x").Updates(nil) }
func selectQuery(db *gorm.DB) *gorm.DB { return db.Select("calendar_feed_selector") }
type launder struct{}
func (launder) Where(statement string) { query{}.Exec(statement) }
func launderedWhere() { launder{}.Where("UPDATE users SET calendar_feed_selector = NULL") }
type FeedSelector string
func namedArm(s FeedSelector, d bool) any { return map[string]any{"calendar_feed_selector": s, "calendar_feed_detailed_titles": d} }
`
	fileSet := token.NewFileSet()
	info := &types.Info{
		Types:     map[ast.Expr]types.TypeAndValue{},
		Defs:      map[*ast.Ident]types.Object{},
		Uses:      map[*ast.Ident]types.Object{},
		Instances: map[*ast.Ident]types.Instance{},
	}
	gormFile, err := parser.ParseFile(fileSet, "gorm.go", gormSource, 0)
	if err != nil {
		t.Fatalf("parse gorm: %v", err)
	}
	gormPackage, err := (&types.Config{}).Check("gorm.io/gorm", fileSet, []*ast.File{gormFile}, nil)
	if err != nil {
		t.Fatalf("type-check gorm: %v", err)
	}
	file, err := parser.ParseFile(fileSet, "synthetic.go", source, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := (&types.Config{Importer: singlePackageImporter{gormPackage}}).Check("db", fileSet, []*ast.File{file}, info); err != nil {
		t.Fatalf("type-check: %v", err)
	}

	disarms, reads, problems := scanCalendarFeedTitleWrites(fileSet, info, []*ast.File{file})
	for _, name := range []string{"missing", "constantKey", "empty", "kept", "cleared", "typedNil", "expression"} {
		if !disarms[name] {
			t.Errorf("the scan did not classify %s as a disarm: %v", name, sortedNamesOf(disarms))
		}
	}
	if disarms["arm"] || disarms["armed"] || disarms["namedArm"] {
		t.Errorf("the scan classified an arm as a disarm: %v", sortedNamesOf(disarms))
	}
	if !reads["LoadSettingsByID"] {
		t.Errorf("the scan did not see the named read's Select: %v", sortedNamesOf(reads))
	}
	wantProblems := []string{
		"missing at", "constantKey at", "empty at", "kept at", "arm at", "typedNil at", "expression at",
		"raw at", "rawConstant at", "rawVariable at", "packageStatement at", "rawBuilt at", "disarmSQL at",
		"tupleSQL at", "newRepository at", "rawParameter at", "Statement at", "rawGeneric at",
		"packageColumns at", "selectQuery at", "launderedWhere at",
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

type singlePackageImporter struct{ pkg *types.Package }

func (importer singlePackageImporter) Import(string) (*types.Package, error) {
	return importer.pkg, nil
}

func scanCalendarFeedTitleWrites(fileSet *token.FileSet, info *types.Info, files []*ast.File) (map[string]bool, map[string]bool, []string) {
	const selectorColumn = "calendar_feed_selector"
	const titlesColumn = "calendar_feed_detailed_titles"
	readers := map[string]bool{}
	for _, name := range calendarFeedSelectorReaders {
		readers[name] = true
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
	// gormMethod names a call's method when it is one of gorm.DB's, else "".
	gormMethod := func(call *ast.CallExpr) string {
		callee, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return ""
		}
		method, ok := info.ObjectOf(callee.Sel).(*types.Func)
		if !ok || method.Pkg() == nil || method.Pkg().Path() != "gorm.io/gorm" {
			return ""
		}
		return method.Name()
	}

	disarms := map[string]bool{}
	reads := map[string]bool{}
	var problems []string
	// The places a constant naming the selector may sit; any other is refused.
	allowed := map[ast.Expr]bool{}
	for _, file := range files {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			name := function.Name.Name
			ast.Inspect(function.Body, func(node ast.Node) bool {
				if call, ok := node.(*ast.CallExpr); ok {
					if method := gormMethod(call); method == "Where" || (method == "Select" && readers[name]) {
						for _, argument := range call.Args {
							allowed[argument] = true
							if method == "Select" && columnOf(argument) == selectorColumn {
								reads[name] = true
							}
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

	// A const declaration is skipped: each use of the constant is judged instead.
	refuseStrayNames := func(name string, node ast.Node) {
		ast.Inspect(node, func(inner ast.Node) bool {
			if declaration, ok := inner.(*ast.GenDecl); ok && declaration.Tok == token.CONST {
				return false
			}
			expr, ok := inner.(ast.Expr)
			if !ok || !strings.Contains(columnOf(expr), selectorColumn) {
				return true
			}
			if !allowed[expr] {
				problems = append(problems, name+" at "+fileSet.Position(expr.Pos()).String()+": calendar_feed_selector is spelled outside a column map, a gorm Where or a named read's Select; raw SQL cannot be checked for calendar_feed_detailed_titles")
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
				if typed.Tok == token.CONST {
					continue
				}
				for _, spec := range typed.Specs {
					switch named := spec.(type) {
					case *ast.ValueSpec:
						for index, value := range named.Values {
							name := named.Names[0].Name
							if len(named.Names) == len(named.Values) {
								name = named.Names[index].Name
							}
							refuseStrayNames(name, value)
						}
					case *ast.TypeSpec:
						refuseStrayNames(named.Name.Name, named)
					}
				}
			}
		}
	}
	return disarms, reads, problems
}
