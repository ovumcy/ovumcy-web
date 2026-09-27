package db

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// This file guards the invariant WEB-50/WEB-65 established: migration 041
// backfills every users.auth_session_version at or below 0 to 1 at boot, so
// no compare-and-set PREDICATE anywhere in this module may still special-case
// a legacy 0 (or below) as a match for a current version it did not verify.
// NormalizeAuthSessionVersion is deliberately out of that scope: it is a
// READER, not a predicate, and still maps <= 0 to 1 on the input side, before
// any caller builds a predicate at all — every production caller normalizes
// through it first, so the un-normalized 0 this guard refuses never reaches a
// predicate on a path production traffic takes. Two arms used to carry the
// predicate special case — authSessionVersionFromPredicate's `(? = 1 AND
// auth_session_version <= 0)` OR clause and
// UpdatePasswordRecoveryCodeAndRevokeSessionsCAS's identical arm — and both
// are gone.
//
// Two checks below the regex sweep resolve that BY DECLARATION, per this
// repository's rule that a guard identifying an OBJECT (not matching a shape
// of text) resolves it through go/packages + types.Info:
// TestAuthSessionVersionFromPredicateConstantIsPlainEquality resolves
// authSessionVersionFromPredicate as a *types.Const and reads its folded
// constant.Value — which sees through a concatenation the regex below cannot
// — and TestUpdatePasswordRecoveryCodeAndRevokeSessionsCASWhereIsPlainEquality
// resolves the method by receiver type and asserts, by name, that its
// auth_session_version Where clause was actually inspected and is the same
// plain-equality predicate. The regex sweep stays as a secondary net: it also
// catches a legacy arm reintroduced somewhere neither declaration check reads
// (migrations/*.sql or a future third call site), in a Go string literal or a
// .sql file, so a regression is caught at the text it would be written in,
// not only at the two call sites known today.
var authSessionVersionLegacyArmPattern = regexp.MustCompile(
	`auth_session_version\s*(<=\s*0|<\s*1)`,
)

// authSessionVersionPlainEqualityPredicate is the predicate WEB-50/WEB-65 left
// behind once the legacy 0-matches-1 arm was removed.
const authSessionVersionPlainEqualityPredicate = "auth_session_version = ?"

// loadAuthSessionVersionPredicatePackage type-checks internal/db's production
// syntax (no _test.go files: the subjects below are both production code) so
// the constant and the CAS method are resolved by declaration rather than by
// grep.
func loadAuthSessionVersionPredicatePackage(t *testing.T) *packages.Package {
	t.Helper()
	root, err := authSessionVersionLegacyArmModuleRoot()
	if err != nil {
		t.Fatalf("locate module root: %v", err)
	}
	config := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedImports | packages.NeedDeps,
		Dir:   root,
		Tests: false,
	}
	loaded, err := packages.Load(config, "./internal/db")
	if err != nil {
		t.Fatalf("type-checking internal/db: %v", err)
	}
	for _, pkg := range loaded {
		for _, packageError := range pkg.Errors {
			t.Fatalf("%s: %v", pkg.PkgPath, packageError)
		}
	}
	for _, pkg := range loaded {
		if pkg.PkgPath == "github.com/ovumcy/ovumcy-web/internal/db" && len(pkg.Syntax) > 0 {
			return pkg
		}
	}
	t.Fatalf("the sweep did not load internal/db with its syntax; it measured the wrong tree")
	return nil
}

// resolveAuthSessionVersionFromPredicateConstant resolves
// authSessionVersionFromPredicate BY DECLARATION — pkg.Types.Scope().Lookup,
// never a textual search — and reads its go/constant folded value, which sees
// through a string built by concatenation the same way the compiler does,
// unlike the regex sweep above.
func resolveAuthSessionVersionFromPredicateConstant(t *testing.T, pkg *packages.Package) string {
	t.Helper()
	object := pkg.Types.Scope().Lookup("authSessionVersionFromPredicate")
	if object == nil {
		t.Fatalf("internal/db declares no authSessionVersionFromPredicate constant; this guard's subject moved")
	}
	constObject, ok := object.(*types.Const)
	if !ok {
		t.Fatalf("authSessionVersionFromPredicate resolved to %T, not a declared constant", object)
	}
	if constObject.Val().Kind() != constant.String {
		t.Fatalf("authSessionVersionFromPredicate is not a string constant")
	}
	return constant.StringVal(constObject.Val())
}

