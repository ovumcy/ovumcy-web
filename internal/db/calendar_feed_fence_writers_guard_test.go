package db

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"sort"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
)

// calendarFeedAccessColumns are the three columns that decide whether a
// subscribe URL resolves. calendar_feed_revealed_at is deliberately absent: it
// records that a URL was shown once, not that one works, so a function that
// only touches it changes no access.
var calendarFeedAccessColumns = []string{
	`"calendar_feed_selector"`,
	`"calendar_feed_verifier_hash"`,
	`"calendar_feed_verifier_mac"`,
}

// exemptCalendarFeedWriters are the functions that write one of those columns
// and deliberately do NOT advance the fence. Each carries the reason, because an
// exemption without one is how this guard would quietly become an allowlist for
// whatever was easiest to skip.
var exemptCalendarFeedWriters = map[string]string{
	"DisarmAllCalendarFeedTokens":        "the restore fence's own bulk disarm: the fence records a fresh token in both halves immediately after it, so advancing here would mint two tokens for one event",
	"DisarmCalendarFeedTokensWithoutMAC": "the key-rotation sentinel's bulk disarm: its trigger is a changed SECRET_KEY, and the epoch that detects that is derived from the key rather than from stored progress, so a restore cannot roll it back into agreement",
	"BackfillCalendarFeedVerifierMAC":    "neither grants nor removes access: it fills a derived column for a token that already verified, on the read path every first poll takes",
}

// alwaysAdvancingWriters change which feeds are armed WITHOUT naming a feed
// column, so the scan cannot find them. Naming them by hand is the compromise
// this one case needs; each says why.
var alwaysAdvancingWriters = map[string]string{
	"DeleteAccountAndRelatedData": "the erased account's feed leaves with its row, and a restore brings both back together",
}

// calendarFeedFenceAdvanceCalls are the two spellings of the advance. They are
// one call in two forms — the error-returning one, which the writers that must
// advance BEFORE their row write use, and the best-effort wrapper that drops it,
// which every writer that advances after uses — so the scan has to count both or
// it would report five writers as never advancing at all.
var calendarFeedFenceAdvanceCalls = map[string]bool{
	"advanceCalendarFeedFence":           true,
	"advanceCalendarFeedFenceBestEffort": true,
}

// advanceBeforeTheWrite are the writers whose advance must precede their row
// write, because there the write IS the revocation and a fence left behind it
// is the window a restore reopens. Every other writer advances after, so that a
// failed fence write cannot refuse an unrelated credential rotation or erasure.
// The split is the load-bearing part of the ordering rule, so it is asserted
// rather than left to the comment on advanceCalendarFeedFence.
var advanceBeforeTheWrite = map[string]bool{
	"SaveCalendarFeedToken":  true,
	"ClearCalendarFeedToken": true,
}

