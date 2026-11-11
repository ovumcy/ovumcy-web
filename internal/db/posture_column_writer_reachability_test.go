package db

import (
	"fmt"
	"go/ast"
	"go/types"
	"sort"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/testpackages"
	"golang.org/x/tools/go/packages"
)

// The password_hash sweep in password_hash_writer_reachability_test.go proves
// the premise "every other password_hash writer bumps auth_session_version".
// The session-invalidation invariant is wider: the login-identity and
// security-posture columns below revoke sessions just the same, and nothing
// refused a new writer of one of them that skipped the bump. This file runs
// the same two halves over the whole posture list:
//
//   - TestEveryPostureColumnWriterBumpsAuthSessionVersionExceptTheNamedException
//     scans every function of internal/db (not only *UserRepository, not only
//     one file) for a write of a posture column — a map literal or local map
//     carrying the key, Update/UpdateColumn("<column>", v), or a models.User
//     struct field set — and requires the same update to carry the
//     gorm.Expr("auth_session_version + 1") bump, or hand the map to
//     updateFromAuthSessionVersionTx. Each column must also have its named
//     writers found, so a scan matching nothing goes red.
//   - TestNoUpdateByIDCallerPassesAPostureColumnWithoutABump is the
//     pass-through half over UpdateByID's callers.

// postureColumns are the users columns whose change must revoke live sessions.
var postureColumns = []string{
	"password_hash",
	"totp_enabled",
	"totp_secret",
	"recovery_code_hash",
	"local_auth_enabled",
	"email",
}

// postureStructFields maps the models.User field to the column it persists.
var postureStructFields = map[string]string{
	"PasswordHash":     "password_hash",
	"TOTPEnabled":      "totp_enabled",
	"TOTPSecret":       "totp_secret",
	"RecoveryCodeHash": "recovery_code_hash",
	"LocalAuthEnabled": "local_auth_enabled",
	"Email":            "email",
}

// postureWriterExceptions is keyed "<function>:<column>" and checked in both
// directions: an entry the sweep no longer finds is stale.
var postureWriterExceptions = map[string]string{
	"UserRepository.UpgradePasswordHashCAS:password_hash": "the opportunistic bcrypt-cost rehash: same password, " +
		"stronger hash, guarded by a CAS on the old hash (TestRehashRace).",
	"UserRepository.UpgradeTOTPSecretCiphertextCAS:totp_secret": "transparent re-encryption of the same TOTP seed " +
		"under the current aad-bound format, guarded by a CAS on the old ciphertext: the factor is unchanged.",
	"UserRepository.RenormalizeUserEmail:email": "repair of the stored spelling of the same mailbox under a CAS " +
		"on the previous value; the login identity is unchanged.",
}

// mustNamePostureWriters is the per-column floor: each column must have at
// least these writers found by the sweep.
var mustNamePostureWriters = map[string][]string{
	"password_hash":      {"UserRepository.UpdatePasswordAndRevokeSessions", "UserRepository.UpgradePasswordHashCAS"},
	"totp_enabled":       {"UserRepository.UpdateTOTPFieldsAndRevokeSessions"},
	"totp_secret":        {"UserRepository.UpdateTOTPFieldsAndRevokeSessions", "UserRepository.UpgradeTOTPSecretCiphertextCAS"},
	"recovery_code_hash": {"UserRepository.UpdateRecoveryCodeHashAndRevokeSessions"},
	"local_auth_enabled": {"UserRepository.UpdatePasswordAndRevokeSessions"},
	"email":              {"UserRepository.SetUserEmailByIDAndRevokeSessions", "UserRepository.RenormalizeUserEmail"},
}

func postureColumnSet() map[string]bool {
	set := map[string]bool{}
	for _, column := range postureColumns {
		set[column] = true
	}
	return set
}

type postureWrite struct {
	function string
	column   string
	position string
	bumps    bool
}