// TestAuthSessionVersionFromPredicateConstantIsPlainEquality is the
// declaration-resolved half of the WEB-50/WEB-65 guard: the predicate must be
// exactly plain equality, whatever spelling produced its value.
func TestAuthSessionVersionFromPredicateConstantIsPlainEquality(t *testing.T) {
	pkg := loadAuthSessionVersionPredicatePackage(t)
	got := resolveAuthSessionVersionFromPredicateConstant(t, pkg)
	if got != authSessionVersionPlainEqualityPredicate {
		t.Fatalf("authSessionVersionFromPredicate = %q, want the plain-equality predicate %q", got, authSessionVersionPlainEqualityPredicate)
	}
}

// authSessionVersionPredicateGuardUserRepositoryNamed resolves *UserRepository
// by declaration. Duplicated in miniature from
// password_hash_writer_reachability_test.go's resolveUserRepositoryNamed
// rather than shared, per this repository's barrier-file convention of
// staying self-contained.
func authSessionVersionPredicateGuardUserRepositoryNamed(t *testing.T, pkg *packages.Package) *types.Named {
	t.Helper()
	object := pkg.Types.Scope().Lookup("UserRepository")
	if object == nil {
		t.Fatalf("internal/db declares no UserRepository type; this guard's subject moved")
	}
	typeName, ok := object.(*types.TypeName)
	if !ok {
		t.Fatalf("UserRepository did not resolve to a type declaration")
	}
	named, ok := typeName.Type().(*types.Named)
	if !ok {
		t.Fatalf("UserRepository did not resolve to a named type")
	}
	return named
}

// authSessionVersionPredicateGuardReceiverIsUserRepository resolves fn's
// receiver type through go/types and compares the resolved object, not the
// receiver's spelling.
func authSessionVersionPredicateGuardReceiverIsUserRepository(pkg *packages.Package, fn *ast.FuncDecl, subject *types.Named) bool {
	if fn.Recv == nil || len(fn.Recv.List) != 1 || len(fn.Recv.List[0].Names) != 1 {
		return false
	}
	recvObject, ok := pkg.TypesInfo.Defs[fn.Recv.List[0].Names[0]].(*types.Var)
	if !ok || recvObject == nil {
		return false
	}
	recvType := recvObject.Type()
	if pointer, ok := recvType.(*types.Pointer); ok {
		recvType = pointer.Elem()
	}
	named, ok := recvType.(*types.Named)
	return ok && named.Obj() == subject.Obj()
}

// resolveUpdatePasswordRecoveryCodeAndRevokeSessionsCASSessionVersionWhere
// finds, by name, the *UserRepository.UpdatePasswordRecoveryCodeAndRevokeSessionsCAS
// method resolved by receiver declaration, and returns the string literal of
// whichever of its Where(...) calls mentions auth_session_version — the site
// F3 requires this guard to name, not merely to have walked past.
func resolveUpdatePasswordRecoveryCodeAndRevokeSessionsCASSessionVersionWhere(t *testing.T, pkg *packages.Package) (string, bool) {
	t.Helper()
	userRepo := authSessionVersionPredicateGuardUserRepositoryNamed(t, pkg)
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "UpdatePasswordRecoveryCodeAndRevokeSessionsCAS" || fn.Body == nil {
				continue
			}
			if !authSessionVersionPredicateGuardReceiverIsUserRepository(pkg, fn, userRepo) {
				continue
			}
			literal, found := "", false
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Where" || len(call.Args) == 0 {
					return true
				}
				basic, ok := call.Args[0].(*ast.BasicLit)
				if !ok {
					return true
				}
				value, err := strconv.Unquote(basic.Value)
				if err != nil || !strings.Contains(value, "auth_session_version") {
					return true
				}
				literal, found = value, true
				return true
			})
			return literal, found
		}
	}
	return "", false
}

// TestUpdatePasswordRecoveryCodeAndRevokeSessionsCASWhereIsPlainEquality is
// the second declaration-resolved half of the guard: it names the exact
// method and asserts its auth_session_version Where clause was inspected (not
// merely that the sweep visited the file it lives in) and is the same
// plain-equality predicate as authSessionVersionFromPredicate.
func TestUpdatePasswordRecoveryCodeAndRevokeSessionsCASWhereIsPlainEquality(t *testing.T) {
	pkg := loadAuthSessionVersionPredicatePackage(t)
	literal, found := resolveUpdatePasswordRecoveryCodeAndRevokeSessionsCASSessionVersionWhere(t, pkg)
	if !found {
		t.Fatalf("did not find *UserRepository.UpdatePasswordRecoveryCodeAndRevokeSessionsCAS's auth_session_version Where clause; the guard is not inspecting the site it names")
	}
	if literal != authSessionVersionPlainEqualityPredicate {
		t.Fatalf("UpdatePasswordRecoveryCodeAndRevokeSessionsCAS's auth_session_version Where clause is %q, want the plain-equality predicate %q", literal, authSessionVersionPlainEqualityPredicate)
	}
}

