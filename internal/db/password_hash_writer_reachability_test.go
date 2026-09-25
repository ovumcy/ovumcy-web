package db

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/packages"
)

// rehashPasswordIfStale (internal/services/auth_service.go) lets a login that
// loses the opportunistic bcrypt-cost upgrade race succeed anyway, on the
// premise that the session minted from the stale read dies regardless —
// "because every other password_hash writer bumps auth_session_version". That
// premise was prose only: nothing stopped a new *UserRepository method, or a
// UserRepository.UpdateByID(...) caller reached through one of its several
// narrow per-service interfaces, from writing password_hash without bumping
// the version alongside it.
//
// This file resolves both halves by declaration, modelled on
// declaration_reachability_barrier_test.go's own two-tier approach:
//
//   - TestEveryPasswordHashWriterBumpsAuthSessionVersionExceptTheNamedException
//     finds every *UserRepository method that writes users.password_hash — by
//     resolving the method's receiver type through go/types, never by the
//     method's name or file position — and requires each one to also carry a
//     valid auth_session_version bump in the SAME Updates() map literal,
//     unless it is named in passwordHashWriterExceptions.
//   - TestNoUpdateByIDCallerPassesPasswordHashWithoutABump resolves every call
//     to the UpdateByID contract (matched by SIGNATURE IDENTITY against the
//     concrete method — receiver ignored, so every one of the several narrow
//     per-service interfaces that separately redeclare UpdateByID(ctx, userID,
//     updates) still matches), traces the literal string keys the "updates"
//     argument can carry — through a local variable's index assignments when
//     it is not a composite literal at the call site — and refuses a call
//     whose key set could contain "password_hash" without "auth_session_version",
//     or whose key set this sweep cannot resolve at all: an unresolvable
//     argument is not proof of absence.
//
// A SQL column name has no Go declaration to resolve, so the leaf comparisons
// below are necessarily string matches against literal keys — that is not the
// "shape of a node" pattern this repository's guides warn against; it is the
// only kind of evidence a column name is. What IS resolved by declaration is
// which Go method a call reaches (a receiver type, a signature) rather than
// by matching the call's spelling.

// passwordHashWriterExceptions is the declared, reasoned exception list for
// TestEveryPasswordHashWriterBumpsAuthSessionVersionExceptTheNamedException.
// It is checked in both directions: a method here that the sweep no longer
// finds writing password_hash is a stale entry claiming something untrue.
var passwordHashWriterExceptions = map[string]string{
	"UpgradePasswordHashCAS": "the opportunistic bcrypt-cost rehash on login: same password, stronger " +
		"hash, so it must NOT revoke the session that read it. Its CAS predicate (WHERE password_hash = " +
		"oldPasswordHash) is what makes skipping the bump safe — a concurrent credential write changes " +
		"password_hash first and the CAS then loses. Regression: TestRehashRace.",
}

// mustNamePasswordHashWriters is the anti-vacuity floor for the writer sweep:
// a run that found none of these five would be reading the wrong receiver or
// the wrong package, not reporting a clean tree. Named, not counted — the
// class this file exists to catch is exactly "a member missing from a name
// list", so the list itself has to be checked by name.
var mustNamePasswordHashWriters = []string{
	"UpdatePasswordAndRevokeSessions",
	"ForceResetPasswordAndRevokeSessions",
	"UpdatePasswordRecoveryCodeAndRevokeSessions",
	"UpdatePasswordRecoveryCodeAndRevokeSessionsCAS",
	"UpgradePasswordHashCAS",
}

const (
	passwordHashWriterModulePath = "github.com/ovumcy/ovumcy-web"
	passwordHashWriterDBPackage  = passwordHashWriterModulePath + "/internal/db"
)

