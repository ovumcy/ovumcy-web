package db

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"regexp"
	"slices"
	"sort"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/testpackages"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
)

// revocationColumns are the users columns a restore must not roll back
// unnoticed: the session version every cookie is checked against, and every
// credential or login-identity column a sign-in is decided by.
var revocationColumns = map[string]bool{
	"auth_session_version": true,
	"password_hash":        true,
	"recovery_code_hash":   true,
	"must_change_password": true,
	"local_auth_enabled":   true,
	"totp_secret":          true,
	"totp_enabled":         true,
	"email":                true,
}

// revocationUserFields are the same columns as models.User fields, so a struct
// write is found as well as a column map.
var revocationUserFields = []string{
	"AuthSessionVersion", "PasswordHash", "RecoveryCodeHash", "MustChangePassword",
	"LocalAuthEnabled", "TOTPSecret", "TOTPEnabled", "Email",
}

// revocationRowTypes are the models whose row removal revokes a way in: an
// erased account, an unlinked OIDC identity.
var revocationRowTypes = []string{"User", "OIDCIdentity"}

// exemptRevocationWriters write one of those columns or rows and deliberately
// do NOT advance the fence. Each says why a restore across it revives nothing,
// or why the advance belongs to someone else.
var exemptRevocationWriters = map[string]string{
	"updateFromAuthSessionVersionTx":                "the compare-and-set primitive, run inside its caller's transaction: the advance must precede that transaction, so it is the caller's, and the caller is a writer here",
	"bumpAuthSessionVersionFromTx":                  "the same primitive without columns, for the OIDC link and unlink transactions, which advance before opening them",
	"deleteAccountChildRowsTx":                      "the erasure's child-row sweep, run inside the transaction of DeleteAccountAndRelatedData or DeleteAccountFromAuthSessionVersion, both of which advance before opening it",
	"UserRepository.RevokeRestoredAccess":           "the fence's own ContinuityBroken pass: the fence records a fresh token in both halves right after it, so an advance here would write a token in the middle of Enforce",
	"UserRepository.RenormalizeUserEmail":           "respells the same mailbox under a compare-and-set; a restore returns the old spelling of the same login identity, which revokes and grants nothing",
	"UserRepository.UpgradePasswordHashCAS":         "rehashes the password just verified; a restore returns an older hash of the same password",
	"UserRepository.UpgradeTOTPSecretCiphertextCAS": "re-encrypts the same TOTP secret under the current key; a restore returns the same secret under the older ciphertext",
}

// loadBearingRevocationWriters must be found, by name, among the writers the
// scan resolves: sign-out, the TOTP toggle, the password change and the OIDC
// unlink are the revocations WEB-418 and WEB-419 found unfenced, so a scan that
// no longer sees them is not measuring what it claims.
var loadBearingRevocationWriters = []string{
	"UserRepository.BumpAuthSessionVersion",
	"UserRepository.UpdateTOTPFieldsAndRevokeSessions",
	"UserRepository.UpdatePasswordAndRevokeSessions",
	"OIDCIdentityRepository.DeleteForUserAndRevokeSessions",
}

var revocationColumnInSQL = regexp.MustCompile(`\b(auth_session_version|password_hash|recovery_code_hash|must_change_password|local_auth_enabled|totp_secret|totp_enabled|email)\b`)