// authSessionVersionLegacyArmModuleRoot walks up from the package directory to
// the module root, the same way password_hash_writer_reachability_test.go's
// own copy does — duplicated rather than shared, per this repository's
// barrier-file convention of staying self-contained.
func authSessionVersionLegacyArmModuleRoot() (string, error) {
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

// authSessionVersionLegacyArmFinding is one line the sweep flagged, kept as a
// value rather than a bare string so a failure message can name the file and
// line without re-parsing it.
type authSessionVersionLegacyArmFinding struct {
	relPath string
	line    int
	text    string
}

// scanForAuthSessionVersionLegacyArm walks root/internal, skipping
// root/migrations entirely (schema history stays as written;
// migration-immutability governs it, not this guard) and every _test.go file
// (a test may legitimately name the retired arm in a comment or as a literal
// it asserts against, the way this package's own CAS tests do), and reports
// every remaining .go or .sql file whose text still matches
// authSessionVersionLegacyArmPattern. visited collects every file inspected,
// by relative path, so the caller can prove the sweep actually reached the
// files that used to hold the arm — not merely that it found nothing.
func scanForAuthSessionVersionLegacyArm(root string) (findings []authSessionVersionLegacyArmFinding, visited []string, err error) {
	internalDir := filepath.Join(root, "internal")
	walkErr := filepath.WalkDir(internalDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		ext := filepath.Ext(name)
		if ext != ".go" && ext != ".sql" {
			return nil
		}
		if strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		visited = append(visited, rel)

		file, openErr := os.Open(path)
		if openErr != nil {
			return openErr
		}
		defer func() {
			_ = file.Close()
		}()

		scanner := bufio.NewScanner(file)
		lineNumber := 0
		for scanner.Scan() {
			lineNumber++
			line := scanner.Text()
			if authSessionVersionLegacyArmPattern.MatchString(line) {
				findings = append(findings, authSessionVersionLegacyArmFinding{
					relPath: rel,
					line:    lineNumber,
					text:    strings.TrimSpace(line),
				})
			}
		}
		if scanErr := scanner.Err(); scanErr != nil {
			return scanErr
		}
		return nil
	})
	if walkErr != nil {
		return nil, nil, walkErr
	}
	return findings, visited, nil
}

// TestNoAuthSessionVersionLegacyZeroMatchArmSurvives is the regression guard
// for WEB-50/WEB-65: neither authSessionVersionFromPredicate nor
// UpdatePasswordRecoveryCodeAndRevokeSessionsCAS (nor anything added beside
// them) may special-case a stored auth_session_version at or below 0 as a
// match for a current version, now that migration 041 makes that state
// unreachable in a freshly migrated database.
func TestNoAuthSessionVersionLegacyZeroMatchArmSurvives(t *testing.T) {
	root, err := authSessionVersionLegacyArmModuleRoot()
	if err != nil {
		t.Fatalf("locate module root: %v", err)
	}
	findings, visited, err := scanForAuthSessionVersionLegacyArm(root)
	if err != nil {
		t.Fatalf("scan for the legacy arm: %v", err)
	}

	// Anti-vacuity: prove the walk actually reached the two files that used to
	// carry the arm, by name, rather than trusting an empty findings list from
	// a walk that silently saw nothing.
	mustVisit := []string{
		"internal/db/auth_session_version_cas.go",
		"internal/db/user_repository.go",
	}
	for _, want := range mustVisit {
		found := false
		for _, got := range visited {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("sweep never visited %s (visited %d files) -- it is proving nothing", want, len(visited))
		}
	}

	if len(findings) > 0 {
		lines := make([]string, 0, len(findings))
		for _, f := range findings {
			lines = append(lines, fmt.Sprintf("%s:%d: %s", f.relPath, f.line, f.text))
		}
		t.Fatalf("a legacy auth_session_version<=0/<1 match arm reappeared outside migrations/:\n%s", strings.Join(lines, "\n"))
	}
}
