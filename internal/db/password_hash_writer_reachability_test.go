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
//     valid auth_session_version bump in the SAME update (the same map
//     literal, or the same local map built up by index assignment), or to hand
//     that same map to updateFromAuthSessionVersionTx, unless it is named in
//     passwordHashWriterExceptions. A struct write through
//     models.User.PasswordHash, and the single-column Update/UpdateColumn
//     forms, can carry no bump at all and always count as unbumped.
//   - TestNoUpdateByIDCallerPassesPasswordHashWithoutABump resolves every call
//     to the UpdateByID contract (matched by SIGNATURE IDENTITY against the
//     concrete method — receiver ignored, so every one of the several narrow
//     per-service interfaces that separately redeclare UpdateByID(ctx, userID,
//     updates) still matches), traces the literal string keys the "updates"
//     argument can carry — through a local variable's index assignments when
//     it is not a composite literal at the call site — and refuses a call
//     whose key set could contain "password_hash" without auth_session_version
//     set to the gorm.Expr("auth_session_version + 1") bump, or whose key set
//     this sweep cannot resolve at all (a non-literal key, or a map handed to
//     another call that could add keys unseen): an unresolvable argument is
//     not proof of absence.
//
// A SQL column name used as a map key has no Go declaration to resolve, so
// those leaf comparisons are necessarily string matches against literal keys —
// that is not the "shape of a node" pattern this repository's guides warn
// against; it is the only kind of evidence a column name is. What IS resolved
// by declaration is everything that has one: which Go method a call reaches (a
// receiver type, a signature), which helper a map is delegated to, and which
// struct field a struct write sets (models.User.PasswordHash as a
// *types.Var, so a lookalike field on another type never matches).

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
	passwordHashWriterModulePath       = "github.com/ovumcy/ovumcy-web"
	passwordHashWriterDBPackage        = passwordHashWriterModulePath + "/internal/db"
	passwordHashWriterModelsPackage    = passwordHashWriterModulePath + "/internal/models"
	passwordHashWriterServicesPackage  = passwordHashWriterModulePath + "/internal/services"
	passwordHashWriterSessionBumpedKey = "auth_session_version"
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
	modelsPkg := passwordHashWriterPackageByPath(pkgs, passwordHashWriterModelsPackage)
	if modelsPkg == nil {
		t.Fatalf("the sweep did not load %s, so models.User.PasswordHash cannot be resolved", passwordHashWriterModelsPackage)
	}
	casHelper := resolveAuthSessionVersionCASHelper(t, dbPkg)
	passwordHashField := resolvePasswordHashField(t, modelsPkg.Types, "User")

	writes := findPasswordHashWrites(t, dbPkg, userRepo, casHelper, passwordHashField)
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
		"same map, delegate that map to updateFromAuthSessionVersionTx, or add the method to "+
		"passwordHashWriterExceptions with the reason it is safe not to.",
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
		if site.problem != "" {
			bad = append(bad, fmt.Sprintf("  %s\n      %s", site.position, site.problem))
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
		"assignments with literal string keys on a map no other call receives) so this barrier can clear it.",
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
// hands the SAME map to this exact function object — not to a same-named
// lookalike — gets auth_session_version bumped atomically with whatever
// columns it passed, including password_hash.
func resolveAuthSessionVersionCASHelper(t *testing.T, dbPkg *packages.Package) types.Object {
	t.Helper()

	object := dbPkg.Types.Scope().Lookup("updateFromAuthSessionVersionTx")
	if object == nil {
		t.Fatalf("%s declares no updateFromAuthSessionVersionTx helper; the CAS-bump delegation this barrier checks for has moved or been renamed", passwordHashWriterDBPackage)
	}
	return object
}

// resolvePasswordHashField resolves the PasswordHash field of typeName as a
// *types.Var. Every struct write this sweep recognises is then matched by
// object identity against it, so a same-named field on any other type is not
// a password_hash write.
func resolvePasswordHashField(t *testing.T, pkg *types.Package, typeName string) types.Object {
	t.Helper()

	typeObject, ok := pkg.Scope().Lookup(typeName).(*types.TypeName)
	if !ok {
		t.Fatalf("%s declares no %s type; the struct write this barrier checks for has moved", pkg.Path(), typeName)
	}
	structType, ok := typeObject.Type().Underlying().(*types.Struct)
	if !ok {
		t.Fatalf("%s.%s is not a struct type", pkg.Path(), typeName)
	}
	for i := range structType.NumFields() {
		if field := structType.Field(i); field.Name() == "PasswordHash" {
			return field
		}
	}
	t.Fatalf("%s.%s declares no PasswordHash field; the struct write this barrier checks for has moved", pkg.Path(), typeName)
	return nil
}

func findPasswordHashWrites(t *testing.T, pkg *packages.Package, userRepo *types.Named, casHelper types.Object, passwordHashField types.Object) []passwordHashWrite {
	t.Helper()

	var writes []passwordHashWrite
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Body == nil {
				continue
			}
			if !receiverIsNamed(pkg, fn, userRepo) {
				continue
			}
			sites := passwordHashWriteSitesInBody(pkg.TypesInfo, passwordHashField, fn.Body)
			if len(sites) == 0 {
				continue
			}
			bumps := true
			for _, site := range sites {
				if site.inlineBump {
					continue
				}
				if siteDelegatesSessionBump(pkg, fn.Body, site, casHelper) {
					continue
				}
				bumps = false
			}
			writes = append(writes, passwordHashWrite{
				method:   fn.Name.Name,
				position: passwordHashWriterPosition(t, pkg.Fset.Position(fn.Pos())),
				bumps:    bumps,
			})
		}
	}
	return writes
}