// TestEverySessionAndCredentialWriterAdvancesTheRestoreFence is WEB-418 and
// WEB-419's completeness guard. The restore fence is the only thing a boot can
// tell a restored backup by; a write that revokes a session or replaces a
// credential and does not move it leaves a restore across that write alone
// invisible, and the restored row validates the old cookie, password, second
// factor or identity again.
//
// Writers are resolved by declaration over every non-test file of this
// package: the columns by constant value, the calls by the object they resolve
// to, struct fields by identity. A writer must call one of the repositories'
// fence advances, and must call it BEFORE its first write or transaction
// (WEB-397).
func TestEverySessionAndCredentialWriterAdvancesTheRestoreFence(t *testing.T) {
	if testpackages.Isolate(t) {
		return
	}
	pkg := loadTypedDBPackage(t)
	info := pkg.TypesInfo

	primitives := map[types.Object]bool{}
	for _, name := range []string{"updateFromAuthSessionVersionTx", "bumpAuthSessionVersionFromTx"} {
		object := pkg.Types.Scope().Lookup(name)
		if object == nil {
			t.Fatalf("the db package no longer declares %s", name)
		}
		primitives[object] = true
	}
	advances := fenceAdvanceMethods(t, pkg)
	fields := map[types.Object]bool{}
	for _, field := range revocationUserFields {
		fields[userModelField(t, pkg, field)] = true
	}
	// Compared with types.Identical, never as map keys: every *types.Pointer is
	// its own instance, so a pointer-keyed lookup would never match.
	var rowTypes []types.Type
	models := pkg.Imports["github.com/ovumcy/ovumcy-web/internal/models"]
	for _, name := range revocationRowTypes {
		object := models.Types.Scope().Lookup(name)
		if object == nil {
			t.Fatalf("internal/models declares no %s", name)
		}
		// gorm deletes from a pointer or a value alike.
		rowTypes = append(rowTypes, object.Type(), types.NewPointer(object.Type()))
	}
	isRevocationRow := func(expr ast.Expr) bool {
		kind := info.TypeOf(expr)
		for _, row := range rowTypes {
			if kind != nil && types.Identical(kind, row) {
				return true
			}
		}
		return false
	}
	rowDeleters := map[string]bool{}

	column := func(expr ast.Expr) string {
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
	isUserLiteral := func(literal *ast.CompositeLit) bool {
		kind := info.TypeOf(literal)
		return kind != nil && types.Identical(kind, models.Types.Scope().Lookup("User").Type())
	}

	writers := map[string]token.Pos{}
	advancing := map[string]bool{}
	advancesFirst := map[string]bool{}
	diverging := map[string]bool{}
	txViolations := map[string][]string{}
	txChecked := map[string]int{}
	helperWrites := map[types.Object]int{}
	for _, file := range pkg.Syntax {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			name := declarationName(function)
			writeAt, advanceAt := token.NoPos, token.NoPos
			var advanceCommit *ast.FuncLit
			earliest := func(current *token.Pos, at token.Pos) {
				if *current == token.NoPos || at < *current {
					*current = at
				}
			}
			transactionAt := token.NoPos
			ast.Inspect(function.Body, func(node ast.Node) bool {
				switch typed := node.(type) {
				case *ast.CallExpr:
					callee, ok := typeutil.Callee(info, typed).(*types.Func)
					if !ok {
						return true
					}
					switch {
					case advances[callee]:
						if advanceAt == token.NoPos || typed.Pos() < advanceAt {
							advanceCommit = fenceWriteClosure(typed)
						}
						earliest(&advanceAt, typed.Pos())
						if callee.Name() == "advanceCalendarFeedFenceBestEffort" {
							diverging[name] = true
						}
					case primitives[callee]:
						earliest(&writeAt, typed.Pos())
					case callee.Pkg() != nil && callee.Pkg().Path() == "gorm.io/gorm":
						switch callee.Name() {
						case "Transaction":
							earliest(&transactionAt, typed.Pos())
						case "Update", "UpdateColumn":
							if len(typed.Args) > 0 && revocationColumns[column(typed.Args[0])] {
								earliest(&writeAt, typed.Pos())
							}
						case "Exec":
							if len(typed.Args) > 0 && revocationColumnInSQL.MatchString(column(typed.Args[0])) {
								earliest(&writeAt, typed.Pos())
							}
						case "Delete":
							if len(typed.Args) > 0 && isRevocationRow(typed.Args[0]) {
								earliest(&writeAt, typed.Pos())
								rowDeleters[name] = true
							}
						}
					}
				case *ast.CompositeLit:
					if isMap(typed) {
						for _, element := range typed.Elts {
							if keyValue, ok := element.(*ast.KeyValueExpr); ok && revocationColumns[column(keyValue.Key)] {
								earliest(&writeAt, typed.Pos())
							}
						}
					}
					if isUserLiteral(typed) {
						for _, element := range typed.Elts {
							if keyValue, ok := element.(*ast.KeyValueExpr); ok {
								if key, ok := keyValue.Key.(*ast.Ident); ok && fields[info.Uses[key]] {
									earliest(&writeAt, typed.Pos())
								}
							}
						}
					}
				case *ast.AssignStmt:
					for _, target := range typed.Lhs {
						switch lhs := target.(type) {
						case *ast.IndexExpr:
							if isMap(lhs.X) && revocationColumns[column(lhs.Index)] {
								earliest(&writeAt, typed.Pos())
							}
						case *ast.SelectorExpr:
							if fields[info.Uses[lhs.Sel]] {
								earliest(&writeAt, typed.Pos())
							}
						}
					}
				}
				return true
			})
			if writeAt == token.NoPos {
				continue
			}
			writers[name] = writeAt
			if advanceAt == token.NoPos {
				continue
			}
			count, found, helpers, _ := fencedWriteTxViolations(pkg, function, func(call *ast.CallExpr) bool {
				callee, ok := typeutil.Callee(info, call).(*types.Func)
				return ok && advances[callee]
			})
			for helper, rooted := range helpers {
				helperWrites[helper] += rooted
			}
			txChecked[name] += count
			txViolations[name] = append(txViolations[name], found...)
			advancing[name] = true
			firstWrite := writeAt
			if transactionAt != token.NoPos && transactionAt < firstWrite {
				firstWrite = transactionAt
			}
			// WEB-505: the first write must sit inside the closure handed to the
			// advance (the transaction that records the marker), and no
			// transaction of the writer's own may open inside it.
			advancesFirst[name] = advanceCommit != nil && insideNode(advanceCommit, int(firstWrite)) &&
				(transactionAt == token.NoPos || !insideNode(advanceCommit, int(transactionAt)))
		}
	}

	// The row-removal arm is anti-vacuous on its own: the unlink must be found
	// as a row deleter, not only through the CAS primitive it also calls.
	if !rowDeleters["OIDCIdentityRepository.DeleteForUserAndRevokeSessions"] {
		t.Fatalf("the scan did not find the OIDC unlink among the row deleters (%v): the Delete arm resolves nothing", sortedNamesOf(rowDeleters))
	}
	for _, required := range loadBearingRevocationWriters {
		if _, found := writers[required]; !found {
			t.Fatalf("the scan did not find %s among the session and credential writers (%v): it is not measuring what it claims", required, sortedPositionNames(writers))
		}
	}

	// The handle check must have read a write of a load-bearing writer, by name:
	// a resolver that rooted nothing would report no violation anywhere.
	if txChecked["UserRepository.BumpAuthSessionVersion"] == 0 {
		t.Fatal("the tx-root check resolved no write of UserRepository.BumpAuthSessionVersion to its closure's tx parameter: it is not measuring what it claims")
	}
	// WEB-517: the helper walk must have entered the OIDC unlink's helper and
	// placed the compare-and-set write two calls down at the tx it was handed,
	// both resolved by declaration.
	bumpHelper := pkg.Types.Scope().Lookup("bumpAuthSessionVersionFromTx")
	casHelper := pkg.Types.Scope().Lookup("updateFromAuthSessionVersionTx")
	if _, entered := helperWrites[bumpHelper]; bumpHelper == nil || !entered {
		t.Fatal("the helper walk never entered bumpAuthSessionVersionFromTx from a fenced closure: it is not measuring what it claims")
	}
	if casHelper == nil || helperWrites[casHelper] == 0 {
		t.Fatal("the helper walk placed no write of updateFromAuthSessionVersionTx at the tx it was handed: it is not measuring what it claims")
	}

	var violations []string
	for _, name := range sortedPositionNames(writers) {
		if _, exempt := exemptRevocationWriters[name]; exempt {
			continue
		}
		at := pkg.Fset.Position(writers[name])
		if found := txViolations[name]; len(found) > 0 {
			violations = append(violations, fmt.Sprintf("%s (%s) must run every write through the tx its advance closure is handed: %v", name, at, found))
		}
		switch {
		case !advancing[name]:
			violations = append(violations, fmt.Sprintf("%s (%s) revokes a session or changes a credential but never advances the restore fence, so a backup restore would undo it unseen", name, at))
		case !advancesFirst[name]:
			violations = append(violations, fmt.Sprintf("%s (%s) must advance the fence BEFORE its write, or a crash between the two leaves the revocation recorded only in the database", name, at))
		case diverging[name] && divergingRevocationWriters[name] == "":
			violations = append(violations, fmt.Sprintf("%s (%s) advances through advanceCalendarFeedFenceBestEffort, which diverges over an unwritable fence file; a session or credential writer must refuse through advanceCalendarFeedFenceOrRefuse (WEB-464)", name, at))
		}
	}
	for name := range divergingRevocationWriters {
		if !diverging[name] {
			t.Errorf("%s is listed as diverging but no longer advances through advanceCalendarFeedFenceBestEffort: drop it from divergingRevocationWriters", name)
		}
	}
	for _, violation := range violations {
		t.Error(violation)
	}

	// An exemption is a rule in both directions: one that stopped writing is
	// stale, and one that started advancing contradicts its own reason.
	for name, reason := range exemptRevocationWriters {
		if _, found := writers[name]; !found {
			t.Errorf("exempt writer %s no longer writes a session or credential column: drop the exemption", name)
		}
		if advancing[name] {
			t.Errorf("%s advances the restore fence although it is exempt (%s): remove the advance or the exemption", name, reason)
		}
	}
}

