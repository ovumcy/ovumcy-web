package services

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/testpackages"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
)

const (
	oidcWriterAPIPath     = "github.com/ovumcy/ovumcy-web/internal/api"
	oidcWriterCLIPath     = "github.com/ovumcy/ovumcy-web/internal/cli"
	oidcWriterModelsPath  = "github.com/ovumcy/ovumcy-web/internal/models"
	oidcWriterFixtureFile = "zz_oidc_identity_writer_fixture.go"

	oidcWriterConfirm      = "(*" + emailGuardServicesPath + ".OIDCLoginService).ConfirmAndLinkIdentity"
	oidcWriterLinkIdentity = "(*" + emailGuardServicesPath + ".OIDCLoginService).linkIdentity"
	oidcWriterRepoCreate   = "(*" + emailGuardDBPath + ".OIDCIdentityRepository).Create"
	oidcWriterRepoRevoking = "(*" + emailGuardDBPath + ".OIDCIdentityRepository).CreateAndRevokeSessions"
)

// oidcIdentityWriterCallers names, for each step on the way to a new
// oidc_identities row, the only declarations allowed to reach it — directly
// or through any interface the receiver satisfies. SECURITY.md allows two
// ways to bind an identity to an existing account; the third writer creates
// the identity of an account the same sign-in just provisioned.
var oidcIdentityWriterCallers = map[string]map[string]string{
	oidcWriterConfirm: {
		"(*" + oidcWriterAPIPath + ".Handler).ConfirmOIDCIdentityLink": "the Settings link confirm, behind the OIDC step-up",
		oidcWriterCLIPath + ".linkOIDCIdentity":                        "the operator's link-oidc-identity command",
	},
	oidcWriterRepoRevoking: {
		oidcWriterConfirm: "the link to an existing account, which revokes its sessions in the same write",
	},
	oidcWriterRepoCreate: {
		oidcWriterLinkIdentity: "the identity of an account auto-provisioned by this sign-in",
	},
	oidcWriterLinkIdentity: {
		"(*" + emailGuardServicesPath + ".OIDCLoginService).authenticateExchange": "only past the !autoProvisioned refusal",
	},
}

// oidcIdentityRowInserters are the only declarations whose gorm calls insert
// an OIDCIdentity: the two repository methods above.
var oidcIdentityRowInserters = map[string]bool{
	oidcWriterRepoCreate:   true,
	oidcWriterRepoRevoking: true,
}

var gormInsertMethods = map[string]bool{"Create": true, "CreateInBatches": true, "Save": true, "FirstOrCreate": true}

var oidcIdentityInsertSQL = regexp.MustCompile(`(?i)insert\s+(or\s+\w+\s+)?into\s+["` + "`" + `]?oidc_identities\b`)

// findOIDCIdentityRowInserts returns, keyed by enclosing declaration, every
// gorm insert whose argument is an OIDCIdentity (value, pointer, slice or
// pointer to slice) and every string constant spelling an INSERT into
// oidc_identities.
func findOIDCIdentityRowInserts(loaded []*packages.Package) map[string]string {
	inserts := map[string]string{}
	for _, pkg := range loaded {
		models := pkg.Imports[oidcWriterModelsPath]
		var identityType types.Type
		if models != nil {
			if object := models.Types.Scope().Lookup("OIDCIdentity"); object != nil {
				identityType = object.Type()
			}
		}
		isIdentityRow := func(kind types.Type) bool {
			for kind != nil && identityType != nil {
				if types.Identical(kind, identityType) {
					return true
				}
				switch typed := kind.(type) {
				case *types.Pointer:
					kind = typed.Elem()
				case *types.Slice:
					kind = typed.Elem()
				default:
					return false
				}
			}
			return false
		}
		for _, file := range pkg.Syntax {
			for _, declaration := range file.Decls {
				site := emailGuardReference{}
				if function, ok := declaration.(*ast.FuncDecl); ok {
					site.enclosing, _ = pkg.TypesInfo.Defs[function.Name].(*types.Func)
				}
				ast.Inspect(declaration, func(node ast.Node) bool {
					switch typed := node.(type) {
					case *ast.CallExpr:
						callee, ok := typeutil.Callee(pkg.TypesInfo, typed).(*types.Func)
						if !ok || callee.Pkg() == nil || callee.Pkg().Path() != emailGuardGormPath || !gormInsertMethods[callee.Name()] {
							return true
						}
						for _, argument := range typed.Args {
							if isIdentityRow(pkg.TypesInfo.TypeOf(argument)) {
								inserts[site.enclosingName()] = pkg.Fset.Position(typed.Pos()).String()
							}
						}
					case ast.Expr:
						value := pkg.TypesInfo.Types[typed].Value
						if value != nil && value.Kind() == constant.String && oidcIdentityInsertSQL.MatchString(constant.StringVal(value)) {
							inserts[site.enclosingName()] = pkg.Fset.Position(typed.Pos()).String()
						}
					}
					return true
				})
			}
		}
	}
	return inserts
}

