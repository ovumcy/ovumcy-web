package cli

import (
	"go/ast"
	"go/types"
	"sort"
	"testing"

	"golang.org/x/tools/go/packages"
)

const (
	operatorGuardCLIPath       = "github.com/ovumcy/ovumcy-web/internal/cli"
	operatorGuardDBPath        = "github.com/ovumcy/ovumcy-web/internal/db"
	operatorGuardBootstrapPath = "github.com/ovumcy/ovumcy-web/internal/bootstrap"
)

// TestOperatorCommandsOpenTheDatabaseOnlyThroughTheSchemaCheck keeps the
// schema refusal from being bypassed by a subcommand added later: in the
// shipped cli package, every way to reach a migrated database with its
// repositories is referenced from exactly one function, and that function is
// the one that runs bootstrap.VerifySchemaInvariants. References are resolved
// by declaration through types.Info, so an alias, a method value or a renamed
// import cannot hide one. `repair` opens with db.OpenDatabaseWithoutMigrations
// on purpose — it exists for a database a migration refuses — and builds no
// repository set, so it is outside this class.
func TestOperatorCommandsOpenTheDatabaseOnlyThroughTheSchemaCheck(t *testing.T) {
	t.Parallel()

	pkg := loadOperatorGuardPackage(t)
	allowed := map[string]string{
		operatorGuardDBPath + ".OpenDatabase":             "openOperatorRepositories",
		operatorGuardCLIPath + ".buildRepositories":       "openOperatorRepositories",
		operatorGuardBootstrapPath + ".BuildRepositories": "buildRepositories",
		operatorGuardDBPath + ".NewRepositories":          "",
	}

	found := map[string]bool{}
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			enclosing := "package-level declaration"
			if fn, ok := decl.(*ast.FuncDecl); ok {
				enclosing = fn.Name.Name
			}
			ast.Inspect(decl, func(node ast.Node) bool {
				ident, ok := node.(*ast.Ident)
				if !ok {
					return true
				}
				fn, ok := pkg.TypesInfo.Uses[ident].(*types.Func)
				if !ok || fn.Pkg() == nil {
					return true
				}
				name := fn.Pkg().Path() + "." + fn.Name()
				want, guarded := allowed[name]
				if !guarded {
					return true
				}
				if enclosing != want {
					t.Errorf("%s references %s at %s: open the database through openOperatorRepositories, which refuses one the server would not boot on", enclosing, name, pkg.Fset.Position(ident.Pos()))
					return true
				}
				found[name] = true
				return true
			})
		}
	}
	for name, want := range allowed {
		if want != "" && !found[name] {
			t.Errorf("%s was not found referencing %s: the scan is not measuring what it claims", want, name)
		}
	}

	verify := lookupOperatorGuardFunc(t, operatorGuardBootstrapPath, "VerifySchemaInvariants", pkg)
	open := pkg.Types.Scope().Lookup("openOperatorRepositories")
	if open == nil {
		t.Fatal("openOperatorRepositories is not declared in the cli package")
	}
	if callers := referencingFunctions(pkg, verify); len(callers) != 1 || callers[0] != "openOperatorRepositories" {
		t.Errorf("bootstrap.VerifySchemaInvariants is referenced from %v, want only openOperatorRepositories", callers)
	}

	callers := map[string]bool{}
	for _, name := range referencingFunctions(pkg, open) {
		callers[name] = true
	}
	for _, entry := range []string{"runUsersCommand", "runResetPasswordCommand", "runLinkOIDCIdentityCommand", "RunNotifyCommand", "openWebhookCLIService"} {
		if !callers[entry] {
			t.Errorf("%s no longer opens its database through openOperatorRepositories", entry)
		}
	}
}

func loadOperatorGuardPackage(t *testing.T) *packages.Package {
	t.Helper()

	loaded, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedSyntax | packages.NeedImports | packages.NeedDeps,
	}, operatorGuardCLIPath)
	if err != nil {
		t.Fatalf("load %s: %v", operatorGuardCLIPath, err)
	}
	if len(loaded) != 1 || len(loaded[0].Errors) != 0 {
		t.Fatalf("load %s: %d packages, errors %v", operatorGuardCLIPath, len(loaded), loaded[0].Errors)
	}
	return loaded[0]
}

func lookupOperatorGuardFunc(t *testing.T, path string, name string, pkg *packages.Package) types.Object {
	t.Helper()

	imported, ok := pkg.Imports[path]
	if !ok {
		t.Fatalf("%s does not import %s", operatorGuardCLIPath, path)
	}
	object := imported.Types.Scope().Lookup(name)
	if object == nil {
		t.Fatalf("%s.%s is not declared", path, name)
	}
	return object
}

func referencingFunctions(pkg *packages.Package, target types.Object) []string {
	names := map[string]bool{}
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			enclosing := "package-level declaration"
			if fn, ok := decl.(*ast.FuncDecl); ok {
				enclosing = fn.Name.Name
			}
			ast.Inspect(decl, func(node ast.Node) bool {
				if ident, ok := node.(*ast.Ident); ok && pkg.TypesInfo.Uses[ident] == target {
					names[enclosing] = true
				}
				return true
			})
		}
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	return sorted
}