// postureSite is one place a function body writes posture columns.
type postureSite struct {
	columns    []string
	literal    *ast.CompositeLit
	variable   types.Object
	inlineBump bool
}

type postureMapKeys struct {
	columns map[string]bool
	bump    bool
	nonBump bool
}

func newPostureMapKeys() *postureMapKeys { return &postureMapKeys{columns: map[string]bool{}} }

func (keys *postureMapKeys) record(key string, value ast.Expr) {
	if postureColumnSet()[key] {
		keys.columns[key] = true
	}
	if key == passwordHashWriterSessionBumpedKey {
		if value != nil && isAuthSessionVersionBumpExpr(value) {
			keys.bump = true
		} else {
			keys.nonBump = true
		}
	}
}

func (keys *postureMapKeys) bumps() bool { return keys.bump && !keys.nonBump }

func (keys *postureMapKeys) sortedColumns() []string {
	var out []string
	for column := range keys.columns {
		out = append(out, column)
	}
	sort.Strings(out)
	return out
}

func postureLitKeys(lit *ast.CompositeLit) *postureMapKeys {
	keys := newPostureMapKeys()
	for _, elt := range lit.Elts {
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			keys.record(stringLiteralOf(kv.Key), kv.Value)
		}
	}
	return keys
}

// resolvePostureFields resolves the models.User fields by declaration.
func resolvePostureFields(t *testing.T, pkg *types.Package) map[types.Object]string {
	t.Helper()

	typeObject, ok := pkg.Scope().Lookup("User").(*types.TypeName)
	if !ok {
		t.Fatalf("%s declares no User type; the struct writes this barrier checks for have moved", pkg.Path())
	}
	structType, ok := typeObject.Type().Underlying().(*types.Struct)
	if !ok {
		t.Fatalf("%s.User is not a struct type", pkg.Path())
	}
	fields := map[types.Object]string{}
	for i := range structType.NumFields() {
		field := structType.Field(i)
		if column, ok := postureStructFields[field.Name()]; ok {
			fields[field] = column
		}
	}
	if len(fields) != len(postureStructFields) {
		t.Fatalf("%s.User declares %d of the %d posture fields; the list moved", pkg.Path(), len(fields), len(postureStructFields))
	}
	return fields
}

func postureSitesInBody(info *types.Info, fields map[types.Object]string, body *ast.BlockStmt) []postureSite {
	var sites []postureSite
	variables := map[types.Object]*postureMapKeys{}
	var variableOrder []types.Object
	variable := func(obj types.Object) *postureMapKeys {
		if variables[obj] == nil {
			variables[obj] = newPostureMapKeys()
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
		litKeys := postureLitKeys(lit)
		keys := variable(obj)
		for column := range litKeys.columns {
			keys.columns[column] = true
		}
		keys.bump = keys.bump || litKeys.bump
		keys.nonBump = keys.nonBump || litKeys.nonBump
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
					if column, ok := fields[info.Uses[target.Sel]]; ok {
						sites = append(sites, postureSite{columns: []string{column}})
					}
				}
			}
		case *ast.ValueSpec:
			for i, name := range n.Names {
				if i < len(n.Values) {
					bind(name, n.Values[i])
				}
			}
		case *ast.CallExpr:
			// maps.Copy(local, literal) merges the literal into the local map:
			// the literal is part of that update, not a separate one.
			if !isMapsCopy(info, n) || len(n.Args) != 2 {
				return true
			}
			dst, ok := n.Args[0].(*ast.Ident)
			lit, litOK := n.Args[1].(*ast.CompositeLit)
			if !ok || !litOK || info.Uses[dst] == nil {
				return true
			}
			bound[lit] = true
			litKeys := postureLitKeys(lit)
			keys := variable(info.Uses[dst])
			for column := range litKeys.columns {
				keys.columns[column] = true
			}
			keys.bump = keys.bump || litKeys.bump
			keys.nonBump = keys.nonBump || litKeys.nonBump
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
			if column := stringLiteralOf(n.Args[0]); postureColumnSet()[column] {
				sites = append(sites, postureSite{columns: []string{column}})
			}
		case *ast.CompositeLit:
			var structColumns []string
			for _, elt := range n.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					if key, ok := kv.Key.(*ast.Ident); ok {
						if column, ok := fields[info.Uses[key]]; ok {
							structColumns = append(structColumns, column)
						}
					}
				}
			}
			if len(structColumns) > 0 {
				sites = append(sites, postureSite{columns: structColumns})
				return true
			}
			if bound[n] {
				return true
			}
			if keys := postureLitKeys(n); len(keys.columns) > 0 {
				sites = append(sites, postureSite{columns: keys.sortedColumns(), literal: n, inlineBump: keys.bumps()})
			}
		}
		return true
	})

	for _, obj := range variableOrder {
		if keys := variables[obj]; len(keys.columns) > 0 {
			sites = append(sites, postureSite{columns: keys.sortedColumns(), variable: obj, inlineBump: keys.bumps()})
		}
	}
	return sites
}