// siteDelegatesSessionBump answers whether the site's map — the literal
// itself, or the local variable that carries it — is passed, as one of the
// call's arguments, to a call resolving BY DECLARATION to casHelper. The
// object identity check is what keeps this from matching a hypothetical
// unrelated function that merely happens to share the name.
func siteDelegatesSessionBump(pkg *packages.Package, body *ast.BlockStmt, site passwordHashWriteSite, casHelper types.Object) bool {
	if casHelper == nil || (site.literal == nil && site.variable == nil) {
		return false
	}
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
			if site.literal != nil && arg == site.literal {
				delegated = true
			}
			if argIdent, ok := arg.(*ast.Ident); ok && site.variable != nil && pkg.TypesInfo.Uses[argIdent] == site.variable {
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
// users.password_hash: a map literal carrying the key itself (literal !=
// nil), a local map given the key by index assignment (variable != nil), or a
// form that can never carry a bump — Update/UpdateColumn("password_hash", v),
// a struct literal or field assignment setting models.User.PasswordHash
// (both nil).
type passwordHashWriteSite struct {
	literal    *ast.CompositeLit
	variable   types.Object
	inlineBump bool
}

// passwordHashMapKeys is what one map — a literal, or a local variable across
// every literal and index assignment it receives — says about the two columns
// this barrier cares about. A bump is valid only if every value that map ever
// assigns to auth_session_version is the gorm.Expr increment: a later literal
// overwrite would replace it.
type passwordHashMapKeys struct {
	hash    bool
	bump    bool
	nonBump bool
}

func (keys *passwordHashMapKeys) record(key string, value ast.Expr) {
	switch key {
	case "password_hash":
		keys.hash = true
	case passwordHashWriterSessionBumpedKey:
		if value != nil && isAuthSessionVersionBumpExpr(value) {
			keys.bump = true
		} else {
			keys.nonBump = true
		}
	}
}

func (keys *passwordHashMapKeys) absorb(other passwordHashMapKeys) {
	keys.hash = keys.hash || other.hash
	keys.bump = keys.bump || other.bump
	keys.nonBump = keys.nonBump || other.nonBump
}

func (keys passwordHashMapKeys) bumps() bool {
	return keys.bump && !keys.nonBump
}

func mapLitPasswordHashKeys(lit *ast.CompositeLit) passwordHashMapKeys {
	var keys passwordHashMapKeys
	for _, elt := range lit.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			keys.record(stringLiteralOf(kv.Key), kv.Value)
		}
	}
	return keys
}