// persistenceWriteMethods are the gorm and database/sql calls that write. A
// session or credential write made through any of them outside this package
// would never reach the guard above, which reads this package alone.
var persistenceWriteMethods = map[string]map[string]bool{
	"gorm.io/gorm": {
		"Create": true, "CreateInBatches": true, "Save": true, "FirstOrCreate": true,
		"Update": true, "Updates": true, "UpdateColumn": true, "UpdateColumns": true,
		"Delete": true, "Exec": true,
	},
	"database/sql": {"Exec": true, "ExecContext": true},
}

// TestNoPackageOutsideDBWritesThroughThePersistenceLayer widens the session
// writer guard to the module (WEB-454). That guard resolves writers in
// internal/db only, so a revocation column written from a service, the CLI or
// a command would escape it unseen. Rather than repeat the column analysis per
// package, this pins the premise it rests on: no non-test package outside
// internal/db reaches a gorm or database/sql write at all.
func TestNoPackageOutsideDBWritesThroughThePersistenceLayer(t *testing.T) {
	if testpackages.Isolate(t) {
		return
	}
	const module = "github.com/ovumcy/ovumcy-web"
	listed, err := testpackages.Load(&packages.Config{Mode: packages.NeedName | packages.NeedImports | packages.NeedDeps, Dir: "../.."}, "./...")
	if err != nil {
		t.Fatalf("list the module: %v", err)
	}
	// A package can call a gorm method on a value whose type it never names,
	// so a candidate is any package that REACHES gorm or database/sql through
	// its imports, not only one that imports it directly.
	reaches := map[string]bool{}
	var reachesPersistence func(pkg *packages.Package) bool
	reachesPersistence = func(pkg *packages.Package) bool {
		if reached, seen := reaches[pkg.PkgPath]; seen {
			return reached
		}
		reaches[pkg.PkgPath] = false
		for importPath, imported := range pkg.Imports {
			if persistenceWriteMethods[importPath] != nil || reachesPersistence(imported) {
				reaches[pkg.PkgPath] = true
				break
			}
		}
		return reaches[pkg.PkgPath]
	}
	var candidates []string
	for _, pkg := range listed {
		for _, packageError := range pkg.Errors {
			t.Fatalf("%s does not load: %v", pkg.PkgPath, packageError)
		}
		if reachesPersistence(pkg) {
			candidates = append(candidates, pkg.PkgPath)
		}
	}
	sort.Strings(candidates)
	// Anti-vacuity: the db package must be among the candidates, a package that
	// only reaches gorm transitively (internal/services, through models) too,
	// and the matcher must resolve the db package's writes, or a scan that
	// finds nothing elsewhere proves nothing.
	if !slices.Contains(candidates, module+"/internal/db") || !slices.Contains(candidates, module+"/internal/services") {
		t.Fatalf("the importer scan missed internal/db or internal/cli: %v", candidates)
	}

	typed, err := testpackages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
		Dir:  "../..",
	}, candidates...)
	if err != nil {
		t.Fatalf("type-check the importers: %v", err)
	}
	writesInDB := 0
	var violations []string
	for _, pkg := range typed {
		for _, packageError := range pkg.Errors {
			t.Fatalf("%s does not type-check: %v", pkg.PkgPath, packageError)
		}
		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				callee, ok := typeutil.Callee(pkg.TypesInfo, call).(*types.Func)
				if !ok || callee.Pkg() == nil || !persistenceWriteMethods[callee.Pkg().Path()][callee.Name()] {
					return true
				}
				if pkg.PkgPath == module+"/internal/db" {
					writesInDB++
					return true
				}
				violations = append(violations, fmt.Sprintf("%s calls %s.%s outside internal/db, where the session writer guard cannot see it", pkg.Fset.Position(call.Pos()), callee.Pkg().Path(), callee.Name()))
				return true
			})
		}
	}
	if writesInDB == 0 {
		t.Fatal("the matcher resolved no write inside internal/db: it is not measuring what it claims")
	}
	for _, violation := range violations {
		t.Error(violation)
	}
}

