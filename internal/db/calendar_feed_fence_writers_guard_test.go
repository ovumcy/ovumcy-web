package db

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"sort"
	"strings"
	"sync"
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
	"DisarmAllCalendarFeedTokens":        "the bulk disarm of the restore fence and of the key-rotation sentinel: the fence records a fresh token in both halves immediately after its own call, so advancing here would mint two tokens for one event; the sentinel's trigger is a changed SECRET_KEY, and the epoch that detects that is derived from the key rather than from stored progress, so a restore cannot roll it back into agreement",
	"DisarmCalendarFeedTokensWithoutMAC": "the key-rotation sentinel's first-boot disarm of MAC-less legacy rows: the absent-epoch boot is detected from app_state, which a restore returns together with the feed columns, so the fence's own pass covers the restore case and an advance here would mint a second token for one event",
	"BackfillCalendarFeedVerifierMAC":    "neither grants nor removes access: it fills a derived column for a token that already verified, on the read path every first poll takes",
}

// alwaysAdvancingWriters change which feeds are armed WITHOUT naming a feed
// column, so the scan cannot find them. Naming them by hand is the compromise
// this one case needs; each says why.
var alwaysAdvancingWriters = map[string]string{
	"DeleteAccountAndRelatedData":         "the erased account's feed leaves with its row, and a restore brings both back together",
	"DeleteAccountFromAuthSessionVersion": "the owner's own erasure: the same row removal, gated on the session version",
}