// TestEveryPasswordHashWriterBumpsAuthSessionVersionExceptTheNamedException is
// the direct-writer half of the barrier.
func TestEveryPasswordHashWriterBumpsAuthSessionVersionExceptTheNamedException(t *testing.T) {
	pkgs := loadPasswordHashWriterTree(t)
	userRepo := resolveUserRepositoryNamed(t, pkgs)
	dbPkg := passwordHashWriterPackageByPath(pkgs, passwordHashWriterDBPackage)
	if dbPkg == nil {
		t.Fatalf("the sweep did not load %s", passwordHashWriterDBPackage)
	}
	casHelper := resolveAuthSessionVersionCASHelper(t, dbPkg)

	writes := findPasswordHashWrites(t, dbPkg, userRepo, casHelper)
	if len(writes) == 0 {
		t.Fatalf("the sweep found no *UserRepository method writing users.password_hash; " +
			"it is reading the wrong receiver or the wrong package, and a barrier with no subject passes about nothing")
	}

	found := map[string]bool{}
	for _, write := range writes {
		found[write.method] = true
	}
	for _, name := range mustNamePasswordHashWriters {
		if !found[name] {
			t.Fatalf("the sweep did not find %s writing users.password_hash; either it moved, or this barrier stopped seeing it", name)
		}
	}
	for name := range passwordHashWriterExceptions {
		if !found[name] {
			t.Fatalf("passwordHashWriterExceptions declares %s, which the sweep no longer finds writing users.password_hash; the entry is stale and now claims something the tree does not do", name)
		}
	}

	var unbumped []string
	for _, write := range writes {
		if reason := passwordHashWriterExceptions[write.method]; reason != "" {
			continue
		}
		if write.bumps {
			continue
		}
		unbumped = append(unbumped, fmt.Sprintf("  %s\n      declared at %s", write.method, write.position))
	}
	if len(unbumped) == 0 {
		return
	}

	sort.Strings(unbumped)
	t.Fatalf("%d *UserRepository method(s) write users.password_hash without a valid auth_session_version "+
		"bump in the same update, and are not declared in passwordHashWriterExceptions:\n%s\n"+
		"A login that loses the opportunistic rehash race is safe only because every OTHER password_hash "+
		"writer revokes the session minted from the stale read. Either bump auth_session_version in the "+
		"same map literal, or add the method to passwordHashWriterExceptions with the reason it is safe not to.",
		len(unbumped), strings.Join(unbumped, "\n"))
}

// TestNoUpdateByIDCallerPassesPasswordHashWithoutABump is the pass-through
// half: UpdateByID applies whatever column map its caller builds, and never
// bumps auth_session_version itself, so a future caller handing it
// password_hash is exactly the gap the prose invariant did not guard against.
func TestNoUpdateByIDCallerPassesPasswordHashWithoutABump(t *testing.T) {
	pkgs := loadPasswordHashWriterTree(t)
	userRepo := resolveUserRepositoryNamed(t, pkgs)
	signature := resolveUpdateByIDSignature(t, userRepo)

	sites := findUpdateByIDCallSites(pkgs, signature)
	if len(sites) == 0 {
		t.Fatalf("the sweep found no call to the UpdateByID contract; it is measuring the wrong signature, " +
			"and a barrier with no subject passes about nothing")
	}

	var bad []string
	for _, site := range sites {
		if site.unresolved != "" {
			bad = append(bad, fmt.Sprintf("  %s\n      %s", site.position, site.unresolved))
			continue
		}
		if site.keys["password_hash"] && !site.keys["auth_session_version"] {
			bad = append(bad, fmt.Sprintf("  %s\n      passes \"password_hash\" without \"auth_session_version\"", site.position))
		}
	}
	if len(bad) == 0 {
		return
	}

	sort.Strings(bad)
	t.Fatalf("%d UpdateByID call site(s) this sweep cannot clear of writing password_hash without a session bump:\n%s\n"+
		"UpdateByID applies exactly the column map its caller builds and never bumps auth_session_version on its own. "+
		"A caller that needs to rewrite password_hash belongs on one of the dedicated *AndRevokeSessions methods instead, "+
		"and a caller this sweep could not resolve needs its key set made statically obvious (a literal map, or index "+
		"assignments with literal string keys) so this barrier can clear it.",
		len(bad), strings.Join(bad, "\n"))
}

// --- direct-writer sweep -----------------------------------------------

type passwordHashWrite struct {
	method   string
	position string
	bumps    bool
}

