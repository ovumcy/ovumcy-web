package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestMustProbeDatabaseWritablePassesAWritableStoreAndCommitsNothing(t *testing.T) {
	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "write-probe-boot.db")})
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	t.Cleanup(func() { closeDatabase(database) })
	repositories := db.NewRepositories(database)

	mustProbeDatabaseWritable(repositories)

	if _, found, err := repositories.AppState.Get(context.Background(), models.AppStateKeyWriteProbe); err != nil || found {
		t.Fatalf("the boot probe must commit nothing (found=%v, err=%v)", found, err)
	}
}

// The probe must precede the restore fence pass: the fence advances its file
// half before the database half, so a fence pass over a store that refuses
// writes leaves the halves apart and the next boot reads a restore.
func TestMainProbesTheDatabaseBeforeTheRestoreFencePass(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	positions := map[string]token.Pos{}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "main" || fn.Recv != nil {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if ident, ok := call.Fun.(*ast.Ident); ok {
				if _, seen := positions[ident.Name]; !seen {
					positions[ident.Name] = call.Pos()
				}
			}
			return true
		})
	}
	probe, fence := positions["mustProbeDatabaseWritable"], positions["mustEnforceCalendarFeedRestoreFence"]
	if probe == token.NoPos || fence == token.NoPos {
		t.Fatalf("main() must call both mustProbeDatabaseWritable and mustEnforceCalendarFeedRestoreFence (probe=%v, fence=%v)", probe, fence)
	}
	if probe > fence {
		t.Fatal("main() must call mustProbeDatabaseWritable before mustEnforceCalendarFeedRestoreFence")
	}
}