// TestEveryCalendarFeedWriterAdvancesTheRestoreFence is the completeness guard
// the fence depends on. The fence can only see a restore if every change to the
// armed-feed set was recorded outside the database; one writer that skips the
// advance leaves exactly one route by which a revoked subscribe URL returns from
// a backup, and nothing else in the suite would notice.
//
// The set is derived from the file rather than re-listed: a hand-written mirror
// of the writers would agree with itself while a new writer went unguarded.
func TestEveryCalendarFeedWriterAdvancesTheRestoreFence(t *testing.T) {
	const path = "user_repository.go"

	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, path, source, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	writers := map[string]bool{}
	advances := map[string]bool{}
	advancesFirst := map[string]bool{}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}
		name := function.Name.Name

		// Positions come from the AST, never from the body text. A doc comment
		// beside the row write that quotes the call — the natural thing to write
		// there — would move a text offset and silently flip the ordering verdict
		// below, which is the load-bearing half of this guard.
		advanceAt := -1
		columnAt := -1
		// A function that issues no write statement cannot be a writer, however
		// many feed columns it names. The settings projection names
		// calendar_feed_selector in its Select list because the egress ledger has
		// to read it, and a name-only scan reported that READ as a write that
		// forgot to advance the fence. The verb test is what separates the two,
		// and it stays inside the scan rather than becoming an exemption: an
		// exemption without a reason is how this guard would quietly turn into an
		// allowlist for whatever was easiest to skip. A Select naming the columns
		// a following Updates writes is still caught, because that function does
		// issue a write.
		if !functionIssuesAWrite(function) {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.CallExpr:
				if selector, ok := typed.Fun.(*ast.SelectorExpr); ok && calendarFeedFenceAdvanceCalls[selector.Sel.Name] {
					if at := int(typed.Pos()); advanceAt < 0 || at < advanceAt {
						advanceAt = at
					}
				}
			case *ast.BasicLit:
				if typed.Kind != token.STRING {
					return true
				}
				for _, column := range calendarFeedAccessColumns {
					if typed.Value != column {
						continue
					}
					if at := int(typed.Pos()); columnAt < 0 || at < columnAt {
						columnAt = at
					}
				}
			}
			return true
		})

		if advanceAt >= 0 {
			advances[name] = true
		}
		if columnAt >= 0 {
			writers[name] = true
		}
		if advanceAt >= 0 && columnAt >= 0 && advanceAt < columnAt {
			advancesFirst[name] = true
		}
	}

	// Anti-vacuity, on two names this test owns: arming and revoking must both
	// be found, or the scan is looking at the wrong thing and would report
	// success over an empty set.
	for _, required := range []string{"SaveCalendarFeedToken", "ClearCalendarFeedToken"} {
		if !writers[required] {
			t.Fatalf("the scan did not find %s among the feed writers (%v): it is not measuring what it claims", required, sortedNamesOf(writers))
		}
	}

	var missing []string
	for name := range writers {
		if _, exempt := exemptCalendarFeedWriters[name]; exempt {
			continue
		}
		if !advances[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("these writes change which calendar feeds are armed but never record it outside the database, so a backup restore would undo them silently: %v", missing)
	}

	for name, reason := range alwaysAdvancingWriters {
		if !advances[name] {
			t.Fatalf("%s must advance the restore fence (%s)", name, reason)
		}
	}

	// The ordering rule, both ways. A revocation path that advances after its
	// row write reopens the window a restore crosses; any other path that
	// advances before it lets a failed fence write refuse a credential rotation
	// or an erasure that has nothing to do with calendar feeds.
	for name := range writers {
		if _, exempt := exemptCalendarFeedWriters[name]; exempt {
			continue
		}
		switch {
		case advanceBeforeTheWrite[name] && !advancesFirst[name]:
			t.Fatalf("%s is a revocation path: it must advance the fence BEFORE its row write, or a crash between the two leaves the row revoked and the fence not, which a restore then undoes", name)
		case !advanceBeforeTheWrite[name] && advancesFirst[name]:
			t.Fatalf("%s clears the feed only as a side effect: advancing before its row write would refuse the whole operation whenever the fence write fails", name)
		}
	}
	for name := range advanceBeforeTheWrite {
		if !writers[name] {
			t.Fatalf("%s no longer writes a feed access column: the ordering entry names a function this rule can no longer be about", name)
		}
	}

	// A stale exemption is an exemption nobody can see is stale — and an
	// exemption that quietly started advancing is worse, because the two boot
	// disarms run INSIDE the fence's own pass: an advance there would write a
	// token in the middle of Enforce, between the disarm and the token Enforce
	// is about to record. The exemption is a rule in both directions.
	for name, reason := range exemptCalendarFeedWriters {
		if !writers[name] {
			t.Fatalf("exempt writer %s no longer writes a feed access column: drop the exemption rather than leaving it standing", name)
		}
		if advances[name] {
			t.Fatalf("%s advances the restore fence although it is exempt (%s): remove the advance or the exemption, not neither", name, reason)
		}
	}
}

