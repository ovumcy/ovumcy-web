package services

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
)

const (
	emailGuardDBPath       = "github.com/ovumcy/ovumcy-web/internal/db"
	emailGuardServicesPath = "github.com/ovumcy/ovumcy-web/internal/services"
	emailGuardGormPath     = "gorm.io/gorm"
	emailGuardFixtureFile  = "zz_email_guard_fixture.go"

	emailGuardRowReader = "(*" + emailGuardDBPath + ".UserRepository).FindAllByNormalizedEmail"
)

// emailReadersByDesign are the only functions in the tree that read rows under
// an email predicate. Each says why a legacy duplicate cannot mislead it.
var emailReadersByDesign = map[string]string{
	emailGuardRowReader: "returns every match, so its one caller can refuse a duplicate",
	"(*" + emailGuardDBPath + ".UserRepository).ExistsByNormalizedEmail":              "a count: a duplicate can only confirm the address is taken",
	"(*" + emailGuardDBPath + ".UserRepository).ExistsByNormalizedEmailExcludingUser": "a count: a duplicate can only confirm the address is taken",
}

// emailResolverCallersThatMustStay are the sign-in paths WEB-13 converted. The
// completeness half of the guard is the reference scan; this list only pins
// that the paths still resolve through the check at all.
var emailResolverCallersThatMustStay = []string{
	"(*" + emailGuardServicesPath + ".AuthService).AuthenticateCredentials",
	"(*" + emailGuardServicesPath + ".AuthService).FindUserByEmailRecoveryCodeAndPassword",
	"(*" + emailGuardServicesPath + ".OIDCLoginService).findUserByEmail",
	"(*" + emailGuardServicesPath + ".OperatorUserService).GetUserByEmail",
	"(*" + emailGuardServicesPath + ".WebhookSettingsCLIService).resolveOwner",
}

// TestEveryEmailAddressedAccountLookupRefusesAnAmbiguousMatch closes the class
// at every site. A database whose idx_users_email_normalized was dropped or
// restored away outside the app can hold two accounts on one mailbox, and any
// lookup that takes the first row acts on whichever one the query reached
// first — the web sign-in paths did exactly that. Both halves are resolved by
// declaration, never by the spelling of a call:
//
//   - no function in the tree reads rows under an email predicate except the
//     named readers, and none of those can return a single row;
//   - the row-returning reader is referenced only from resolveUniqueUserByEmail,
//     whether called on *db.UserRepository itself or through any interface
//     (or constraint) it satisfies, as a call or as a method value.
func TestEveryEmailAddressedAccountLookupRefusesAnAmbiguousMatch(t *testing.T) {
	loaded := withoutEmailGuardFixtures(loadEmailGuardTree(t))

	readers := findEmailReaders(loaded)
	if _, ok := readers[emailGuardRowReader]; !ok {
		t.Fatalf("the scan did not find %s among the email readers (%v): it is not measuring what it claims", emailGuardRowReader, sortedKeys(readers))
	}
	for _, name := range sortedKeys(readers) {
		reader := readers[name]
		if reader.singleRow {
			t.Errorf("%s (%s) reads ONE row under an email predicate: on a legacy database it silently picks one of two accounts; return every match and resolve through resolveUniqueUserByEmail", name, reader.at)
		}
		if _, ok := emailReadersByDesign[name]; !ok {
			t.Errorf("%s (%s) reads rows under an email predicate and is not a named reader: resolve the account through FindAllByNormalizedEmail + resolveUniqueUserByEmail, or name it here with the reason a duplicate cannot mislead it", name, reader.at)
		}
	}
	for name := range emailReadersByDesign {
		if _, ok := readers[name]; !ok {
			t.Errorf("named reader %s no longer reads under an email predicate: drop the entry rather than leave it standing", name)
		}
	}

	rowReader := lookupEmailGuardMethod(t, loaded, emailGuardDBPath, "UserRepository", "FindAllByNormalizedEmail")
	resolver := lookupEmailGuardFunc(t, loaded, emailGuardServicesPath, "resolveUniqueUserByEmail")

	resolverReachesRowReader := false
	for _, site := range referencesTo(loaded, rowReader) {
		if site.enclosing == resolver {
			resolverReachesRowReader = true
			continue
		}
		t.Errorf("%s reaches FindAllByNormalizedEmail at %s outside resolveUniqueUserByEmail: a caller that picks from the match list itself can pick the wrong account", site.enclosingName(), site.at)
	}
	if !resolverReachesRowReader {
		t.Fatal("resolveUniqueUserByEmail was not found reading FindAllByNormalizedEmail: the reference resolution is not measuring what it claims")
	}

	callers := map[string]bool{}
	for _, site := range referencesTo(loaded, resolver) {
		callers[site.enclosingName()] = true
	}
	for _, required := range emailResolverCallersThatMustStay {
		if !callers[required] {
			t.Errorf("%s no longer resolves its account through resolveUniqueUserByEmail", required)
		}
	}
}