// resolveAuthSessionVersionCASHelper resolves the package-private helper that
// several writers now delegate their bump to, by declaration: a caller that
// hands the SAME map literal to this exact function object — not to a
// same-named lookalike — gets auth_session_version bumped atomically with
// whatever columns it passed, including password_hash.
func resolveAuthSessionVersionCASHelper(t *testing.T, dbPkg *packages.Package) types.Object {
	t.Helper()

	object := dbPkg.Types.Scope().Lookup("updateFromAuthSessionVersionTx")
	if object == nil {
		t.Fatalf("%s declares no updateFromAuthSessionVersionTx helper; the CAS-bump delegation this barrier checks for has moved or been renamed", passwordHashWriterDBPackage)
	}
	return object
}

func findPasswordHashWrites(t *testing.T, dbPkg *packages.Package, userRepo *types.Named, casHelper types.Object) []passwordHashWrite {
	t.Helper()

	var writes []passwordHashWrite
	for _, file := range dbPkg.Syntax {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Body == nil {
				continue
			}
			if !receiverIsNamed(dbPkg, fn, userRepo) {
				continue
			}
			sites := passwordHashWriteSitesInBody(fn.Body)
			if len(sites) == 0 {
				continue
			}
			bumps := true
			for _, site := range sites {
				if site.inlineBump {
					continue
				}
				if site.literal != nil && compositeLitDelegatesSessionBump(dbPkg, fn.Body, site.literal, casHelper) {
					continue
				}
				bumps = false
			}
			writes = append(writes, passwordHashWrite{
				method:   fn.Name.Name,
				position: passwordHashWriterPosition(t, dbPkg.Fset.Position(fn.Pos())),
				bumps:    bumps,
			})
		}
	}
	return writes
}

// compositeLitDelegatesSessionBump answers whether target is passed, as one
// of the call's arguments, to a call resolving BY DECLARATION to casHelper —
// the object identity check is what keeps this from matching a hypothetical
// unrelated function that merely happens to share the name.
func compositeLitDelegatesSessionBump(pkg *packages.Package, body *ast.BlockStmt, target *ast.CompositeLit, casHelper types.Object) bool {
	delegated := false
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok {
			return true
		}
		if pkg.TypesInfo.Uses[ident] != casHelper {
			return true
		}
		for _, arg := range call.Args {
			if arg == target {
				delegated = true
			}
		}
		return true
	})
	return delegated
}

// receiverIsNamed resolves the method's receiver type through go/types and
// compares the resolved *types.TypeName object, not the receiver's spelling —
// a type alias or a second type also called UserRepository elsewhere would
// defeat a textual match.
func receiverIsNamed(pkg *packages.Package, fn *ast.FuncDecl, subject *types.Named) bool {
	if fn.Recv == nil || len(fn.Recv.List) != 1 || len(fn.Recv.List[0].Names) != 1 {
		return false
	}
	recvName := fn.Recv.List[0].Names[0]
	recvObj, ok := pkg.TypesInfo.Defs[recvName].(*types.Var)
	if !ok || recvObj == nil {
		return false
	}
	named := namedBehindPointer(recvObj.Type())
	return named != nil && named.Obj() == subject.Obj()
}

func namedBehindPointer(t types.Type) *types.Named {
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	named, _ := t.(*types.Named)
	return named
}

// passwordHashWriteSite is one place in a method body that writes
// users.password_hash: either a gorm Updates() map literal (literal != nil)
// or the single-column Update("password_hash", v) form (literal == nil,
// which can never carry an inline bump — it takes exactly two arguments).
type passwordHashWriteSite struct {
	literal    *ast.CompositeLit
	inlineBump bool
}