// isMapsCopy resolves the call to the standard library's maps.Copy by
// declaration, not by spelling.
func isMapsCopy(info *types.Info, call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	fn, ok := info.Uses[sel.Sel].(*types.Func)
	return ok && fn.Name() == "Copy" && fn.Pkg() != nil && fn.Pkg().Path() == "maps"
}

func postureFunctionName(pkg *packages.Package, fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return fn.Name.Name
	}
	if named := namedBehindPointer(pkg.TypesInfo.TypeOf(fn.Recv.List[0].Type)); named != nil {
		return named.Obj().Name() + "." + fn.Name.Name
	}
	return fn.Name.Name
}

func findPostureWrites(t *testing.T, pkg *packages.Package, casHelper types.Object, fields map[types.Object]string) []postureWrite {
	t.Helper()

	var writes []postureWrite
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			sites := postureSitesInBody(pkg.TypesInfo, fields, fn.Body)
			bumped := map[string]bool{}
			for _, site := range sites {
				ok := site.inlineBump ||
					siteDelegatesSessionBump(pkg, fn.Body, passwordHashWriteSite{literal: site.literal, variable: site.variable}, casHelper)
				for _, column := range site.columns {
					if _, seen := bumped[column]; !seen {
						bumped[column] = ok
					} else {
						bumped[column] = bumped[column] && ok
					}
				}
			}
			for column, ok := range bumped {
				writes = append(writes, postureWrite{
					function: postureFunctionName(pkg, fn),
					column:   column,
					position: passwordHashWriterPosition(t, pkg.Fset.Position(fn.Pos())),
					bumps:    ok,
				})
			}
		}
	}
	sort.Slice(writes, func(i, j int) bool {
		if writes[i].function != writes[j].function {
			return writes[i].function < writes[j].function
		}
		return writes[i].column < writes[j].column
	})
	return writes
}