// passwordHashWriteSitesInBody finds every place a method body writes
// users.password_hash and whether that SAME map also carries a valid inline
// auth_session_version bump. Map keys are column names and are read as
// string literals; the struct field is resolved through info and compared by
// object identity against passwordHashField; a local map is tracked by its
// *types.Object, so two locals that share a name never pool their keys.
//
// A bump delegated to the auth_session_version_cas.go helper rather than
// written inline is a SEPARATE signal, checked in findPasswordHashWrites.
// TestPasswordHashWriteDetectorRecognisesItsOwnFixtures drives both through
// findPasswordHashWrites on a fixture it type-checks itself, so the
// detector's correctness is not entangled with the tree it judges.
func passwordHashWriteSitesInBody(info *types.Info, passwordHashField types.Object, body *ast.BlockStmt) []passwordHashWriteSite {
	var sites []passwordHashWriteSite
	variables := map[types.Object]*passwordHashMapKeys{}
	var variableOrder []types.Object
	variable := func(obj types.Object) *passwordHashMapKeys {
		if variables[obj] == nil {
			variables[obj] = &passwordHashMapKeys{}
			variableOrder = append(variableOrder, obj)
		}
		return variables[obj]
	}
	bound := map[*ast.CompositeLit]bool{}
	bind := func(ident *ast.Ident, value ast.Expr) {
		lit, ok := value.(*ast.CompositeLit)
		if !ok {
			return
		}
		obj := identObjectIn(info, ident)
		if obj == nil {
			return
		}
		bound[lit] = true
		variable(obj).absorb(mapLitPasswordHashKeys(lit))
	}

	ast.Inspect(body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.AssignStmt:
			for i, lhs := range n.Lhs {
				var value ast.Expr
				if len(n.Lhs) == len(n.Rhs) {
					value = n.Rhs[i]
				}
				switch target := lhs.(type) {
				case *ast.Ident:
					bind(target, value)
				case *ast.IndexExpr:
					ident, ok := target.X.(*ast.Ident)
					if !ok {
						continue
					}
					if obj := info.Uses[ident]; obj != nil {
						variable(obj).record(stringLiteralOf(target.Index), value)
					}
				case *ast.SelectorExpr:
					if passwordHashField != nil && info.Uses[target.Sel] == passwordHashField {
						sites = append(sites, passwordHashWriteSite{})
					}
				}
			}
		case *ast.ValueSpec:
			for i, name := range n.Names {
				if i < len(n.Values) {
					bind(name, n.Values[i])
				}
			}
		}
		return true
	})

	ast.Inspect(body, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.CallExpr:
			sel, ok := n.Fun.(*ast.SelectorExpr)
			if !ok || len(n.Args) != 2 || (sel.Sel.Name != "Update" && sel.Sel.Name != "UpdateColumn") {
				return true
			}
			if stringLiteralOf(n.Args[0]) == "password_hash" {
				sites = append(sites, passwordHashWriteSite{})
			}
		case *ast.CompositeLit:
			if compositeLitSetsField(info, n, passwordHashField) {
				sites = append(sites, passwordHashWriteSite{})
				return true
			}
			if bound[n] {
				return true
			}
			if keys := mapLitPasswordHashKeys(n); keys.hash {
				sites = append(sites, passwordHashWriteSite{literal: n, inlineBump: keys.bumps()})
			}
		}
		return true
	})

	for _, obj := range variableOrder {
		if keys := variables[obj]; keys.hash {
			sites = append(sites, passwordHashWriteSite{variable: obj, inlineBump: keys.bumps()})
		}
	}
	return sites
}