// passwordHashWriteSitesInBody finds every place a method body writes
// users.password_hash and whether that SAME gorm Updates() map literal also
// carries a valid inline auth_session_version bump.
//
// This is pure AST matching with no type information, which is deliberate: it
// is tested standalone against a fixture
// (TestPasswordHashWriteDetectorRecognisesItsOwnFixtures) that never goes
// through go/packages, so the detector's own correctness is not entangled
// with the tree it judges in the barrier tests above. A bump delegated to the
// auth_session_version_cas.go helper rather than written inline is a SEPARATE
// signal, checked with type information in findPasswordHashWrites, because
// telling "this is the real helper" from "this merely shares its name" needs
// declaration resolution that a pure-AST fixture cannot exercise meaningfully.
func passwordHashWriteSitesInBody(body *ast.BlockStmt) []passwordHashWriteSite {
	var sites []passwordHashWriteSite
	ast.Inspect(body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.CallExpr:
			sel, ok := n.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Update" || len(n.Args) != 2 {
				return true
			}
			if stringLiteralOf(n.Args[0]) == "password_hash" {
				sites = append(sites, passwordHashWriteSite{literal: nil, inlineBump: false})
			}
		case *ast.CompositeLit:
			hasHash := false
			hasBump := false
			for _, elt := range n.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				switch stringLiteralOf(kv.Key) {
				case "password_hash":
					hasHash = true
				case "auth_session_version":
					if isAuthSessionVersionBumpExpr(kv.Value) {
						hasBump = true
					}
				}
			}
			if hasHash {
				sites = append(sites, passwordHashWriteSite{literal: n, inlineBump: hasBump})
			}
		}
		return true
	})
	return sites
}

// passwordHashWriteInBody is the fixture-testable summary used by
// TestPasswordHashWriteDetectorRecognisesItsOwnFixtures: found is whether any
// site was seen, bumps is whether every site seen carries an INLINE bump (the
// delegated-bump path is exercised only by findPasswordHashWrites, which has
// type information).
func passwordHashWriteInBody(body *ast.BlockStmt) (bumps bool, found bool) {
	sites := passwordHashWriteSitesInBody(body)
	if len(sites) == 0 {
		return false, false
	}
	bumps = true
	for _, site := range sites {
		if !site.inlineBump {
			bumps = false
		}
	}
	return bumps, true
}

// isAuthSessionVersionBumpExpr recognises gorm.Expr("auth_session_version + 1")
// however it is spaced, and refuses to recognise anything else — a literal
// value or a different expression would not actually increment the column.
func isAuthSessionVersionBumpExpr(value ast.Expr) bool {
	call, ok := value.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Expr" {
		return false
	}
	literal := stringLiteralOf(call.Args[0])
	return strings.ReplaceAll(literal, " ", "") == "auth_session_version+1"
}

func stringLiteralOf(expr ast.Expr) string {
	basic, ok := expr.(*ast.BasicLit)
	if !ok || basic.Kind != token.STRING {
		return ""
	}
	value, err := strconv.Unquote(basic.Value)
	if err != nil {
		return ""
	}
	return value
}