// unguardedLinkIdentityCalls returns the calls to linkIdentity in function
// that are not preceded, in the same top-level statement list, by an
// `if !autoProvisioned { … return … }` refusal, and how many calls it saw.
func unguardedLinkIdentityCalls(info *types.Info, function *ast.FuncDecl, linkIdentity *types.Func, fileSet *token.FileSet) (unguarded []string, calls int) {
	guarded := false
	for _, statement := range function.Body.List {
		ast.Inspect(statement, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || typeutil.Callee(info, call) != linkIdentity {
				return true
			}
			calls++
			if !guarded {
				unguarded = append(unguarded, fileSet.Position(call.Pos()).String())
			}
			return true
		})
		branch, ok := statement.(*ast.IfStmt)
		if !ok || branch.Else != nil || branch.Init != nil || len(branch.Body.List) == 0 {
			continue
		}
		not, ok := branch.Cond.(*ast.UnaryExpr)
		if !ok || not.Op != token.NOT {
			continue
		}
		flag, ok := not.X.(*ast.Ident)
		if !ok || flag.Name != "autoProvisioned" {
			continue
		}
		if _, returns := branch.Body.List[len(branch.Body.List)-1].(*ast.ReturnStmt); returns {
			guarded = true
		}
	}
	return unguarded, calls
}

// oidcWriterFuncDecl returns the declaration of target among the loaded syntax.
func oidcWriterFuncDecl(t *testing.T, loaded []*packages.Package, target *types.Func) (*packages.Package, *ast.FuncDecl) {
	t.Helper()
	for _, pkg := range loaded {
		for _, file := range pkg.Syntax {
			for _, declaration := range file.Decls {
				if function, ok := declaration.(*ast.FuncDecl); ok && pkg.TypesInfo.Defs[function.Name] == target {
					return pkg, function
				}
			}
		}
	}
	t.Fatalf("no declaration of %s in the loaded syntax", target.FullName())
	return nil, nil
}