// compositeLitSetsField answers whether a struct literal keys field by
// declaration: go/types records a struct literal's key identifier as a use of
// the field it names.
func compositeLitSetsField(info *types.Info, lit *ast.CompositeLit, field types.Object) bool {
	if field == nil {
		return false
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key, ok := kv.Key.(*ast.Ident); ok && info.Uses[key] == field {
			return true
		}
	}
	return false
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
// findPasswordHashWrites on inputs it owns, independent of the live tree —
// an anchor read off the tree itself stops firing the day the tree it judges
// changes shape. The fixture declares its own receiver, its own helper and its
// own User type, so the receiver, delegation and struct-field checks all run
// by declaration exactly as they do against internal/db and internal/models.
func TestPasswordHashWriteDetectorRecognisesItsOwnFixtures(t *testing.T) {
	const fixture = `package fixture

type DB struct{ Error error }

func (db *DB) Updates(values any) *DB                   { return db }
func (db *DB) UpdateColumns(values any) *DB             { return db }
func (db *DB) Update(column string, value any) *DB      { return db }
func (db *DB) UpdateColumn(column string, value any) *DB { return db }
func (db *DB) Save(value any) *DB                       { return db }

type exprs struct{}

func (exprs) Expr(sql string) any { return sql }

var gorm exprs

type User struct {
	PasswordHash       string
	AuthSessionVersion int
}

type Lookalike struct {
	PasswordHash string
}

func updateFromAuthSessionVersionTx(db *DB, columns map[string]any) error { return nil }

type R struct{ db *DB }

type Other struct{ db *DB }

func (repo *R) q() *DB { return repo.db }

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

func (repo *R) IndexAssignedWrite() error {
	updates := map[string]any{}
	updates["password_hash"] = "x"
	return repo.q().Updates(updates).Error
}

func (repo *R) IndexAssignedBumpingWrite() error {
	updates := map[string]any{"must_change_password": false}
	updates["password_hash"] = "x"
	updates["auth_session_version"] = gorm.Expr("auth_session_version + 1")
	return repo.q().Updates(updates).Error
}

func (repo *R) IndexAssignedLiteralVersionIsNotABump() error {
	updates := map[string]any{"password_hash": "x"}
	updates["auth_session_version"] = 1
	return repo.q().Updates(updates).Error
}

func (repo *R) StructLiteralWrite() error {
	return repo.q().Updates(User{PasswordHash: "x", AuthSessionVersion: 2}).Error
}

func (repo *R) StructFieldAssignmentWrite() error {
	var user User
	user.PasswordHash = "x"
	return repo.q().Save(&user).Error
}

func (repo *R) LookalikeStructIsNotAWrite() error {
	return repo.q().Updates(&Lookalike{PasswordHash: "x"}).Error
}

func (repo *R) UpdateColumnWrite() error {
	return repo.q().UpdateColumn("password_hash", "x").Error
}

func (repo *R) UpdateColumnsWrite() error {
	return repo.q().UpdateColumns(map[string]any{"password_hash": "x"}).Error
}

func (repo *R) DelegatedLiteralWrite() error {
	return updateFromAuthSessionVersionTx(repo.q(), map[string]any{"password_hash": "x"})
}

func (repo *R) DelegatedIndexAssignedWrite() error {
	updates := map[string]any{}
	updates["password_hash"] = "x"
	return updateFromAuthSessionVersionTx(repo.q(), updates)
}

func (other *Other) OtherReceiverIsNotASubject() error {
	return other.db.Update("password_hash", "x").Error
}
`
	pkg := typeCheckPasswordHashWriterFixture(t, fixture)
	repo := passwordHashWriterFixtureNamed(t, pkg, "R")
	casHelper := pkg.Types.Scope().Lookup("updateFromAuthSessionVersionTx")
	passwordHashField := resolvePasswordHashField(t, pkg.Types, "User")

	results := map[string]bool{}
	for _, write := range findPasswordHashWrites(t, pkg, repo, casHelper, passwordHashField) {
		results[write.method] = write.bumps
	}

	const (
		wantBumps    = "found, bumps"
		wantUnbumped = "found, does not bump"
		wantAbsent   = "not a password_hash write"
	)
	expectations := map[string]string{
		"BumpingWrite":                          wantBumps,
		"NonBumpingWrite":                       wantUnbumped,
		"SingleColumnWrite":                     wantUnbumped,
		"UnrelatedWrite":                        wantAbsent,
		"MisspelledBumpIsNotABump":              wantUnbumped,
		"IndexAssignedWrite":                    wantUnbumped,
		"IndexAssignedBumpingWrite":             wantBumps,
		"IndexAssignedLiteralVersionIsNotABump": wantUnbumped,
		"StructLiteralWrite":                    wantUnbumped,
		"StructFieldAssignmentWrite":            wantUnbumped,
		"LookalikeStructIsNotAWrite":            wantAbsent,
		"UpdateColumnWrite":                     wantUnbumped,
		"UpdateColumnsWrite":                    wantUnbumped,
		"DelegatedLiteralWrite":                 wantBumps,
		"DelegatedIndexAssignedWrite":           wantBumps,
		"OtherReceiverIsNotASubject":            wantAbsent,
	}
	names := make([]string, 0, len(expectations))
	for name := range expectations {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		bumps, found := results[name]
		got := wantAbsent
		if found && bumps {
			got = wantBumps
		} else if found {
			got = wantUnbumped
		}
		if got != expectations[name] {
			t.Errorf("%s: got %q, want %q", name, got, expectations[name])
		}
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
	position string
	problem  string
}

// localMapEvidence is what one pass over a package's syntax learns about
// every local variable ever indexed or literal-initialized as a
// map[string]any — keyed by the variable's *types.Object, which is unique per
// declaration even when two functions both name their local "updates".
type localMapEvidence struct {
	keys           map[string]bool
	dynamic        bool // a key this sweep could not read as a string literal
	nonBumpVersion bool // auth_session_version assigned something other than the gorm.Expr increment
	escaped        bool // the map was handed to another call, or aliased, where keys can be added unseen
}

func (evidence *localMapEvidence) bumpsSessionVersion() bool {
	return evidence.keys[passwordHashWriterSessionBumpedKey] && !evidence.nonBumpVersion
}

func (evidence *localMapEvidence) recordKey(key string, value ast.Expr) {
	if key == "" {
		evidence.dynamic = true
		return
	}
	evidence.keys[key] = true
	if key == passwordHashWriterSessionBumpedKey && (value == nil || !isAuthSessionVersionBumpExpr(value)) {
		evidence.nonBumpVersion = true
	}
}

// callReachesUpdateByID answers whether call resolves, by signature identity,
// to the UpdateByID contract.
func callReachesUpdateByID(pkg *packages.Package, call *ast.CallExpr, signature *types.Signature) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "UpdateByID" {
		return false
	}
	selection := pkg.TypesInfo.Selections[sel]
	if selection == nil {
		return false
	}
	fn, ok := selection.Obj().(*types.Func)
	if !ok {
		return false
	}
	sig, ok := fn.Type().(*types.Signature)
	return ok && types.Identical(sig, signature)
}

// findUpdateByIDCallSites resolves every call to the UpdateByID contract
// across the loaded production packages and judges the key set its "updates"
// argument can carry.
func findUpdateByIDCallSites(pkgs []*packages.Package, signature *types.Signature) []updateByIDCallSite {
	var sites []updateByIDCallSite
	for _, pkg := range pkgs {
		if len(pkg.Syntax) == 0 {
			continue
		}
		evidence := collectLocalMapEvidence(pkg, signature)
		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok || !callReachesUpdateByID(pkg, call, signature) || len(call.Args) != 3 {
					return true
				}
				sites = append(sites, updateByIDCallSite{
					position: passwordHashWriterFsetPosition(pkg.Fset.Position(call.Pos())),
					problem:  judgeUpdatesArgument(pkg, evidence, call.Args[2]),
				})
				return true
			})
		}
	}
	return sites
}