// TestEmailLookupGuardRecognisesItsOwnFixtures proves the detector on shapes
// the tree does not contain today: fixture files are overlaid into the db and
// services packages and type-checked with them, so each shape resolves exactly
// as a real one would.
func TestEmailLookupGuardRecognisesItsOwnFixtures(t *testing.T) {
	loaded := loadEmailGuardTree(t)

	fixtureMethod := func(name string) string { return "(*" + emailGuardDBPath + ".UserRepository)." + name }
	readers := findEmailReaders(loaded)
	for name, wantSingle := range map[string]bool{
		fixtureMethod("emailGuardFixtureFirst"):                true,
		fixtureMethod("emailGuardFixtureFindIntoStruct"):       true,
		fixtureMethod("emailGuardFixtureInlineCondition"):      true,
		fixtureMethod("emailGuardFixtureStructCondition"):      false,
		fixtureMethod("emailGuardFixtureUpperCaseColumn"):      true,
		fixtureMethod("emailGuardFixtureOtherStructCondition"): true,
		fixtureMethod("emailGuardFixtureSQLQueryRow"):          true,
		fixtureMethod("emailGuardFixtureSQLRowsScan"):          false,
		emailGuardDBPath + ".emailGuardFixtureVarInitializer":  true,
	} {
		reader, ok := readers[name]
		switch {
		case !ok:
			t.Errorf("%s reads under an email predicate but the scan did not find it", name)
		case reader.singleRow != wantSingle:
			t.Errorf("%s: singleRow = %v, want %v", name, reader.singleRow, wantSingle)
		}
	}
	if _, ok := readers[fixtureMethod("emailGuardFixtureProjectionOnly")]; ok {
		t.Error("emailGuardFixtureProjectionOnly only selects and orders by email; it filters on nothing and must not count as an email reader")
	}

	rowReader := lookupEmailGuardMethod(t, loaded, emailGuardDBPath, "UserRepository", "FindAllByNormalizedEmail")
	found := map[string]bool{}
	for _, site := range referencesTo(loaded, rowReader) {
		found[site.enclosingName()] = true
	}
	for _, name := range []string{"emailGuardFixtureThroughAnInterface", "emailGuardFixtureMethodValue", "emailGuardFixtureThroughAConstraint", "emailGuardFixtureThroughAnEmbeddedInterface", "resolveUniqueUserByEmail"} {
		if !found[emailGuardServicesPath+"."+name] {
			t.Errorf("the reference scan missed %s (found %v)", name, sortedKeys(found))
		}
	}

	for name := range findEmailReaders(withoutEmailGuardFixtures(loaded)) {
		if strings.Contains(name, "emailGuardFixture") {
			t.Errorf("%s survived the fixture filter: the tree guard would judge its own fixtures", name)
		}
	}
}