// TestOIDCIdentityRowsHaveAClosedSetOfWriters holds SECURITY.md's claim that
// the Settings step-up and the operator CLI are the only ways to bind a new
// OIDC identity to an existing account. Resolved by declaration over the whole
// tree: who reaches ConfirmAndLinkIdentity, who reaches each repository
// insert, which declarations insert an OIDCIdentity at all, and that
// authenticateExchange calls linkIdentity only past its refusal of an account
// it did not just provision.
func TestOIDCIdentityRowsHaveAClosedSetOfWriters(t *testing.T) {
	if testpackages.Isolate(t) {
		return
	}
	loaded := withoutEmailGuardFixtures(loadEmailGuardTree(t))
	targets := map[string]*types.Func{
		oidcWriterConfirm:      lookupEmailGuardMethod(t, loaded, emailGuardServicesPath, "OIDCLoginService", "ConfirmAndLinkIdentity"),
		oidcWriterLinkIdentity: lookupEmailGuardMethod(t, loaded, emailGuardServicesPath, "OIDCLoginService", "linkIdentity"),
		oidcWriterRepoCreate:   lookupEmailGuardMethod(t, loaded, emailGuardDBPath, "OIDCIdentityRepository", "Create"),
		oidcWriterRepoRevoking: lookupEmailGuardMethod(t, loaded, emailGuardDBPath, "OIDCIdentityRepository", "CreateAndRevokeSessions"),
	}
	for _, name := range sortedKeys(oidcIdentityWriterCallers) {
		allowed := oidcIdentityWriterCallers[name]
		seen := map[string]bool{}
		for _, site := range referencesTo(loaded, targets[name]) {
			seen[site.enclosingName()] = true
			if _, ok := allowed[site.enclosingName()]; !ok {
				t.Errorf("%s reaches %s at %s: a new way to write an OIDC identity must be named in SECURITY.md and here", site.enclosingName(), name, site.at)
			}
		}
		for _, caller := range sortedKeys(allowed) {
			if !seen[caller] {
				t.Errorf("named caller %s no longer reaches %s: drop the entry", caller, name)
			}
		}
	}

	inserts := findOIDCIdentityRowInserts(loaded)
	for _, name := range sortedKeys(inserts) {
		if !oidcIdentityRowInserters[name] {
			t.Errorf("%s inserts an OIDCIdentity at %s outside the repository's two writers", name, inserts[name])
		}
	}
	for _, name := range sortedKeys(oidcIdentityRowInserters) {
		if _, ok := inserts[name]; !ok {
			t.Errorf("%s no longer inserts an OIDCIdentity: the insert scan is not measuring what it claims", name)
		}
	}

	linkIdentity := targets[oidcWriterLinkIdentity]
	pkg, authenticate := oidcWriterFuncDecl(t, loaded, lookupEmailGuardMethod(t, loaded, emailGuardServicesPath, "OIDCLoginService", "authenticateExchange"))
	unguarded, calls := unguardedLinkIdentityCalls(pkg.TypesInfo, authenticate, linkIdentity, pkg.Fset)
	if calls == 0 {
		t.Fatal("authenticateExchange no longer calls linkIdentity: the auto-provision check is not measuring what it claims")
	}
	for _, at := range unguarded {
		t.Errorf("authenticateExchange calls linkIdentity at %s without first returning on !autoProvisioned: an existing account would be linked with no confirmation", at)
	}
}

// TestOIDCIdentityWriterScanClassifiesItsOwnFixtures proves each detector on
// shapes overlaid into internal/db and internal/services: a call through an
// interface, inserts by value, slice and raw SQL, a bystander insert of
// another model, and a linkIdentity call with and without the refusal.
func TestOIDCIdentityWriterScanClassifiesItsOwnFixtures(t *testing.T) {
	if testpackages.Isolate(t) {
		return
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve module root: %v", err)
	}
	loaded, err := testpackages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
		Dir:  root,
		Overlay: map[string][]byte{
			filepath.Join(root, "internal", "db", oidcWriterFixtureFile):       []byte(oidcWriterDBFixtureSource),
			filepath.Join(root, "internal", "services", oidcWriterFixtureFile): []byte(oidcWriterServicesFixtureSource),
		},
	}, "./internal/db", "./internal/services")
	if err != nil {
		t.Fatalf("load the fixture packages: %v", err)
	}
	for _, pkg := range loaded {
		for _, packageError := range pkg.Errors {
			t.Fatalf("%s does not type-check: %v", pkg.PkgPath, packageError)
		}
	}

	callersOf := func(pkgPath, typeName, method string) map[string]bool {
		callers := map[string]bool{}
		for _, site := range referencesTo(loaded, lookupEmailGuardMethod(t, loaded, pkgPath, typeName, method)) {
			callers[site.enclosingName()] = true
		}
		return callers
	}
	if callers := callersOf(emailGuardDBPath, "OIDCIdentityRepository", "Create"); !callers[emailGuardServicesPath+".oidcWriterFixtureStoreCreate"] {
		t.Errorf("the reference scan missed a Create through OIDCIdentityStore (found %v)", sortedKeys(callers))
	}
	confirmCallers := callersOf(emailGuardServicesPath, "OIDCLoginService", "ConfirmAndLinkIdentity")
	for _, name := range []string{"oidcWriterFixtureConfirm", "oidcWriterFixtureConfirmThroughPort"} {
		if !confirmCallers[emailGuardServicesPath+"."+name] {
			t.Errorf("the reference scan missed %s (found %v)", name, sortedKeys(confirmCallers))
		}
	}

	inserts := findOIDCIdentityRowInserts(loaded)
	fixtureMethod := func(name string) string { return "(*" + emailGuardDBPath + ".OIDCIdentityRepository)." + name }
	for _, name := range []string{"oidcWriterFixtureGormCreate", "oidcWriterFixtureGormSaveSlice", "oidcWriterFixtureRawInsert", "Create", "CreateAndRevokeSessions"} {
		if _, ok := inserts[fixtureMethod(name)]; !ok {
			t.Errorf("the insert scan missed %s (found %v)", name, sortedKeys(inserts))
		}
	}
	if _, ok := inserts[fixtureMethod("oidcWriterFixtureBystanderUserCreate")]; ok {
		t.Error("oidcWriterFixtureBystanderUserCreate inserts a User, not an OIDCIdentity, and must not count")
	}

	linkIdentity := lookupEmailGuardMethod(t, loaded, emailGuardServicesPath, "OIDCLoginService", "linkIdentity")
	for name, wantUnguarded := range map[string]bool{"oidcWriterFixtureUnguardedLink": true, "oidcWriterFixtureGuardedLink": false, "oidcWriterFixtureGuardWithoutReturn": true} {
		pkg, function := oidcWriterFuncDecl(t, loaded, lookupEmailGuardMethod(t, loaded, emailGuardServicesPath, "OIDCLoginService", name))
		unguarded, calls := unguardedLinkIdentityCalls(pkg.TypesInfo, function, linkIdentity, pkg.Fset)
		if calls != 1 || (len(unguarded) == 1) != wantUnguarded {
			t.Errorf("%s: calls = %d, unguarded = %v, want one call, unguarded %v", name, calls, unguarded, wantUnguarded)
		}
	}
}

