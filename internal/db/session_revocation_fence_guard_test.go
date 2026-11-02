package db

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"regexp"
	"sort"
	"testing"

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
	rowTypes := map[types.Type]bool{}
	models := pkg.Imports["github.com/ovumcy/ovumcy-web/internal/models"]
	for _, name := range revocationRowTypes {
		object := models.Types.Scope().Lookup(name)
		if object == nil {
			t.Fatalf("internal/models declares no %s", name)
		}
		rowTypes[types.NewPointer(object.Type())] = true
	}

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
	for _, file := range pkg.Syntax {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			name := declarationName(function)
			writeAt, advanceAt := token.NoPos, token.NoPos
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
						earliest(&advanceAt, typed.Pos())
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
							if len(typed.Args) > 0 && rowTypes[info.TypeOf(typed.Args[0])] {
								earliest(&writeAt, typed.Pos())
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
			advancing[name] = true
			firstWrite := writeAt
			if transactionAt != token.NoPos && transactionAt < firstWrite {
				firstWrite = transactionAt
			}
			advancesFirst[name] = advanceAt < firstWrite
		}
	}

	for _, required := range loadBearingRevocationWriters {
		if _, found := writers[required]; !found {
			t.Fatalf("the scan did not find %s among the session and credential writers (%v): it is not measuring what it claims", required, sortedPositionNames(writers))
		}
	}

	var violations []string
	for _, name := range sortedPositionNames(writers) {
		if _, exempt := exemptRevocationWriters[name]; exempt {
			continue
		}
		at := pkg.Fset.Position(writers[name])
		switch {
		case !advancing[name]:
			violations = append(violations, fmt.Sprintf("%s (%s) revokes a session or changes a credential but never advances the restore fence, so a backup restore would undo it unseen", name, at))
		case !advancesFirst[name]:
			violations = append(violations, fmt.Sprintf("%s (%s) must advance the fence BEFORE its write, or a crash between the two leaves the revocation recorded only in the database", name, at))
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

// fenceAdvanceMethods resolves the repositories' fence advances to their
// declarations. Both repositories that revoke must carry the best-effort form.
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
		foundBestEffort := false
		for index := range named.NumMethods() {
			method := named.Method(index)
			if calendarFeedFenceAdvanceCalls[method.Name()] {
				methods[method] = true
				foundBestEffort = foundBestEffort || method.Name() == "advanceCalendarFeedFenceBestEffort"
			}
		}
		if !foundBestEffort {
			t.Errorf("%s has no advanceCalendarFeedFenceBestEffort: its revoking writes cannot reach the fence", receiver)
		}
	}
	return methods
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