// judgeUpdatesArgument returns why this sweep cannot clear an UpdateByID
// call's "updates" argument, or "" when its key set is fully known and either
// excludes password_hash or pairs it with the real session bump.
func judgeUpdatesArgument(pkg *packages.Package, evidence map[types.Object]*localMapEvidence, arg ast.Expr) string {
	entry, resolved := resolveUpdatesArgument(pkg, evidence, arg)
	switch {
	case !resolved:
		return "the \"updates\" argument is neither a map literal nor a traceable local variable; " +
			"this sweep cannot prove it excludes password_hash"
	case entry.escaped:
		return "the \"updates\" map is handed to another call or aliased, where keys can be added unseen; " +
			"this sweep cannot prove it excludes password_hash"
	case entry.dynamic:
		return "the \"updates\" argument carries at least one non-literal key; " +
			"this sweep cannot prove it excludes password_hash"
	case entry.keys["password_hash"] && !entry.bumpsSessionVersion():
		return "passes \"password_hash\" without auth_session_version set to gorm.Expr(\"auth_session_version + 1\")"
	}
	return ""
}

// resolveUpdatesArgument answers what an UpdateByID call's third argument can
// carry: directly, for a map literal at the call site; via
// collectLocalMapEvidence's table, for a local variable built up beforehand.
func resolveUpdatesArgument(pkg *packages.Package, evidence map[types.Object]*localMapEvidence, arg ast.Expr) (entry *localMapEvidence, resolved bool) {
	switch typed := arg.(type) {
	case *ast.CompositeLit:
		return literalMapEvidence(typed), true
	case *ast.Ident:
		obj := pkg.TypesInfo.Uses[typed]
		if obj == nil {
			return nil, false
		}
		entry := evidence[obj]
		if entry == nil {
			// A variable this sweep's evidence pass never saw write a key —
			// an empty map, or one built entirely outside an AssignStmt shape
			// it recognises. Either way, unresolved rather than "no keys":
			// treating it as an empty, safe key set would be the vacuity this
			// barrier exists to refuse.
			return nil, false
		}
		return entry, true
	default:
		return nil, false
	}
}

