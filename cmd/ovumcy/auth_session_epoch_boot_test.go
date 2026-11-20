package main

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

type epochBootStore struct {
	values map[string]string
	getErr error
	setErr error
}

func (store *epochBootStore) Get(_ context.Context, key string) (string, bool, error) {
	value, found := store.values[key]
	return value, found, store.getErr
}

func (store *epochBootStore) Set(_ context.Context, key string, value string) error {
	if store.setErr != nil {
		return store.setErr
	}
	store.values[key] = value
	return nil
}

func (store *epochBootStore) SetIfAbsent(_ context.Context, key string, value string) (bool, error) {
	if store.setErr != nil {
		return false, store.setErr
	}
	if _, exists := store.values[key]; exists {
		return false, nil
	}
	store.values[key] = value
	return true, nil
}

func TestLoadAuthSessionEpochReportsCreatedExistingAndStoreFailure(t *testing.T) {
	store := &epochBootStore{values: map[string]string{}}

	epoch, message, err := loadAuthSessionEpoch(context.Background(), store)
	if err != nil || !security.ValidAuthSessionEpoch(epoch) || !strings.Contains(message, "created") {
		t.Fatalf("a boot with no epoch must create one and say so, got %q %q %v", epoch, message, err)
	}
	if store.values[models.AppStateKeyAuthSessionEpoch] != epoch {
		t.Fatal("the created epoch must be stored")
	}

	again, message, err := loadAuthSessionEpoch(context.Background(), store)
	if err != nil || again != epoch || message != "" {
		t.Fatalf("a later boot must serve the stored epoch quietly, got %q %q %v", again, message, err)
	}

	for name, failing := range map[string]*epochBootStore{
		"read":  {values: map[string]string{}, getErr: errors.New("read refused")},
		"write": {values: map[string]string{}, setErr: errors.New("write refused")},
	} {
		if epoch, _, err := loadAuthSessionEpoch(context.Background(), failing); err == nil || epoch != "" {
			t.Fatalf("a failed %s must fail the boot with no epoch, got %q %v", name, epoch, err)
		}
	}
}

// The epoch is loaded after the restore fence pass and before the dependencies
// are built: the fence's revocation rotates the stored value, so a load before
// it would serve the pre-restore epoch, and dependencies built before the load
// would hold none.
func TestMainLoadsTheSessionEpochAfterTheRestoreFenceAndBeforeTheDependencies(t *testing.T) {
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
			name := ""
			switch fun := call.Fun.(type) {
			case *ast.Ident:
				name = fun.Name
			case *ast.SelectorExpr:
				name = fun.Sel.Name
			}
			if _, seen := positions[name]; name != "" && !seen {
				positions[name] = call.Pos()
			}
			return true
		})
	}
	fence, load, build := positions["mustEnforceCalendarFeedRestoreFence"], positions["mustLoadAuthSessionEpoch"], positions["BuildDependencies"]
	if fence == token.NoPos || load == token.NoPos || build == token.NoPos {
		t.Fatalf("main() must call mustEnforceCalendarFeedRestoreFence, mustLoadAuthSessionEpoch and BuildDependencies (fence=%v, load=%v, build=%v)", fence, load, build)
	}
	if fence > load {
		t.Fatal("main() must call mustEnforceCalendarFeedRestoreFence before mustLoadAuthSessionEpoch")
	}
	if load > build {
		t.Fatal("main() must call mustLoadAuthSessionEpoch before BuildDependencies")
	}
}