// emailGuardFixtureOverlay is type-checked into the one shared load; the tree
// guard drops these files again through withoutEmailGuardFixtures.
func emailGuardFixtureOverlay(root string) map[string][]byte {
	return map[string][]byte{
		filepath.Join(root, "internal", "db", emailGuardFixtureFile): []byte(`package db

import (
	"database/sql"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

const emailGuardFixturePredicate = "lower(trim(email)) = ?"

var emailGuardFixtureVarInitializer = func(repo *UserRepository, email string) (user models.User) {
	repo.database.Where("email = ?", email).First(&user)
	return user
}

func (repo *UserRepository) emailGuardFixtureSQLQueryRow(conn *sql.DB, email string) (id uint) {
	_ = conn.QueryRow("SELECT id FROM users WHERE email = ?", email).Scan(&id)
	return id
}

func (repo *UserRepository) emailGuardFixtureSQLRowsScan(conn *sql.DB, email string) (ids []uint) {
	rows, err := conn.Query("SELECT id FROM users WHERE email = ?", email)
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var id uint
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	return ids
}

func (repo *UserRepository) emailGuardFixtureFirst(email string) (user models.User) {
	repo.database.Where("email = ?", email).First(&user)
	return user
}

func (repo *UserRepository) emailGuardFixtureFindIntoStruct(email string) (user models.User) {
	repo.database.Where(emailGuardFixturePredicate, email).Find(&user)
	return user
}

func (repo *UserRepository) emailGuardFixtureInlineCondition(email string) (user models.User) {
	repo.database.Take(&user, "email = ?", email)
	return user
}

func (repo *UserRepository) emailGuardFixtureStructCondition(email string) (users []models.User) {
	repo.database.Where(&models.User{Email: email}).Find(&users)
	return users
}

func (repo *UserRepository) emailGuardFixtureUpperCaseColumn(email string) (user models.User) {
	repo.database.Where("LOWER(TRIM(EMAIL)) = ?", email).Last(&user)
	return user
}

func (repo *UserRepository) emailGuardFixtureOtherStructCondition(email string) (summary models.OperatorUserSummary) {
	repo.database.Model(&models.User{}).Where(&models.OperatorUserSummary{Email: email}).First(&summary)
	return summary
}

func (repo *UserRepository) emailGuardFixtureProjectionOnly() (users []models.User) {
	repo.database.Select("id", "email").Order("email").Find(&users)
	return users
}
`),
		filepath.Join(root, "internal", "services", emailGuardFixtureFile): []byte(`package services

import "context"

func emailGuardFixtureThroughAnInterface(ctx context.Context, users AuthUserRepository) {
	_, _ = users.FindAllByNormalizedEmail(ctx, "owner@example.com")
}

func emailGuardFixtureMethodValue(users OIDCUserStore) any {
	return users.FindAllByNormalizedEmail
}

func emailGuardFixtureThroughAConstraint[T normalizedEmailFinder](ctx context.Context, users T) {
	_, _ = users.FindAllByNormalizedEmail(ctx, "owner@example.com")
}

type emailGuardFixtureEmbedding struct{ WebhookOwnerReader }

func emailGuardFixtureThroughAnEmbeddedInterface(ctx context.Context, holder emailGuardFixtureEmbedding) {
	_, _ = holder.FindAllByNormalizedEmail(ctx, "owner@example.com")
}
`),
	}
}

type emailReader struct {
	at        string
	singleRow bool
}

// emailColumn matches the column as an identifier in any case, not as a
// substring of another one (email_verified, emails).
var emailColumn = regexp.MustCompile(`(?i)(^|[^a-z0-9_])email([^a-z0-9_]|$)`)

var (
	// queryRowReads are the calls that return rows to Go, in gorm and in
	// database/sql alike.
	queryRowReads = map[string]bool{
		"First": true, "Take": true, "Last": true, "Find": true, "FindInBatches": true,
		"Scan": true, "ScanRows": true, "Pluck": true, "Count": true, "Row": true, "Rows": true,
		"FirstOrInit": true, "FirstOrCreate": true,
		"Query": true, "QueryContext": true, "QueryRow": true, "QueryRowContext": true,
	}
	// querySingleRowReads return one row whatever matched.
	querySingleRowReads = map[string]bool{
		"First": true, "Take": true, "Last": true, "Row": true,
		"FirstOrInit": true, "FirstOrCreate": true,
		"QueryRow": true, "QueryRowContext": true,
	}
	// queryDestinationReads return one row when handed a non-slice
	// destination — in gorm only: database/sql's and pgx's Scan copy the
	// current row of a query already counted above.
	queryDestinationReads = map[string]bool{"Find": true, "Scan": true}
	// queryProjections name columns without filtering on them.
	queryProjections = map[string]bool{
		"Select": true, "Omit": true, "Order": true, "Group": true, "Distinct": true,
		"Model": true, "Table": true, "Pluck": true,
	}
)

// findEmailReaders returns every function that reads rows through gorm,
// database/sql or pgx with the email column in a filtering argument. A column
// is recognised as a constant-valued string (a literal, a named constant, a
// constant expression, or a constant nested in a built argument) or as a
// struct field named Email — gorm's column email — in a struct condition.
//
// Deliberately narrower than "every email-keyed read": the unit is one
// function declaration or one package-level variable initializer (a func
// literal assigned there included), so a query whose predicate is built in one
// function and read in another (a helper returning *gorm.DB, a named Scope) is
// not joined up, and a column name held in a non-constant variable is not
// seen. Neither shape exists in the tree; each would pass this scan.
func findEmailReaders(loaded []*packages.Package) map[string]emailReader {
	readers := map[string]emailReader{}
	record := func(pkg *packages.Package, name string, node ast.Node) {
		namesEmail, reads, singleRow := scanEmailRead(pkg.TypesInfo, node)
		if namesEmail && reads {
			readers[name] = emailReader{at: pkg.Fset.Position(node.Pos()).String(), singleRow: singleRow}
		}
	}
	for _, pkg := range loaded {
		for _, file := range pkg.Syntax {
			for _, declaration := range file.Decls {
				switch typed := declaration.(type) {
				case *ast.FuncDecl:
					if object, ok := pkg.TypesInfo.Defs[typed.Name].(*types.Func); ok && typed.Body != nil {
						record(pkg, object.FullName(), typed.Body)
					}
				case *ast.GenDecl:
					for _, spec := range typed.Specs {
						value, ok := spec.(*ast.ValueSpec)
						if !ok {
							continue
						}
						for index, initializer := range value.Values {
							name := value.Names[min(index, len(value.Names)-1)]
							record(pkg, pkg.PkgPath+"."+name.Name, initializer)
						}
					}
				}
			}
		}
	}
	return readers
}