func literalMapEvidence(lit *ast.CompositeLit) *localMapEvidence {
	evidence := &localMapEvidence{keys: map[string]bool{}}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			evidence.dynamic = true
			continue
		}
		evidence.recordKey(stringLiteralOf(kv.Key), kv.Value)
	}
	return evidence
}

// collectLocalMapEvidence walks the package once, recording every literal key
// (and the value given to auth_session_version) a local variable is ever
// initialized with or indexed by, and marking a map-typed variable escaped
// when it is passed to any call other than the UpdateByID call itself or a
// builtin, or assigned to another name — a callee or an alias can add keys
// this pass never sees. It over-approximates deliberately in one direction
// only: a key assigned ANYWHERE in the package to a variable object is
// attributed to that object, without checking that the assignment happens
// before the UpdateByID call that reads it — the safe direction for a sweep
// whose finding is "this key might reach the call".
func collectLocalMapEvidence(pkg *packages.Package, updateByID *types.Signature) map[types.Object]*localMapEvidence {
	table := map[types.Object]*localMapEvidence{}
	entry := func(obj types.Object) *localMapEvidence {
		if table[obj] == nil {
			table[obj] = &localMapEvidence{keys: map[string]bool{}}
		}
		return table[obj]
	}
	markEscaped := func(expr ast.Expr) {
		for {
			switch typed := expr.(type) {
			case *ast.ParenExpr:
				expr = typed.X
				continue
			case *ast.UnaryExpr:
				expr = typed.X
				continue
			}
			break
		}
		ident, ok := expr.(*ast.Ident)
		if !ok {
			return
		}
		variable, ok := pkg.TypesInfo.Uses[ident].(*types.Var)
		if !ok {
			return
		}
		if _, isMap := variable.Type().Underlying().(*types.Map); isMap {
			entry(variable).escaped = true
		}
	}

	for _, file := range pkg.Syntax {
		ast.Inspect(file, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.CallExpr:
				if callReachesUpdateByID(pkg, n, updateByID) || isBuiltinCall(pkg, n) {
					return true
				}
				for _, arg := range n.Args {
					markEscaped(arg)
				}
			case *ast.AssignStmt:
				for _, rhs := range n.Rhs {
					markEscaped(rhs)
				}
				for i, lhs := range n.Lhs {
					var value ast.Expr
					if len(n.Lhs) == len(n.Rhs) {
						value = n.Rhs[i]
					}
					switch target := lhs.(type) {
					case *ast.Ident:
						lit, ok := value.(*ast.CompositeLit)
						if !ok {
							continue
						}
						obj := identObject(pkg, target)
						if obj == nil {
							continue
						}
						e := entry(obj)
						literal := literalMapEvidence(lit)
						e.dynamic = e.dynamic || literal.dynamic
						e.nonBumpVersion = e.nonBumpVersion || literal.nonBumpVersion
						for key := range literal.keys {
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
						entry(obj).recordKey(stringLiteralOf(target.Index), value)
					}
				}
			}
			return true
		})
	}
	return table
}