// calendarFeedFenceAdvanceCalls are the three spellings of the advance. They
// are one call in three forms — the error-returning one the arming and revoking
// writers use, the retrying-then-refusing one every session or credential
// writer uses, and the best-effort wrapper clear-data and erasure use — so the
// scan has to count all three or it would report writers as never advancing.
var calendarFeedFenceAdvanceCalls = map[string]bool{
	"advanceCalendarFeedFence":           true,
	"advanceCalendarFeedFenceOrRefuse":   true,
	"advanceCalendarFeedFenceBestEffort": true,
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
	// The whole package, type-checked: a writer in another file, or one that
	// names the column through a constant, is as much a writer as one spelling
	// the literal in user_repository.go. Columns are recognised by their
	// resolved constant value and by the models.User field, never by a literal
	// in one file; the advance is matched by the function it resolves to.
	pkg := loadTypedDBPackage(t)
	info := pkg.TypesInfo
	accessColumns := map[string]bool{}
	for _, column := range calendarFeedAccessColumns {
		accessColumns[strings.Trim(column, `"`)] = true
	}
	accessFields := map[types.Object]bool{}
	for _, field := range []string{"CalendarFeedSelector", "CalendarFeedVerifierHash", "CalendarFeedVerifierMAC"} {
		accessFields[userModelField(t, pkg, field)] = true
	}

	writers := map[string]bool{}
	advances := map[string]bool{}
	advancesFirst := map[string]bool{}
	txViolations := map[string][]string{}
	txChecked := map[string]int{}
	clearEnteredScopedUpdate := false
	postCommitCalled := map[string]bool{}
	var functions []*ast.FuncDecl
	for _, file := range pkg.Syntax {
		for _, declaration := range file.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok && function.Body != nil {
				functions = append(functions, function)
			}
		}
	}
	for _, function := range functions {
		name := function.Name.Name

		// Positions come from the AST, never from the body text. A doc comment
		// beside the row write that quotes the call — the natural thing to write
		// there — would move a text offset and silently flip the ordering verdict
		// below, which is the load-bearing half of this guard.
		advanceAt := -1
		var advanceCommit *ast.FuncLit
		columnAt := -1
		transactionAt := -1
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
		markColumn := func(node ast.Node) {
			if at := int(node.Pos()); columnAt < 0 || at < columnAt {
				columnAt = at
			}
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.CallExpr:
				at := int(typed.Pos())
				if callee, ok := typeutil.Callee(info, typed).(*types.Func); ok {
					if callee.Pkg() == pkg.Types && calendarFeedFenceAdvanceCalls[callee.Name()] && (advanceAt < 0 || at < advanceAt) {
						advanceAt = at
						advanceCommit = fenceWriteClosure(typed)
					}
					if callee.Name() == "Transaction" && (transactionAt < 0 || at < transactionAt) {
						transactionAt = at
					}
				}
			case *ast.KeyValueExpr:
				if key, ok := typed.Key.(*ast.Ident); ok && accessFields[info.Uses[key]] {
					markColumn(typed)
				}
			case *ast.AssignStmt:
				for _, target := range typed.Lhs {
					if selector, ok := target.(*ast.SelectorExpr); ok && accessFields[info.Uses[selector.Sel]] {
						markColumn(typed)
					}
				}
			case ast.Expr:
				// Any expression whose constant value is a feed access column: a
				// literal, a named constant, or a constant expression.
				if value := info.Types[typed].Value; value != nil && value.Kind() == constant.String &&
					accessColumns[constant.StringVal(value)] {
					markColumn(typed)
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
		// The write is the writer's first feed column or, for one that opens a
		// transaction of its own, that transaction. Since WEB-505 the write must
		// sit INSIDE the closure handed to the advance — the transaction
		// runFencedWrite opens after the fence file moved, which records the
		// marker beside it — so it runs after the file write in time and commits
		// together with the marker. A write before the call, after it, or in a
		// transaction of its own (inside the closure or not) commits apart from
		// the marker, which is the backup window.
		writeAt := columnAt
		if transactionAt >= 0 && (writeAt < 0 || transactionAt < writeAt) {
			writeAt = transactionAt
		}
		// A writer naming no feed column — the erasures, whose row delete takes
		// the feed with it — has no column to place, so only the closure and
		// the absence of a transaction of its own are asked of it.
		if advanceCommit != nil && (writeAt < 0 || insideNode(advanceCommit, writeAt)) && (transactionAt < 0 || !insideNode(advanceCommit, transactionAt)) {
			advancesFirst[name] = true
		}
		if advanceAt >= 0 {
			count, found, helpers, postCommit := fencedWriteTxViolations(pkg, function, func(call *ast.CallExpr) bool {
				callee, ok := typeutil.Callee(info, call).(*types.Func)
				return ok && callee.Pkg() == pkg.Types && calendarFeedFenceAdvanceCalls[callee.Name()]
			})
			for helper := range postCommit {
				postCommitCalled[helper] = true
			}
			if name == "ClearCalendarFeedToken" {
				_, clearEnteredScopedUpdate = helpers[pkg.Types.Scope().Lookup("scopedUserUpdateTx")]
			}
			txChecked[name] += count
			txViolations[name] = append(txViolations[name], found...)
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
	// And the handle check must have read a write of a load-bearing writer, by
	// name: a resolver that rooted nothing would report no violation anywhere.
	if txChecked["ClearCalendarFeedToken"] == 0 {
		t.Fatal("the tx-root check resolved no write of ClearCalendarFeedToken to its closure's tx parameter: it is not measuring what it claims")
	}
	// WEB-526: this guard shares the helper walk with the session guard, so it
	// owns a name check of its own — the revocation's query helper, by
	// declaration.
	if !clearEnteredScopedUpdate {
		t.Fatal("the helper walk never entered scopedUserUpdateTx from ClearCalendarFeedToken's closure: it is not measuring what it claims")
	}
	for helper := range postCommitFenceHelpers {
		if !postCommitCalled[helper] {
			t.Errorf("post-commit helper %s is no longer called outside a fenced closure: drop it from postCommitFenceHelpers", helper)
		}
	}
	for name, found := range txViolations {
		if _, exempt := exemptCalendarFeedWriters[name]; exempt || len(found) == 0 {
			continue
		}
		t.Errorf("%s must run every write through the tx its advance closure is handed: %s", name, strings.Join(found, "; "))
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

	// The ordering rule (WEB-397): every writer advances BEFORE its write. One
	// that advances after leaves a crash window in which the row is revoked and
	// the fence is not, and a restore of a backup taken before the revocation
	// then compares equal and revives the feed.
	mustAdvanceFirst := make(map[string]bool, len(writers)+len(alwaysAdvancingWriters))
	for name := range writers {
		mustAdvanceFirst[name] = true
	}
	for name := range alwaysAdvancingWriters {
		mustAdvanceFirst[name] = true
	}
	ordered := sortedNamesOf(mustAdvanceFirst)
	for _, name := range ordered {
		if _, exempt := exemptCalendarFeedWriters[name]; exempt {
			continue
		}
		if !advancesFirst[name] {
			t.Fatalf("%s must advance the fence BEFORE its write and run that write inside the closure it hands the advance, through its tx: a write ahead of the file leaves the feed revoked and the fence not, and one committed apart from the marker leaves a snapshot holding the new marker beside the old row, which a restore then reads as continuity", name)
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

// fenceWriteClosure is the write closure an advance call hands its helper —
// its last argument, when that is a function literal — or nil.
func fenceWriteClosure(call *ast.CallExpr) *ast.FuncLit {
	if len(call.Args) == 0 {
		return nil
	}
	closure, _ := call.Args[len(call.Args)-1].(*ast.FuncLit)
	return closure
}

// insideNode reports whether at lies within node's source span.
func insideNode(node ast.Node, at int) bool {
	return at > int(node.Pos()) && at < int(node.End())
}

// isGormDBHandle reports whether expr is a *gorm.DB: the handle a write goes
// through.
func isGormDBHandle(info *types.Info, expr ast.Expr) bool {
	kind := info.TypeOf(expr)
	return kind != nil && types.TypeString(kind, nil) == "*gorm.io/gorm.DB"
}

// handleRoots resolves the objects a *gorm.DB expression is built from: the
// receiver chain of a method call, the handle argument of a helper that returns
// a query (scopedUserUpdateTx), and every assignment a local variable took.
// The roots are objects, never names, so a shadowing `tx` or a copy of the
// repository's own handle does not read as the closure's transaction.
func handleRoots(info *types.Info, assigned map[types.Object][]ast.Expr, expr ast.Expr, seen map[types.Object]bool) []types.Object {
	switch typed := expr.(type) {
	case *ast.ParenExpr:
		return handleRoots(info, assigned, typed.X, seen)
	case *ast.UnaryExpr:
		return handleRoots(info, assigned, typed.X, seen)
	case *ast.StarExpr:
		return handleRoots(info, assigned, typed.X, seen)
	case *ast.SelectorExpr:
		return handleRoots(info, assigned, typed.X, seen)
	case *ast.Ident:
		object := info.Uses[typed]
		if object == nil {
			object = info.Defs[typed]
		}
		if object == nil {
			return nil
		}
		sources := assigned[object]
		if len(sources) == 0 || seen[object] {
			return []types.Object{object}
		}
		seen[object] = true
		var roots []types.Object
		for _, source := range sources {
			roots = append(roots, handleRoots(info, assigned, source, seen)...)
		}
		delete(seen, object)
		return roots
	case *ast.CallExpr:
		if selector, ok := typed.Fun.(*ast.SelectorExpr); ok && info.Selections[selector] != nil {
			return handleRoots(info, assigned, selector.X, seen)
		}
		for _, argument := range typed.Args {
			if isGormDBHandle(info, argument) {
				return handleRoots(info, assigned, argument, seen)
			}
		}
	}
	return nil
}

// fencedWriteTxViolations checks EVERY write site of a fenced writer, not the
// first: each gorm write call, and each call of a db helper handed a *gorm.DB,
// must sit inside the closure given to an advance and be rooted at that
// closure's own tx parameter object. A second write after the closure returns,
// a write through the repository's own handle inside it, and a helper handed
// that handle all name a root other than tx. It returns the sites it found
// rooted at tx, so a caller can assert a load-bearing writer was really read.
//
// Every same-package helper called inside the closure is walked by
// declaration (fenceHelperWalk); helpers maps each helper entered to the
// tx-rooted writes found in its own body, so a caller can assert a
// load-bearing helper was really read.
func fencedWriteTxViolations(pkg *packages.Package, function *ast.FuncDecl, isAdvance func(*ast.CallExpr) bool) (checked int, violations []string, helpers map[types.Object]int, postCommit map[string]bool) {
	info := pkg.TypesInfo
	var closures []*ast.FuncLit
	assigned := assignmentsIn(info, function.Body)
	walk := newFenceHelperWalk(pkg, isAdvance)
	ast.Inspect(function.Body, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok && isAdvance(call) {
			if closure := fenceWriteClosure(call); closure != nil {
				closures = append(closures, closure)
			}
		}
		return true
	})

	ast.Inspect(function.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || isAdvance(call) {
			return true
		}
		callee, ok := typeutil.Callee(info, call).(*types.Func)
		if !ok || callee.Pkg() == nil {
			return true
		}
		var handles []ast.Expr
		switch {
		case persistenceWriteMethods[callee.Pkg().Path()][callee.Name()]:
			// A database/sql handle never roots at the closure's *gorm.DB tx,
			// so a raw Exec anywhere in a fenced writer is a violation.
			if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
				handles = append(handles, selector.X)
			}
		case callee.Pkg() == pkg.Types:
			for _, argument := range call.Args {
				if isGormDBHandle(info, argument) {
					handles = append(handles, argument)
				}
			}
		}
		var enclosing *ast.FuncLit
		for _, closure := range closures {
			if insideNode(closure, int(call.Pos())) && (enclosing == nil || closure.Pos() > enclosing.Pos()) {
				enclosing = closure
			}
		}
		var txObject types.Object
		if enclosing != nil {
			if params := enclosing.Type.Params.List; len(params) > 0 && len(params[0].Names) > 0 {
				txObject = info.Defs[params[0].Names[0]]
			}
		}
		// WEB-517: a same-package helper called inside the closure is walked,
		// whether or not it is handed a handle — one given none must not write
		// through the repository's own *gorm.DB, and one given tx must write
		// through that parameter and nothing else. WEB-526: one called outside
		// every closure is walked with nothing bound, so any write it makes is
		// one committed apart from the marker.
		if reason := postCommitFenceHelpers[callee.FullName()]; enclosing == nil && callee.Pkg() == pkg.Types && reason != "" {
			walk.postCommit[callee.FullName()] = true
		} else if callee.Pkg() == pkg.Types {
			bound := map[types.Object]bool{}
			if enclosing != nil && txObject != nil {
				bound[txObject] = true
			}
			if enclosing == nil || txObject != nil {
				helperChecked, helperViolations := walk.enter(info, assigned, bound, call, callee, 0)
				checked += helperChecked
				violations = append(violations, helperViolations...)
			}
		}
		if len(handles) == 0 {
			return true
		}
		where := pkg.Fset.Position(call.Pos())
		if enclosing == nil {
			violations = append(violations, fmt.Sprintf("%s: %s writes outside the closure handed to the advance, so it commits apart from the marker", where, callee.Name()))
			return true
		}
		for _, handle := range handles {
			roots := handleRoots(info, assigned, handle, map[types.Object]bool{})
			rooted := txObject != nil && len(roots) > 0
			for _, root := range roots {
				rooted = rooted && root == txObject
			}
			if !rooted {
				violations = append(violations, fmt.Sprintf("%s: %s goes through a handle that is not the closure's tx parameter, so it commits apart from the marker", where, callee.Name()))
				continue
			}
			checked++
		}
		return true
	})
	return checked, violations, walk.helpers, walk.postCommit
}

// assignmentsIn maps each local object in body to every expression it was
// assigned, so handleRoots can follow a query variable back to its handle.
func assignmentsIn(info *types.Info, body ast.Node) map[types.Object][]ast.Expr {
	assigned := map[types.Object][]ast.Expr{}
	record := func(target ast.Expr, source ast.Expr) {
		if ident, ok := target.(*ast.Ident); ok {
			object := info.Defs[ident]
			if object == nil {
				object = info.Uses[ident]
			}
			if object != nil {
				assigned[object] = append(assigned[object], source)
			}
		}
	}
	ast.Inspect(body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.AssignStmt:
			for index, target := range typed.Lhs {
				switch {
				case len(typed.Lhs) == len(typed.Rhs):
					record(target, typed.Rhs[index])
				case len(typed.Rhs) == 1:
					record(target, typed.Rhs[0])
				}
			}
		case *ast.ValueSpec:
			for index, name := range typed.Names {
				switch {
				case len(typed.Names) == len(typed.Values):
					record(name, typed.Values[index])
				case len(typed.Values) == 1:
					record(name, typed.Values[0])
				}
			}
		}
		return true
	})
	return assigned
}

// fenceHelperDepth bounds the interprocedural walk: a helper chain deeper
// than this is a violation, never a silent stop.
const fenceHelperDepth = 8

// fenceHelperWalk follows a fenced closure into the same-package helpers it
// calls, resolved by declaration (WEB-517). A helper's parameters are bound to
// the closure's tx when the argument passed for them roots at an already-bound
// object; inside the helper every write must root at a bound parameter, so a
// helper given no handle may not write at all — the repository's own *gorm.DB
// (repo.database) roots at the receiver, which is never bound.
//
// A helper's own call that is an advance fences its own writes and is not
// descended: that helper is checked where it is itself a writer. A func
// literal handed to a call on a bound handle (tx.Transaction(func(inner
// *gorm.DB) …)) has its *gorm.DB parameter bound too: gorm runs it as a
// savepoint of the same transaction.
type fenceHelperWalk struct {
	pkg       *packages.Package
	isAdvance func(*ast.CallExpr) bool
	decls     map[*types.Func]*ast.FuncDecl
	visited   map[string]bool
	helpers   map[types.Object]int
	// postCommit records the postCommitFenceHelpers a fenced writer called
	// outside its closure, so a stale entry can be refused.
	postCommit map[string]bool
}

// postCommitFenceHelpers write after a fenced writer's transaction has
// committed, on purpose, and outside every closure; each says why that write
// revokes and grants nothing a restore could undo.
var postCommitFenceHelpers = map[string]string{
	"(*github.com/ovumcy/ovumcy-web/internal/db.UserRepository).purgeExpiredOIDCLogoutStatesBestEffort": "erasure housekeeping: it deletes only globally EXPIRED logout states, which no restore can make valid again, and it must stay out of the erasure transaction because a failed statement there poisons it on Postgres",
}

func newFenceHelperWalk(pkg *packages.Package, isAdvance func(*ast.CallExpr) bool) *fenceHelperWalk {
	decls := map[*types.Func]*ast.FuncDecl{}
	for _, file := range pkg.Syntax {
		for _, declaration := range file.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok && function.Body != nil {
				if object, ok := pkg.TypesInfo.Defs[function.Name].(*types.Func); ok {
					decls[object] = function
				}
			}
		}
	}
	return &fenceHelperWalk{pkg: pkg, isAdvance: isAdvance, decls: decls, visited: map[string]bool{}, helpers: map[types.Object]int{}, postCommit: map[string]bool{}}
}

// rootedIn reports whether every root of handle is a bound object, and that
// there is at least one.
func rootedIn(info *types.Info, assigned map[types.Object][]ast.Expr, bound map[types.Object]bool, handle ast.Expr) bool {
	roots := handleRoots(info, assigned, handle, map[types.Object]bool{})
	for _, root := range roots {
		if !bound[root] {
			return false
		}
	}
	return len(roots) > 0
}

// enter walks callee as called by call, from a scope whose assignments and
// tx-bound objects are given. It returns the tx-rooted writes it read.
func (walk *fenceHelperWalk) enter(info *types.Info, assigned map[types.Object][]ast.Expr, bound map[types.Object]bool, call *ast.CallExpr, callee *types.Func, depth int) (checked int, violations []string) {
	where := walk.pkg.Fset.Position(call.Pos())
	decl := walk.decls[callee.Origin()]
	if decl == nil {
		return 0, []string{fmt.Sprintf("%s: helper %s has no body in package db to walk, so its writes cannot be placed", where, callee.FullName())}
	}
	if depth >= fenceHelperDepth {
		return 0, []string{fmt.Sprintf("%s: helper chain through %s exceeds %d calls, so its writes were not placed", where, callee.FullName(), fenceHelperDepth)}
	}
	var params []*ast.Ident
	for _, field := range decl.Type.Params.List {
		params = append(params, field.Names...)
		if len(field.Names) == 0 {
			params = append(params, nil)
		}
	}
	inner := map[types.Object]bool{}
	key := callee.FullName() + ":"
	for index, argument := range call.Args {
		if index >= len(params) || params[index] == nil || !isGormDBHandle(info, argument) {
			continue
		}
		if !rootedIn(info, assigned, bound, argument) {
			violations = append(violations, fmt.Sprintf("%s: %s is handed a handle that is not the closure's tx, so it commits apart from the marker", where, callee.Name()))
			continue
		}
		inner[info.Defs[params[index]]] = true
		key += fmt.Sprintf("%d,", index)
	}
	if _, seen := walk.helpers[callee.Origin()]; !seen {
		walk.helpers[callee.Origin()] = 0
	}
	if walk.visited[key] {
		return 0, violations
	}
	walk.visited[key] = true

	helperAssigned := assignmentsIn(info, decl.Body)
	ast.Inspect(decl.Body, func(node ast.Node) bool {
		inside, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if walk.isAdvance(inside) {
			return false
		}
		if selector, ok := inside.Fun.(*ast.SelectorExpr); ok && isGormDBHandle(info, selector.X) && rootedIn(info, helperAssigned, inner, selector.X) {
			for _, argument := range inside.Args {
				literal, ok := argument.(*ast.FuncLit)
				if !ok {
					continue
				}
				for _, field := range literal.Type.Params.List {
					for _, name := range field.Names {
						if object := info.Defs[name]; object != nil && isGormDBHandle(info, name) {
							inner[object] = true
						}
					}
				}
			}
		}
		at := walk.pkg.Fset.Position(inside.Pos())
		switch resolved := typeutil.Callee(info, inside).(type) {
		case *types.Func:
			if resolved.Pkg() == nil {
				return true
			}
			if persistenceWriteMethods[resolved.Pkg().Path()][resolved.Name()] {
				selector, ok := inside.Fun.(*ast.SelectorExpr)
				if !ok || !rootedIn(info, helperAssigned, inner, selector.X) {
					violations = append(violations, fmt.Sprintf("%s: %s inside helper %s goes through a handle that is not the closure's tx, so it commits apart from the marker", at, resolved.Name(), callee.Name()))
					return true
				}
				checked++
				walk.helpers[callee.Origin()]++
				return true
			}
			if resolved.Pkg() == walk.pkg.Types {
				innerChecked, innerViolations := walk.enter(info, helperAssigned, inner, inside, resolved, depth+1)
				checked += innerChecked
				violations = append(violations, innerViolations...)
			}
		case nil:
			if tv, ok := info.Types[inside.Fun]; ok && tv.IsType() {
				return true
			}
			violations = append(violations, fmt.Sprintf("%s: helper %s makes a call that does not resolve to a declaration, so its writes cannot be placed", at, callee.Name()))
		}
		return true
	})
	return checked, violations
}

// TestTheFencedWriteRecordsTheMarkerInsideTheWritersTransaction is the
// load-bearing half of WEB-505 the two writer guards rely on: they prove each
// writer's statements run inside the closure handed to the advance, and this
// proves that closure runs in ONE transaction with the marker. runFencedWrite
// must call setAppStateTx and the write closure inside the closure it hands
// gorm's Transaction, resolved by declaration.
func TestTheFencedWriteRecordsTheMarkerInsideTheWritersTransaction(t *testing.T) {
	pkg := loadTypedDBPackage(t)
	setMarker := pkg.Types.Scope().Lookup("setAppStateTx")
	builder := pkg.Types.Scope().Lookup("runFencedWrite")
	if setMarker == nil || builder == nil {
		t.Fatal("setAppStateTx or runFencedWrite is not declared in package db: the guard resolves nothing")
	}
	found := false
	for _, file := range pkg.Syntax {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil || pkg.TypesInfo.Defs[function.Name] != builder {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				callee, ok := typeutil.Callee(pkg.TypesInfo, call).(*types.Func)
				if !ok || callee.Name() != "Transaction" || callee.Pkg() == nil || callee.Pkg().Path() != "gorm.io/gorm" {
					return true
				}
				transaction := fenceWriteClosure(call)
				if transaction == nil || len(transaction.Type.Params.List) == 0 || len(transaction.Type.Params.List[0].Names) == 0 {
					return true
				}
				// write is the builder's parameter and tx the closure's, both
				// resolved by declaration: a local called write, or a handle
				// other than the transaction's own, is neither.
				txObject := pkg.TypesInfo.Defs[transaction.Type.Params.List[0].Names[0]]
				var writeParam types.Object
				for _, field := range function.Type.Params.List {
					for _, name := range field.Names {
						if name.Name == "write" {
							writeParam = pkg.TypesInfo.Defs[name]
						}
					}
				}
				if writeParam == nil || txObject == nil {
					t.Fatal("runFencedWrite declares no write parameter or its transaction closure no tx: the guard resolves nothing")
				}
				handedTx := func(call *ast.CallExpr) bool {
					if len(call.Args) == 0 {
						return false
					}
					ident, ok := call.Args[0].(*ast.Ident)
					return ok && pkg.TypesInfo.Uses[ident] == txObject
				}
				marker, write := false, false
				ast.Inspect(transaction.Body, func(inner ast.Node) bool {
					if innerCall, ok := inner.(*ast.CallExpr); ok {
						if typeutil.Callee(pkg.TypesInfo, innerCall) == setMarker && handedTx(innerCall) {
							marker = true
						}
						if ident, ok := innerCall.Fun.(*ast.Ident); ok && pkg.TypesInfo.Uses[ident] == writeParam && handedTx(innerCall) {
							write = true
						}
					}
					return true
				})
				found = found || (marker && write)
				return true
			})
		}
	}
	if !found {
		t.Fatal("runFencedWrite must record the marker (setAppStateTx) and run the writer's write inside ONE gorm Transaction closure: a marker committed apart from the row is the backup window a live restore reads as continuity")
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
	loaded, err := loadTypedDBPackageOnce()
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

// loadTypedDBPackageOnce shares one type-check between the guards that read it;
// they only inspect the result.
var loadTypedDBPackageOnce = sync.OnceValues(func() ([]*packages.Package, error) {
	config := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
		Tests: false,
	}
	return packages.Load(config, ".")
})

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