// TestPasswordHashWriteDetectorRecognisesItsOwnFixtures anchors
// passwordHashWriteInBody on inputs it owns, independent of the live tree —
// an anchor read off the tree itself stops firing the day the tree it judges
// changes shape.
func TestPasswordHashWriteDetectorRecognisesItsOwnFixtures(t *testing.T) {
	const fixture = `package fixture

func (repo *R) BumpingWrite() error {
	return repo.q().Updates(map[string]any{
		"password_hash":        "x",
		"auth_session_version": gorm.Expr("auth_session_version + 1"),
	}).Error
}

func (repo *R) NonBumpingWrite() error {
	return repo.q().Updates(map[string]any{
		"password_hash": "x",
	}).Error
}

func (repo *R) SingleColumnWrite() error {
	return repo.q().Update("password_hash", "x").Error
}

func (repo *R) UnrelatedWrite() error {
	return repo.q().Updates(map[string]any{
		"display_name": "x",
	}).Error
}

func (repo *R) MisspelledBumpIsNotABump() error {
	return repo.q().Updates(map[string]any{
		"password_hash":        "x",
		"auth_session_version": "not-an-expr",
	}).Error
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture.go", fixture, 0)
	if err != nil {
		t.Fatalf("parsing the fixture: %v", err)
	}

	results := map[string]struct {
		bumps bool
		found bool
	}{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		bumps, found := passwordHashWriteInBody(fn.Body)
		results[fn.Name.Name] = struct {
			bumps bool
			found bool
		}{bumps, found}
	}

	if r := results["BumpingWrite"]; !r.found || !r.bumps {
		t.Fatalf("BumpingWrite: got found=%v bumps=%v, want both true", r.found, r.bumps)
	}
	if r := results["NonBumpingWrite"]; !r.found || r.bumps {
		t.Fatalf("NonBumpingWrite: got found=%v bumps=%v, want found=true bumps=false", r.found, r.bumps)
	}
	if r := results["SingleColumnWrite"]; !r.found || r.bumps {
		t.Fatalf("SingleColumnWrite: got found=%v bumps=%v, want found=true bumps=false", r.found, r.bumps)
	}
	if r := results["MisspelledBumpIsNotABump"]; !r.found || r.bumps {
		t.Fatalf("MisspelledBumpIsNotABump: got found=%v bumps=%v, want found=true bumps=false — a string that is not gorm.Expr(...) must not count", r.found, r.bumps)
	}
	if r := results["UnrelatedWrite"]; r.found {
		t.Fatalf("UnrelatedWrite: got found=%v, want false — it never touches password_hash", r.found)
	}
}

// --- UpdateByID pass-through sweep --------------------------------------

func resolveUserRepositoryNamed(t *testing.T, pkgs []*packages.Package) *types.Named {
	t.Helper()

	dbPkg := passwordHashWriterPackageByPath(pkgs, passwordHashWriterDBPackage)
	if dbPkg == nil {
		t.Fatalf("the sweep did not load %s; there is nothing to judge", passwordHashWriterDBPackage)
	}
	object := dbPkg.Types.Scope().Lookup("UserRepository")
	if object == nil {
		t.Fatalf("%s declares no UserRepository type; the barrier's subject moved", passwordHashWriterDBPackage)
	}
	typeName, ok := object.(*types.TypeName)
	if !ok {
		t.Fatalf("UserRepository in %s did not resolve to a type declaration", passwordHashWriterDBPackage)
	}
	named, ok := typeName.Type().(*types.Named)
	if !ok {
		t.Fatalf("UserRepository in %s did not resolve to a named type", passwordHashWriterDBPackage)
	}
	return named
}

func resolveUpdateByIDSignature(t *testing.T, userRepo *types.Named) *types.Signature {
	t.Helper()

	mset := types.NewMethodSet(types.NewPointer(userRepo))
	for i := range mset.Len() {
		selection := mset.At(i)
		if selection.Obj().Name() != "UpdateByID" {
			continue
		}
		fn, ok := selection.Obj().(*types.Func)
		if !ok {
			continue
		}
		sig, ok := fn.Type().(*types.Signature)
		if ok {
			return sig
		}
	}
	t.Fatalf("*UserRepository has no UpdateByID method; the barrier's subject moved")
	return nil
}

type updateByIDCallSite struct {
	position   string
	keys       map[string]bool
	unresolved string
}

// localMapEvidence is what one pass over a package's syntax learns about
// every local variable ever indexed or literal-initialized as a
// map[string]any — keyed by the variable's *types.Object, which is unique per
// declaration even when two functions both name their local "updates".
type localMapEvidence struct {
	keys    map[string]bool
	dynamic bool // a key this sweep could not read as a string literal
}

// findUpdateByIDCallSites resolves every call to the UpdateByID contract
// across the loaded production packages and the literal key set its "updates"
// argument can carry.
func findUpdateByIDCallSites(pkgs []*packages.Package, signature *types.Signature) []updateByIDCallSite {
	var sites []updateByIDCallSite
	for _, pkg := range pkgs {
		if len(pkg.Syntax) == 0 {
			continue
		}
		evidence := collectLocalMapEvidence(pkg)
		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "UpdateByID" {
					return true
				}
				selection := pkg.TypesInfo.Selections[sel]
				if selection == nil {
					return true
				}
				fn, ok := selection.Obj().(*types.Func)
				if !ok {
					return true
				}
				sig, ok := fn.Type().(*types.Signature)
				if !ok || !types.Identical(sig, signature) {
					return true
				}
				if len(call.Args) != 3 {
					return true
				}
				site := updateByIDCallSite{
					position: passwordHashWriterFsetPosition(pkg.Fset.Position(call.Pos())),
				}
				keys, dynamic, resolved := resolveUpdatesArgument(pkg, evidence, call.Args[2])
				if !resolved {
					site.unresolved = "the \"updates\" argument is neither a map literal nor a traceable local variable; " +
						"this sweep cannot prove it excludes password_hash"
				} else if dynamic {
					site.unresolved = "the \"updates\" argument carries at least one non-literal key; " +
						"this sweep cannot prove it excludes password_hash"
				} else {
					site.keys = keys
				}
				sites = append(sites, site)
				return true
			})
		}
	}
	return sites
}

// resolveUpdatesArgument answers the literal key set an UpdateByID call's
// third argument can carry: directly, for a map literal at the call site; via
// collectLocalMapEvidence's table, for a local variable built up beforehand.
func resolveUpdatesArgument(pkg *packages.Package, evidence map[types.Object]*localMapEvidence, arg ast.Expr) (keys map[string]bool, dynamic bool, resolved bool) {
	switch typed := arg.(type) {
	case *ast.CompositeLit:
		keys, dynamic := literalMapLitKeys(typed)
		return keys, dynamic, true
	case *ast.Ident:
		obj := pkg.TypesInfo.Uses[typed]
		if obj == nil {
			return nil, false, false
		}
		entry := evidence[obj]
		if entry == nil {
			// A variable this sweep's evidence pass never saw write a key —
			// an empty map, or one built entirely outside an AssignStmt shape
			// it recognises. Either way, unresolved rather than "no keys":
			// treating it as an empty, safe key set would be the vacuity this
			// barrier exists to refuse.
			return nil, false, false
		}
		return entry.keys, entry.dynamic, true
	default:
		return nil, false, false
	}
}

func literalMapLitKeys(lit *ast.CompositeLit) (keys map[string]bool, dynamic bool) {
	keys = map[string]bool{}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			dynamic = true
			continue
		}
		key := stringLiteralOf(kv.Key)
		if key == "" {
			dynamic = true
			continue
		}
		keys[key] = true
	}
	return keys, dynamic
}

// collectLocalMapEvidence walks every AssignStmt in the package once,
// recording every literal key a local variable is ever initialized with or
// indexed by. It over-approximates deliberately in one direction only: a key
// assigned ANYWHERE in the package to a variable object is attributed to that
// object, without checking that the assignment happens before the UpdateByID
// call that reads it — the safe direction for a sweep whose finding is
// "this key might reach the call".
func collectLocalMapEvidence(pkg *packages.Package) map[types.Object]*localMapEvidence {
	table := map[types.Object]*localMapEvidence{}
	entry := func(obj types.Object) *localMapEvidence {
		if table[obj] == nil {
			table[obj] = &localMapEvidence{keys: map[string]bool{}}
		}
		return table[obj]
	}

	for _, file := range pkg.Syntax {
		ast.Inspect(file, func(node ast.Node) bool {
			assign, ok := node.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for i, lhs := range assign.Lhs {
				switch target := lhs.(type) {
				case *ast.Ident:
					if i >= len(assign.Rhs) {
						continue
					}
					lit, ok := assign.Rhs[i].(*ast.CompositeLit)
					if !ok {
						continue
					}
					obj := identObject(pkg, target)
					if obj == nil {
						continue
					}
					keys, dynamic := literalMapLitKeys(lit)
					e := entry(obj)
					if dynamic {
						e.dynamic = true
					}
					for key := range keys {
						e.keys[key] = true
					}
				case *ast.IndexExpr:
					ident, ok := target.X.(*ast.Ident)
					if !ok {
						continue
					}
					obj := pkg.TypesInfo.Uses[ident]
					if obj == nil {
						continue
					}
					e := entry(obj)
					key := stringLiteralOf(target.Index)
					if key == "" {
						e.dynamic = true
						continue
					}
					e.keys[key] = true
				}
			}
			return true
		})
	}
	return table
}

// identObject resolves an *ast.Ident to its *types.Object whether the
// identifier is a fresh declaration (`updates := ...`) or a later use.
func identObject(pkg *packages.Package, ident *ast.Ident) types.Object {
	if obj := pkg.TypesInfo.Defs[ident]; obj != nil {
		return obj
	}
	return pkg.TypesInfo.Uses[ident]
}

// TestUpdateByIDKeyResolutionRecognisesItsOwnFixtures anchors the local-map
// tracing (collectLocalMapEvidence + resolveUpdatesArgument) on a fixture this
// test owns and type-checks itself, independent of the live tree: a direct
// literal call, a traced local variable, a variable indexed with a dynamic
// key (must read as unresolved, never as an empty safe key set), and an
// argument this sweep cannot trace at all (a bare function-call result).
func TestUpdateByIDKeyResolutionRecognisesItsOwnFixtures(t *testing.T) {
	const fixture = `package fixture

type R struct{}

func (r *R) UpdateByID(userID uint, updates map[string]any) error { return nil }

func direct(r *R, userID uint) error {
	return r.UpdateByID(userID, map[string]any{"password_hash": "x", "auth_session_version": 1})
}

func traced(r *R, userID uint) error {
	updates := map[string]any{}
	updates["cycle_length"] = 1
	updates["period_length"] = 2
	return r.UpdateByID(userID, updates)
}

func tracedWithDynamicKey(r *R, userID uint, column string) error {
	updates := map[string]any{}
	updates[column] = 1
	return r.UpdateByID(userID, updates)
}

func untraceable(r *R, userID uint) error {
	return r.UpdateByID(userID, buildUpdates())
}

func buildUpdates() map[string]any { return nil }
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture.go", fixture, 0)
	if err != nil {
		t.Fatalf("parsing the fixture: %v", err)
	}

	info := &types.Info{
		Types:      map[ast.Expr]types.TypeAndValue{},
		Defs:       map[*ast.Ident]types.Object{},
		Uses:       map[*ast.Ident]types.Object{},
		Selections: map[*ast.SelectorExpr]*types.Selection{},
	}
	conf := types.Config{Importer: nil, Error: func(err error) { t.Fatalf("type-checking the fixture: %v", err) }}
	pkgTypes, err := conf.Check("fixture", fset, []*ast.File{file}, info)
	if err != nil {
		t.Fatalf("type-checking the fixture: %v", err)
	}

	pkg := &packages.Package{
		Fset:      fset,
		Syntax:    []*ast.File{file},
		TypesInfo: info,
		Types:     pkgTypes,
	}

	evidence := collectLocalMapEvidence(pkg)

	var directCall, tracedCall, dynamicCall, untraceableCall *ast.CallExpr
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "UpdateByID" {
			return true
		}
		enclosingFuncName := enclosingFuncNameFor(file, call)
		switch enclosingFuncName {
		case "direct":
			directCall = call
		case "traced":
			tracedCall = call
		case "tracedWithDynamicKey":
			dynamicCall = call
		case "untraceable":
			untraceableCall = call
		}
		return true
	})
	if directCall == nil || tracedCall == nil || dynamicCall == nil || untraceableCall == nil {
		t.Fatalf("the fixture walk did not find all four UpdateByID calls it declares")
	}

	if keys, dynamic, resolved := resolveUpdatesArgument(pkg, evidence, directCall.Args[1]); !resolved || dynamic || !keys["password_hash"] || !keys["auth_session_version"] {
		t.Fatalf("direct literal: resolved=%v dynamic=%v keys=%v, want resolved=true dynamic=false with both keys present", resolved, dynamic, keys)
	}
	if keys, dynamic, resolved := resolveUpdatesArgument(pkg, evidence, tracedCall.Args[1]); !resolved || dynamic || keys["password_hash"] || !keys["cycle_length"] || !keys["period_length"] {
		t.Fatalf("traced local var: resolved=%v dynamic=%v keys=%v, want resolved=true dynamic=false with the two traced keys and no password_hash", resolved, dynamic, keys)
	}
	if _, dynamic, resolved := resolveUpdatesArgument(pkg, evidence, dynamicCall.Args[1]); !resolved || !dynamic {
		t.Fatalf("dynamic-keyed local var: resolved=%v dynamic=%v, want resolved=true dynamic=true — a variable-keyed index must read as unresolved, never as an empty safe key set", resolved, dynamic)
	}
	if _, _, resolved := resolveUpdatesArgument(pkg, evidence, untraceableCall.Args[1]); resolved {
		t.Fatalf("untraceable argument: resolved=%v, want false — a bare function-call result is not a map literal or a traced local variable", resolved)
	}
}