const oidcWriterDBFixtureSource = `package db

import "github.com/ovumcy/ovumcy-web/internal/models"

func (repo *OIDCIdentityRepository) oidcWriterFixtureGormCreate() {
	repo.database.Create(&models.OIDCIdentity{})
}

func (repo *OIDCIdentityRepository) oidcWriterFixtureGormSaveSlice(rows []models.OIDCIdentity) {
	repo.database.Save(&rows)
}

func (repo *OIDCIdentityRepository) oidcWriterFixtureRawInsert() {
	repo.database.Exec("INSERT INTO oidc_identities (user_id) VALUES (1)")
}

func (repo *OIDCIdentityRepository) oidcWriterFixtureBystanderUserCreate() {
	repo.database.Create(&models.User{})
}
`

const oidcWriterServicesFixtureSource = `package services

import (
	"context"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

func oidcWriterFixtureStoreCreate(ctx context.Context, store OIDCIdentityStore) {
	_ = store.Create(ctx, &models.OIDCIdentity{})
}

func oidcWriterFixtureConfirm(ctx context.Context, service *OIDCLoginService) {
	_, _ = service.ConfirmAndLinkIdentity(ctx, 1, 0, security.OIDCClaims{}, time.Time{})
}

type oidcWriterFixtureLinker interface {
	ConfirmAndLinkIdentity(ctx context.Context, targetUserID uint, expectedSessionVersion int, claims security.OIDCClaims, linkTime time.Time) (int, error)
}

func oidcWriterFixtureConfirmThroughPort(ctx context.Context, linker oidcWriterFixtureLinker) {
	_, _ = linker.ConfirmAndLinkIdentity(ctx, 1, 0, security.OIDCClaims{}, time.Time{})
}

func (service *OIDCLoginService) oidcWriterFixtureUnguardedLink(ctx context.Context, autoProvisioned bool) error {
	return service.linkIdentity(ctx, 1, security.OIDCClaims{}, time.Time{})
}

func (service *OIDCLoginService) oidcWriterFixtureGuardedLink(ctx context.Context, autoProvisioned bool) error {
	if !autoProvisioned {
		return nil
	}
	return service.linkIdentity(ctx, 1, security.OIDCClaims{}, time.Time{})
}

func (service *OIDCLoginService) oidcWriterFixtureGuardWithoutReturn(ctx context.Context, autoProvisioned bool) error {
	if !autoProvisioned {
		_ = ctx
	}
	return service.linkIdentity(ctx, 1, security.OIDCClaims{}, time.Time{})
}
`