func scanEmailRead(info *types.Info, body ast.Node) (namesEmail bool, reads bool, singleRow bool) {
	var visit func(node ast.Node, filtering bool)
	visit = func(node ast.Node, filtering bool) {
		ast.Inspect(node, func(current ast.Node) bool {
			if expression, ok := current.(ast.Expr); ok && filtering && namesEmailColumn(info, expression) {
				namesEmail = true
			}
			call, ok := current.(*ast.CallExpr)
			if !ok {
				return true
			}
			callee, ok := typeutil.Callee(info, call).(*types.Func)
			if !ok || !isQueryAPI(callee) {
				return true
			}
			name := callee.Name()
			reads = reads || queryRowReads[name]
			singleRow = singleRow || querySingleRowReads[name] ||
				(callee.Pkg().Path() == emailGuardGormPath && queryDestinationReads[name] && len(call.Args) > 0 && !pointsToSlice(info.TypeOf(call.Args[0])))
			visit(call.Fun, filtering)
			for _, argument := range call.Args {
				visit(argument, filtering || !queryProjections[name])
			}
			return false
		})
	}
	visit(body, false)
	return namesEmail, reads, singleRow
}

func namesEmailColumn(info *types.Info, expression ast.Expr) bool {
	if identifier, ok := expression.(*ast.Ident); ok {
		if field, ok := info.Uses[identifier].(*types.Var); ok && field.IsField() && field.Name() == "Email" {
			return true
		}
	}
	value := info.Types[expression].Value
	return value != nil && value.Kind() == constant.String && emailColumn.MatchString(constant.StringVal(value))
}

func isQueryAPI(callee *types.Func) bool {
	if callee.Pkg() == nil {
		return false
	}
	path := callee.Pkg().Path()
	return path == emailGuardGormPath || path == "database/sql" || strings.HasPrefix(path, "github.com/jackc/pgx/")
}

func pointsToSlice(kind types.Type) bool {
	if kind == nil {
		return false
	}
	if pointer, ok := kind.Underlying().(*types.Pointer); ok {
		kind = pointer.Elem()
	}
	_, ok := kind.Underlying().(*types.Slice)
	return ok
}

type emailGuardReference struct {
	at        string
	enclosing *types.Func
}

func (site emailGuardReference) enclosingName() string {
	if site.enclosing == nil {
		return "a package-level declaration"
	}
	return site.enclosing.FullName()
}

// referencesTo returns every selector in the loaded packages that resolves to
// target: the method itself, or an interface or constraint method that a
// value of target's receiver type satisfies with target.
func referencesTo(loaded []*packages.Package, target *types.Func) []emailGuardReference {
	receiver := target.Signature().Recv()
	var sites []emailGuardReference
	for _, pkg := range loaded {
		for _, file := range pkg.Syntax {
			for _, declaration := range file.Decls {
				var enclosing *types.Func
				if function, ok := declaration.(*ast.FuncDecl); ok {
					enclosing, _ = pkg.TypesInfo.Defs[function.Name].(*types.Func)
				}
				ast.Inspect(declaration, func(node ast.Node) bool {
					switch typed := node.(type) {
					case *ast.SelectorExpr:
						if selection, ok := pkg.TypesInfo.Selections[typed]; ok && selectionReaches(selection.Obj(), receiver, target) {
							sites = append(sites, emailGuardReference{at: pkg.Fset.Position(typed.Pos()).String(), enclosing: enclosing})
						}
					case *ast.Ident:
						if pkg.TypesInfo.Uses[typed] == target && receiver == nil {
							sites = append(sites, emailGuardReference{at: pkg.Fset.Position(typed.Pos()).String(), enclosing: enclosing})
						}
					}
					return true
				})
			}
		}
	}
	return sites
}