func enclosingFuncNameFor(file *ast.File, target *ast.CallExpr) string {
	var name string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		found := false
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			if node == target {
				found = true
			}
			return true
		})
		if found {
			name = fn.Name.Name
			break
		}
	}
	return name
}

// --- shared tree loading (mirrors declaration_reachability_barrier_test.go) --

var (
	passwordHashWriterTreeOnce   sync.Once
	passwordHashWriterTreeCached []*packages.Package
	passwordHashWriterTreeErr    error
)

func loadPasswordHashWriterTree(t *testing.T) []*packages.Package {
	t.Helper()

	passwordHashWriterTreeOnce.Do(func() {
		passwordHashWriterTreeCached, passwordHashWriterTreeErr = buildPasswordHashWriterTree()
	})
	if passwordHashWriterTreeErr != nil {
		t.Fatalf("the barrier could not read the tree, so nothing here is a clean bill: %v", passwordHashWriterTreeErr)
	}
	if len(passwordHashWriterTreeCached) < 14 {
		t.Fatalf("type-checked only %d package(s); the module is far larger, so this sweep measured the wrong tree", len(passwordHashWriterTreeCached))
	}
	return passwordHashWriterTreeCached
}

func buildPasswordHashWriterTree() ([]*packages.Package, error) {
	root, err := passwordHashWriterModuleRoot()
	if err != nil {
		return nil, err
	}

	config := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedImports | packages.NeedDeps,
		Dir: root,
		// Tests excluded on purpose: this measures what the APPLICATION
		// reaches. A call reached only from a _test.go is not a production
		// caller of UpdateByID.
		Tests: false,
	}
	loaded, err := packages.Load(config, "./cmd/...", "./internal/...", "./migrations/...", "./scripts/...", "./web/...")
	if err != nil {
		return nil, fmt.Errorf("type-checking the shipped packages: %w", err)
	}

	var loadErrors []string
	for _, pkg := range loaded {
		for _, packageError := range pkg.Errors {
			loadErrors = append(loadErrors, pkg.PkgPath+": "+packageError.Error())
		}
	}
	if len(loadErrors) > 0 {
		return nil, fmt.Errorf("the tree does not type-check, so no writer or call site could be identified:\n  %s", strings.Join(loadErrors, "\n  "))
	}
	return loaded, nil
}

func passwordHashWriterPackageByPath(pkgs []*packages.Package, path string) *packages.Package {
	for _, pkg := range pkgs {
		if pkg.PkgPath == path {
			return pkg
		}
	}
	return nil
}

func passwordHashWriterPosition(t *testing.T, position token.Position) string {
	t.Helper()
	return passwordHashWriterFsetPosition(position)
}

func passwordHashWriterFsetPosition(position token.Position) string {
	root, err := passwordHashWriterModuleRoot()
	if err != nil {
		return position.String()
	}
	relative, err := filepath.Rel(root, position.Filename)
	if err != nil {
		return position.String()
	}
	return fmt.Sprintf("%s:%d", filepath.ToSlash(relative), position.Line)
}

// passwordHashWriterModuleRoot walks up from the package directory to the
// module root, exactly as declaration_reachability_barrier_test.go's
// moduleRootForBarrier does — duplicated rather than shared because the two
// files live in different packages and this repository's test discipline
// keeps a mutation-kill-shaped barrier file self-contained.
func passwordHashWriterModuleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolving the working directory: %w", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above %s; the sweep would measure nothing", dir)
		}
		dir = parent
	}
}