// fenceAdvanceMethods resolves the repositories' fence advances to their
// declarations. Both repositories that revoke must carry the refusing form.
func fenceAdvanceMethods(t *testing.T, pkg *packages.Package) map[types.Object]bool {
	t.Helper()
	methods := map[types.Object]bool{}
	for _, receiver := range []string{"UserRepository", "OIDCIdentityRepository"} {
		object := pkg.Types.Scope().Lookup(receiver)
		if object == nil {
			t.Fatalf("the db package no longer declares %s", receiver)
		}
		named, ok := object.Type().(*types.Named)
		if !ok {
			t.Fatalf("%s is not a named type", receiver)
		}
		foundRefusing := false
		for index := range named.NumMethods() {
			method := named.Method(index)
			if calendarFeedFenceAdvanceCalls[method.Name()] {
				methods[method] = true
				foundRefusing = foundRefusing || method.Name() == "advanceCalendarFeedFenceOrRefuse"
			}
		}
		if !foundRefusing {
			t.Errorf("%s has no advanceCalendarFeedFenceOrRefuse: its revoking writes cannot reach the fence", receiver)
		}
	}
	return methods
}

// divergingRevocationWriters may advance through
// advanceCalendarFeedFenceBestEffort, which moves the database half alone over
// an unwritable fence file. Every other session or credential writer must
// refuse instead (WEB-464): a divergence there leaves the file on the token a
// pre-write backup carries, and restoring that backup before the next boot
// revives what the write revoked.
var divergingRevocationWriters = map[string]string{
	"UserRepository.ClearAllDataAndResetSettings":        "an erasure; refusing it keeps health data the owner asked to have gone",
	"UserRepository.DeleteAccountAndRelatedData":         "an erasure; refusing it keeps health data the owner asked to have gone",
	"UserRepository.DeleteAccountFromAuthSessionVersion": "an erasure; refusing it keeps health data the owner asked to have gone",
}

// declarationName is Receiver.Method for a method and the bare name otherwise:
// both repositories declare a Create.
func declarationName(function *ast.FuncDecl) string {
	if function.Recv == nil || len(function.Recv.List) == 0 {
		return function.Name.Name
	}
	receiver := function.Recv.List[0].Type
	if star, ok := receiver.(*ast.StarExpr); ok {
		receiver = star.X
	}
	if ident, ok := receiver.(*ast.Ident); ok {
		return ident.Name + "." + function.Name.Name
	}
	return function.Name.Name
}

func sortedPositionNames(set map[string]token.Pos) []string {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