// TestEveryCalendarFeedSelectorWriterAlsoClearsTheLastPolledMark is WEB-46's
// completeness guard. The mark is a fact about the token the selector names,
// so every write of calendar_feed_selector must null
// calendar_feed_last_polled_on in the same statement, or a stale "last
// checked" date outlives the link it was about.
//
// Columns are resolved by the type checker over every non-test file of this
// package, not matched as string literals in one file: a map key spelled as a
// named constant is the same column, and a single-column Update, raw SQL in
// Exec, a models.User field write, or a key added to a map by index all write
// the selector with no literal "calendar_feed_selector" map key to match. None
// of those four can null the mark in the same statement, so they are refused
// outright. The behavior of each writer this guard finds is exercised one
// subtest per site in user_repository_calendar_feed_last_polled_test.go.
func TestEveryCalendarFeedSelectorWriterAlsoClearsTheLastPolledMark(t *testing.T) {
	const selectorColumn = "calendar_feed_selector"
	const markColumn = "calendar_feed_last_polled_on"

	pkg := loadTypedDBPackage(t)
	info := pkg.TypesInfo
	selectorField := userModelField(t, pkg, "CalendarFeedSelector")
	columnOf := func(expr ast.Expr) string {
		value := info.Types[expr].Value
		if value == nil || value.Kind() != constant.String {
			return ""
		}
		return constant.StringVal(value)
	}
	isMap := func(expr ast.Expr) bool {
		kind := info.TypeOf(expr)
		if kind == nil {
			return false
		}
		_, ok := kind.Underlying().(*types.Map)
		return ok
	}

	clearingWriters := map[string]bool{}
	var violations []string
	for _, file := range pkg.Syntax {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			name := function.Name.Name
			refuse := func(node ast.Node, what string) {
				violations = append(violations, fmt.Sprintf("%s at %s: %s", name, pkg.Fset.Position(node.Pos()), what))
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				switch typed := node.(type) {
				case *ast.CompositeLit:
					if !isMap(typed) {
						return true
					}
					writesSelector, nullsMark := false, false
					for _, element := range typed.Elts {
						keyValue, ok := element.(*ast.KeyValueExpr)
						if !ok {
							continue
						}
						switch columnOf(keyValue.Key) {
						case selectorColumn:
							writesSelector = true
						case markColumn:
							nullsMark = info.Types[keyValue.Value].IsNil()
						}
					}
					switch {
					case writesSelector && nullsMark:
						clearingWriters[name] = true
					case writesSelector:
						refuse(typed, "a column map writes calendar_feed_selector without nulling calendar_feed_last_polled_on")
					}
				case *ast.AssignStmt:
					for _, target := range typed.Lhs {
						switch lhs := target.(type) {
						case *ast.IndexExpr:
							if isMap(lhs.X) && columnOf(lhs.Index) == selectorColumn {
								refuse(typed, "calendar_feed_selector is added to a column map by index; write it in one map literal beside the mark")
							}
						case *ast.SelectorExpr:
							if info.Uses[lhs.Sel] == selectorField {
								refuse(typed, "models.User.CalendarFeedSelector is assigned; a struct write cannot null the mark in the same statement")
							}
						}
					}
				case *ast.KeyValueExpr:
					if key, ok := typed.Key.(*ast.Ident); ok && info.Uses[key] == selectorField {
						refuse(typed, "a models.User literal sets CalendarFeedSelector; a struct write cannot null the mark in the same statement")
					}
				case *ast.CallExpr:
					callee, ok := typeutil.Callee(info, typed).(*types.Func)
					if !ok || callee.Pkg() == nil || callee.Pkg().Path() != "gorm.io/gorm" || len(typed.Args) == 0 {
						return true
					}
					switch callee.Name() {
					case "Update", "UpdateColumn":
						if columnOf(typed.Args[0]) == selectorColumn {
							refuse(typed, callee.Name()+" writes calendar_feed_selector alone, leaving the mark standing")
						}
					case "Exec":
						if strings.Contains(columnOf(typed.Args[0]), selectorColumn) {
							refuse(typed, "raw SQL names calendar_feed_selector; write it through a column map beside the mark")
						}
					}
				}
				return true
			})
		}
	}

	sort.Strings(violations)
	for _, violation := range violations {
		t.Error(violation)
	}
	// Anti-vacuity, by site name: arming and revoking must both be found among
	// the writers that null the mark, or the resolution is looking at the wrong
	// thing and would report success over an empty set.
	for _, required := range []string{"SaveCalendarFeedToken", "ClearCalendarFeedToken"} {
		if !clearingWriters[required] {
			t.Errorf("the scan did not find %s among the calendar_feed_selector writers that null the mark (%v): it is not measuring what it claims", required, sortedNamesOf(clearingWriters))
		}
	}
}

// loadTypedDBPackage type-checks this package's non-test sources. It fails
// closed: a package that does not type-check resolves no column, and a guard
// that resolves nothing passes over everything.
func loadTypedDBPackage(t *testing.T) *packages.Package {
	t.Helper()
	config := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
		Tests: false,
	}
	loaded, err := packages.Load(config, ".")
	if err != nil {
		t.Fatalf("type-check the db package: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected one package, loaded %d", len(loaded))
	}
	for _, packageError := range loaded[0].Errors {
		t.Fatalf("the db package does not type-check: %v", packageError)
	}
	return loaded[0]
}

// userModelField resolves a models.User field to its declaration, so a use is
// matched by object identity rather than by spelling.
func userModelField(t *testing.T, pkg *packages.Package, field string) types.Object {
	t.Helper()
	models, ok := pkg.Imports["github.com/ovumcy/ovumcy-web/internal/models"]
	if !ok {
		t.Fatal("the db package no longer imports internal/models")
	}
	user := models.Types.Scope().Lookup("User")
	if user == nil {
		t.Fatal("internal/models declares no User")
	}
	structure, ok := user.Type().Underlying().(*types.Struct)
	if !ok {
		t.Fatal("models.User is not a struct")
	}
	for index := range structure.NumFields() {
		if candidate := structure.Field(index); candidate.Name() == field {
			return candidate
		}
	}
	t.Fatalf("models.User has no field %s", field)
	return nil
}

func sortedNamesOf(set map[string]bool) []string {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// gormWriteVerbs are the calls through which this repository actually writes a
// row. The list is deliberately generous: a false positive here only makes the
// guard look at a function it would have looked at before, while a false
// negative would hide a real writer.
var gormWriteVerbs = map[string]bool{
	"Updates":       true,
	"Update":        true,
	"UpdateColumn":  true,
	"UpdateColumns": true,
	"Save":          true,
	"Create":        true,
	"Delete":        true,
	"Exec":          true,
}

// functionIssuesAWrite reports whether the body reaches any statement that can
// change a row.
func functionIssuesAWrite(function *ast.FuncDecl) bool {
	writes := false
	ast.Inspect(function.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && gormWriteVerbs[selector.Sel.Name] {
			writes = true
			return false
		}
		return true
	})
	return writes
}
