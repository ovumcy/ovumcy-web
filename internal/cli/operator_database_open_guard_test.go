package cli

import (
	"go/ast"
	"go/token"
	"go/types"
	"testing"

	"golang.org/x/tools/go/packages"
)

const (
	operatorGuardCLIPath       = "github.com/ovumcy/ovumcy-web/internal/cli"
	operatorGuardDBPath        = "github.com/ovumcy/ovumcy-web/internal/db"
	operatorGuardBootstrapPath = "github.com/ovumcy/ovumcy-web/internal/bootstrap"
)

// operatorGuardSite is one reference in the cli package: the function a
// declaration resolves to, and the function whose body (or whose function
// literal) mentions it. A package-level declaration outside any function has a
// nil enclosing.
type operatorGuardSite struct {
	callee    *types.Func
	enclosing *types.Func
}

// TestOperatorCommandsOpenTheDatabaseOnlyThroughTheSchemaCheck keeps the
// schema refusal from being bypassed by a subcommand added later: in the
// shipped cli package, every way to reach a migrated database with its
// repositories is referenced from exactly one function, and that function is
// the one that runs bootstrap.VerifySchemaInvariants under the shared storage
// budget. One pass over the package records every reference to a function
// declaration as a (callee, enclosing) pair of *types.Func objects; the rules
// and the anti-vacuity anchors below are all read from that one record.
// References are resolved by declaration through types.Info, so an alias, a
// method value, a renamed import or a same-named method on another type cannot
// hide one. `repair` opens with db.OpenDatabaseWithoutMigrations on purpose —
// it exists for a database a migration refuses — and builds no repository set,
// so it is outside this class.
func TestOperatorCommandsOpenTheDatabaseOnlyThroughTheSchemaCheck(t *testing.T) {
	t.Parallel()

	pkg := loadOperatorGuardPackage(t)
	references := collectOperatorGuardReferences(pkg)

	open := resolveOperatorGuardFunc(t, pkg, operatorGuardCLIPath, "openOperatorRepositories")
	build := resolveOperatorGuardFunc(t, pkg, operatorGuardCLIPath, "buildRepositories")
	openDatabase := resolveOperatorGuardFunc(t, pkg, operatorGuardDBPath, "OpenDatabase")
	newRepositories := resolveOperatorGuardFunc(t, pkg, operatorGuardDBPath, "NewRepositories")
	buildBootstrap := resolveOperatorGuardFunc(t, pkg, operatorGuardBootstrapPath, "BuildRepositories")
	verify := resolveOperatorGuardFunc(t, pkg, operatorGuardBootstrapPath, "VerifySchemaInvariants")
	passContext := resolveOperatorGuardFunc(t, pkg, operatorGuardBootstrapPath, "PassContext")

	// Each guarded function and the ONLY function allowed to reference it.
	// db.NewRepositories has none: every set is built through buildRepositories.
	allowed := map[*types.Func]*types.Func{
		openDatabase:    open,
		build:           open,
		buildBootstrap:  build,
		newRepositories: nil,
		verify:          open,
	}
	for site, position := range references {
		want, guarded := allowed[site.callee]
		if !guarded || (want != nil && site.enclosing == want) {
			continue
		}
		t.Errorf("%s references %s at %s: open the database through openOperatorRepositories, which refuses one the server would not boot on",
			operatorGuardName(site.enclosing), operatorGuardQualifiedName(site.callee), pkg.Fset.Position(position))
	}

	// Anti-vacuity: the load-bearing sites, each asserted by name. A scan that
	// reached none of them would report every rule above as satisfied.
	required := []operatorGuardSite{
		{openDatabase, open},
		{build, open},
		{buildBootstrap, build},
		{verify, open},
		// The schema check must run under the same storage budget as the
		// server's boot passes, or a stalled catalog read hangs the command.
		{passContext, open},
	}
	for _, command := range []string{"runUsersCommand", "runResetPasswordCommand", "runLinkOIDCIdentityCommand", "runNotifyOperatorCommand", "openWebhookCLIService"} {
		required = append(required, operatorGuardSite{open, resolveOperatorGuardFunc(t, pkg, operatorGuardCLIPath, command)})
	}
	for _, site := range required {
		if _, found := references[site]; !found {
			t.Errorf("%s was not found referencing %s: the scan is not measuring what it claims, or the command no longer goes through it",
				operatorGuardName(site.enclosing), operatorGuardQualifiedName(site.callee))
		}
	}
}

// collectOperatorGuardReferences is the single pass: every identifier in the
// package that resolves to a function declaration, keyed by that function and
// by the function declaration that encloses the identifier, with the position
// of one such reference.
func collectOperatorGuardReferences(pkg *packages.Package) map[operatorGuardSite]token.Pos {
	references := map[operatorGuardSite]token.Pos{}
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			var enclosing *types.Func
			if declared, ok := decl.(*ast.FuncDecl); ok {
				enclosing, _ = pkg.TypesInfo.Defs[declared.Name].(*types.Func)
			}
			ast.Inspect(decl, func(node ast.Node) bool {
				ident, ok := node.(*ast.Ident)
				if !ok {
					return true
				}
				callee, ok := pkg.TypesInfo.Uses[ident].(*types.Func)
				if !ok {
					return true
				}
				site := operatorGuardSite{callee: callee.Origin(), enclosing: enclosing}
				if _, seen := references[site]; !seen {
					references[site] = ident.Pos()
				}
				return true
			})
		}
	}
	return references
}

func operatorGuardName(fn *types.Func) string {
	if fn == nil {
		return "package-level declaration"
	}
	return fn.Name()
}

func operatorGuardQualifiedName(fn *types.Func) string {
	if fn.Pkg() == nil {
		return fn.Name()
	}
	return fn.Pkg().Path() + "." + fn.Name()
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

// resolveOperatorGuardFunc returns the function declared as path.name, looked
// up in the cli package itself or in a package it imports.
func resolveOperatorGuardFunc(t *testing.T, pkg *packages.Package, path string, name string) *types.Func {
	t.Helper()

	scope := pkg.Types.Scope()
	if path != operatorGuardCLIPath {
		imported, ok := pkg.Imports[path]
		if !ok {
			t.Fatalf("%s does not import %s", operatorGuardCLIPath, path)
		}
		scope = imported.Types.Scope()
	}
	fn, ok := scope.Lookup(name).(*types.Func)
	if !ok {
		t.Fatalf("%s.%s is not a declared function", path, name)
	}
	return fn
}