func TestEveryPostureColumnWriterBumpsAuthSessionVersionExceptTheNamedException(t *testing.T) {
	if testpackages.Isolate(t) {
		return
	}
	pkgs := loadPasswordHashWriterTree(t)
	dbPkg := passwordHashWriterPackageByPath(pkgs, passwordHashWriterDBPackage)
	modelsPkg := passwordHashWriterPackageByPath(pkgs, passwordHashWriterModelsPackage)
	if dbPkg == nil || modelsPkg == nil {
		t.Fatalf("the sweep did not load %s and %s", passwordHashWriterDBPackage, passwordHashWriterModelsPackage)
	}
	casHelper := resolveAuthSessionVersionCASHelper(t, dbPkg)
	fields := resolvePostureFields(t, modelsPkg.Types)

	writes := findPostureWrites(t, dbPkg, casHelper, fields)
	found := map[string]bool{}
	byColumn := map[string]map[string]bool{}
	for _, write := range writes {
		found[write.function+":"+write.column] = true
		if byColumn[write.column] == nil {
			byColumn[write.column] = map[string]bool{}
		}
		byColumn[write.column][write.function] = true
	}
	for _, column := range postureColumns {
		if len(byColumn[column]) == 0 {
			t.Fatalf("the sweep found no writer of users.%s; it is reading the wrong package or shape, and a barrier with no subject passes about nothing", column)
		}
		names := mustNamePostureWriters[column]
		if len(names) == 0 {
			t.Fatalf("mustNamePostureWriters names no writer for users.%s", column)
		}
		for _, name := range names {
			if !byColumn[column][name] {
				t.Fatalf("the sweep did not find %s writing users.%s; either it moved, or this barrier stopped seeing it", name, column)
			}
		}
	}
	for key := range postureWriterExceptions {
		if !found[key] {
			t.Fatalf("postureWriterExceptions declares %s, which the sweep no longer finds; the entry is stale", key)
		}
	}

	var unbumped []string
	for _, write := range writes {
		if write.bumps || postureWriterExceptions[write.function+":"+write.column] != "" {
			continue
		}
		unbumped = append(unbumped, fmt.Sprintf("  %s writes users.%s\n      declared at %s", write.function, write.column, write.position))
	}
	if len(unbumped) == 0 {
		return
	}
	t.Fatalf("%d writer(s) of a login-identity or security-posture column lack a valid auth_session_version bump "+
		"in the same update and are not declared in postureWriterExceptions:\n%s\n"+
		"A change to a credential, the login identity or the security posture must revoke live sessions: bump "+
		"auth_session_version in the same map, delegate that map to updateFromAuthSessionVersionTx, or declare the "+
		"writer with the reason it is safe not to.",
		len(unbumped), strings.Join(unbumped, "\n"))
}

// judgePostureUpdatesArgument is judgeUpdatesArgument over the whole posture
// list.
func judgePostureUpdatesArgument(pkg *packages.Package, evidence map[types.Object]*localMapEvidence, arg ast.Expr) string {
	entry, resolved := resolveUpdatesArgument(pkg, evidence, arg)
	switch {
	case !resolved:
		return "the \"updates\" argument is neither a map literal nor a traceable local variable; " +
			"this sweep cannot prove it excludes the posture columns"
	case entry.escaped:
		return "the \"updates\" map is handed to another call or aliased, where keys can be added unseen"
	case entry.dynamic:
		return "the \"updates\" argument carries at least one non-literal key"
	}
	for _, column := range postureColumns {
		if entry.keys[column] && !entry.bumpsSessionVersion() {
			return fmt.Sprintf("passes %q without auth_session_version set to gorm.Expr(\"auth_session_version + 1\")", column)
		}
	}
	return ""
}

func TestNoUpdateByIDCallerPassesAPostureColumnWithoutABump(t *testing.T) {
	if testpackages.Isolate(t) {
		return
	}
	pkgs := loadPasswordHashWriterTree(t)
	userRepo := resolveUserRepositoryNamed(t, pkgs)
	signature := resolveUpdateByIDSignature(t, userRepo)

	var bad []string
	calls := 0
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
				calls++
				if problem := judgePostureUpdatesArgument(pkg, evidence, call.Args[2]); problem != "" {
					bad = append(bad, fmt.Sprintf("  %s\n      %s", passwordHashWriterFsetPosition(pkg.Fset.Position(call.Pos())), problem))
				}
				return true
			})
		}
	}
	if calls == 0 {
		t.Fatalf("the sweep found no call to the UpdateByID contract; it is measuring the wrong signature")
	}
	if len(bad) == 0 {
		return
	}
	sort.Strings(bad)
	t.Fatalf("%d UpdateByID call site(s) this sweep cannot clear of writing a posture column without a session bump:\n%s\n"+
		"UpdateByID never bumps auth_session_version on its own; a posture-column change belongs on a dedicated "+
		"*AndRevokeSessions method.", len(bad), strings.Join(bad, "\n"))
}