// selectionReaches reads the interface off the selected method's own receiver,
// not off the selector's operand: a struct embedding the interface is the
// operand there, and the method still dispatches to target.
func selectionReaches(selected types.Object, receiver *types.Var, target *types.Func) bool {
	if selected == target {
		return true
	}
	method, ok := selected.(*types.Func)
	if !ok || receiver == nil || method.Name() != target.Name() || method.Signature().Recv() == nil {
		return false
	}
	iface, ok := method.Signature().Recv().Type().Underlying().(*types.Interface)
	if !ok || !types.Implements(receiver.Type(), iface) {
		return false
	}
	concrete, _, _ := types.LookupFieldOrMethod(receiver.Type(), true, method.Pkg(), method.Name())
	return concrete == target
}

func lookupEmailGuardMethod(t *testing.T, loaded []*packages.Package, pkgPath string, typeName string, method string) *types.Func {
	t.Helper()
	pkg := emailGuardPackage(t, loaded, pkgPath)
	named := pkg.Types.Scope().Lookup(typeName)
	if named == nil {
		t.Fatalf("%s declares no %s", pkgPath, typeName)
	}
	object, _, _ := types.LookupFieldOrMethod(types.NewPointer(named.Type()), true, pkg.Types, method)
	resolved, ok := object.(*types.Func)
	if !ok {
		t.Fatalf("%s.%s has no method %s", pkgPath, typeName, method)
	}
	return resolved
}

func lookupEmailGuardFunc(t *testing.T, loaded []*packages.Package, pkgPath string, name string) *types.Func {
	t.Helper()
	resolved, ok := emailGuardPackage(t, loaded, pkgPath).Types.Scope().Lookup(name).(*types.Func)
	if !ok {
		t.Fatalf("%s declares no function %s", pkgPath, name)
	}
	return resolved
}

func emailGuardPackage(t *testing.T, loaded []*packages.Package, pkgPath string) *packages.Package {
	t.Helper()
	for _, pkg := range loaded {
		if pkg.PkgPath == pkgPath {
			return pkg
		}
	}
	t.Fatalf("%s was not loaded", pkgPath)
	return nil
}

var (
	emailGuardLoadOnce sync.Once
	emailGuardLoaded   []*packages.Package
	emailGuardLoadErr  error
)

// loadEmailGuardTree type-checks the tree's production sources with the
// fixture overlay, once for both guard tests. It fails closed: a package that
// does not type-check resolves nothing, and a guard that resolves nothing
// passes over everything.
func loadEmailGuardTree(t *testing.T) []*packages.Package {
	t.Helper()
	emailGuardLoadOnce.Do(func() {
		emailGuardLoaded, emailGuardLoadErr = loadEmailGuardPackages()
	})
	if emailGuardLoadErr != nil {
		t.Fatal(emailGuardLoadErr)
	}
	return emailGuardLoaded
}

func loadEmailGuardPackages() ([]*packages.Package, error) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		return nil, fmt.Errorf("resolve module root: %w", err)
	}
	patterns := []string{"./cmd/...", "./internal/...", "./migrations/...", "./scripts/...", "./web/..."}
	config := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
		Dir:     root,
		Overlay: emailGuardFixtureOverlay(root),
		Tests:   false,
	}
	loaded, err := packages.Load(config, patterns...)
	if err != nil {
		return nil, fmt.Errorf("load %v: %w", patterns, err)
	}
	var failures []string
	for _, pkg := range loaded {
		for _, packageError := range pkg.Errors {
			failures = append(failures, fmt.Sprintf("%s: %v", pkg.PkgPath, packageError))
		}
	}
	if len(failures) > 0 {
		return nil, fmt.Errorf("the tree does not type-check:\n%v", failures)
	}
	return loaded, nil
}

// withoutEmailGuardFixtures drops the overlaid fixture files from each
// package's syntax, leaving the shared type information intact, so the tree
// guard scans only real sources.
func withoutEmailGuardFixtures(loaded []*packages.Package) []*packages.Package {
	filtered := make([]*packages.Package, 0, len(loaded))
	for _, pkg := range loaded {
		view := *pkg
		view.Syntax = nil
		for _, file := range pkg.Syntax {
			if filepath.Base(pkg.Fset.Position(file.Package).Filename) != emailGuardFixtureFile {
				view.Syntax = append(view.Syntax, file)
			}
		}
		filtered = append(filtered, &view)
	}
	return filtered
}