// isBuiltinCall reports a call to a Go builtin (len, delete, clear, ...):
// none of them can add a key to a map.
func isBuiltinCall(pkg *packages.Package, call *ast.CallExpr) bool {
	fun := call.Fun
	for {
		paren, ok := fun.(*ast.ParenExpr)
		if !ok {
			break
		}
		fun = paren.X
	}
	ident, ok := fun.(*ast.Ident)
	if !ok {
		return false
	}
	_, builtin := pkg.TypesInfo.Uses[ident].(*types.Builtin)
	return builtin
}

// identObject resolves an *ast.Ident to its *types.Object whether the
// identifier is a fresh declaration (`updates := ...`) or a later use.
func identObject(pkg *packages.Package, ident *ast.Ident) types.Object {
	return identObjectIn(pkg.TypesInfo, ident)
}

func identObjectIn(info *types.Info, ident *ast.Ident) types.Object {
	if obj := info.Defs[ident]; obj != nil {
		return obj
	}
	return info.Uses[ident]
}

// TestUpdateByIDKeyResolutionRecognisesItsOwnFixtures anchors the local-map
// tracing (collectLocalMapEvidence + judgeUpdatesArgument) on a fixture this
// test owns and type-checks itself, independent of the live tree: direct
// literals with and without the real bump, traced local variables with and
// without it, a dynamic key, a map handed to a helper or aliased before the
// call (must read as unresolved, never as a known safe key set), and an
// argument this sweep cannot trace at all (a bare function-call result).
func TestUpdateByIDKeyResolutionRecognisesItsOwnFixtures(t *testing.T) {
	const fixture = `package fixture

type R struct{}

func (r *R) UpdateByID(userID uint, updates map[string]any) error { return nil }

type exprs struct{}

func (exprs) Expr(sql string) any { return sql }

var gorm exprs

func direct(r *R, userID uint) error {
	return r.UpdateByID(userID, map[string]any{"password_hash": "x", "auth_session_version": gorm.Expr("auth_session_version + 1")})
}

func directWithLiteralVersion(r *R, userID uint) error {
	return r.UpdateByID(userID, map[string]any{"password_hash": "x", "auth_session_version": 1})
}

func traced(r *R, userID uint) error {
	updates := map[string]any{}
	updates["cycle_length"] = 1
	updates["period_length"] = 2
	if len(updates) == 0 {
		return nil
	}
	return r.UpdateByID(userID, updates)
}

func tracedWithBump(r *R, userID uint) error {
	updates := map[string]any{"password_hash": "x"}
	updates["auth_session_version"] = gorm.Expr("auth_session_version+1")
	return r.UpdateByID(userID, updates)
}

func tracedWithLiteralVersion(r *R, userID uint) error {
	updates := map[string]any{"password_hash": "x"}
	updates["auth_session_version"] = 1
	return r.UpdateByID(userID, updates)
}

func tracedWithDynamicKey(r *R, userID uint, column string) error {
	updates := map[string]any{}
	updates[column] = 1
	return r.UpdateByID(userID, updates)
}

func tracedThenHandedToAHelper(r *R, userID uint) error {
	updates := map[string]any{"cycle_length": 1}
	addKeys(updates)
	return r.UpdateByID(userID, updates)
}

func tracedThenAliased(r *R, userID uint) error {
	updates := map[string]any{"cycle_length": 1}
	alias := updates
	alias["password_hash"] = "x"
	return r.UpdateByID(userID, updates)
}

func untraceable(r *R, userID uint) error {
	return r.UpdateByID(userID, buildUpdates())
}

func addKeys(updates map[string]any) {}

func buildUpdates() map[string]any { return nil }
`
	pkg := typeCheckPasswordHashWriterFixture(t, fixture)
	signature := resolveUpdateByIDSignature(t, passwordHashWriterFixtureNamed(t, pkg, "R"))
	evidence := collectLocalMapEvidence(pkg, signature)

	calls := map[string]*ast.CallExpr{}
	file := pkg.Syntax[0]
	ast.Inspect(file, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok && callReachesUpdateByID(pkg, call, signature) {
			calls[enclosingFuncNameFor(file, call)] = call
		}
		return true
	})

	// Each problem case names the fragment of its reason, so a case refused
	// for the wrong reason does not pass.
	expectations := map[string]string{
		"direct":                    "",
		"directWithLiteralVersion":  "without auth_session_version set to gorm.Expr",
		"traced":                    "",
		"tracedWithBump":            "",
		"tracedWithLiteralVersion":  "without auth_session_version set to gorm.Expr",
		"tracedWithDynamicKey":      "non-literal key",
		"tracedThenHandedToAHelper": "handed to another call or aliased",
		"tracedThenAliased":         "handed to another call or aliased",
		"untraceable":               "neither a map literal nor a traceable local variable",
	}
	names := make([]string, 0, len(expectations))
	for name := range expectations {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		call := calls[name]
		if call == nil {
			t.Fatalf("the fixture walk did not find the UpdateByID call in %s", name)
		}
		problem := judgeUpdatesArgument(pkg, evidence, call.Args[1])
		want := expectations[name]
		if want == "" && problem != "" {
			t.Errorf("%s: refused with %q, want cleared", name, problem)
		}
		if want != "" && !strings.Contains(problem, want) {
			t.Errorf("%s: got %q, want a refusal containing %q", name, problem, want)
		}
	}

	if entry, resolved := resolveUpdatesArgument(pkg, evidence, calls["traced"].Args[1]); !resolved || entry.keys["password_hash"] || !entry.keys["cycle_length"] || !entry.keys["period_length"] {
		t.Fatalf("traced local var: resolved=%v evidence=%+v, want the two traced keys and no password_hash", resolved, entry)
	}
}

// typeCheckPasswordHashWriterFixture parses and type-checks a self-contained
// fixture (no imports) into the same *packages.Package shape the live sweep
// reads, so a fixture test runs the production detector unchanged.
func typeCheckPasswordHashWriterFixture(t *testing.T, source string) *packages.Package {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture.go", source, 0)
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
	return &packages.Package{
		Fset:      fset,
		Syntax:    []*ast.File{file},
		TypesInfo: info,
		Types:     pkgTypes,
	}
}

func passwordHashWriterFixtureNamed(t *testing.T, pkg *packages.Package, name string) *types.Named {
	t.Helper()

	typeName, ok := pkg.Types.Scope().Lookup(name).(*types.TypeName)
	if !ok {
		t.Fatalf("the fixture declares no type %s", name)
	}
	named, ok := typeName.Type().(*types.Named)
	if !ok {
		t.Fatalf("the fixture's %s is not a named type", name)
	}
	return named
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

// loadPasswordHashWriterTree also checks, by import path, that the two
// packages this barrier reads were loaded: internal/db, where every direct
// writer and UpdateByID itself live, and internal/services, where UpdateByID's
// callers live. A load that misses either measured the wrong tree.
func loadPasswordHashWriterTree(t *testing.T) []*packages.Package {
	t.Helper()

	passwordHashWriterTreeOnce.Do(func() {
		passwordHashWriterTreeCached, passwordHashWriterTreeErr = buildPasswordHashWriterTree()
	})
	if passwordHashWriterTreeErr != nil {
		t.Fatalf("the barrier could not read the tree, so nothing here is a clean bill: %v", passwordHashWriterTreeErr)
	}
	for _, path := range []string{passwordHashWriterDBPackage, passwordHashWriterServicesPackage} {
		pkg := passwordHashWriterPackageByPath(passwordHashWriterTreeCached, path)
		if pkg == nil || len(pkg.Syntax) == 0 {
			t.Fatalf("the sweep did not load %s with its syntax; it measured the wrong tree", path)
		}
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
