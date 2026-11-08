package main

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/bootstrap"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"golang.org/x/tools/go/packages"
	"golang.org/x/tools/go/types/typeutil"
)

// TestMainBuildsTheServerRepositoriesWithTheFenceMountOptions holds main()'s
// own call to the options the boot tests below exercise: main() is not run by
// any test, so a call that dropped calendarFeedFenceOptions(config)... would
// leave every behavioural test green while the image armed a fence on the
// container's writable layer. Both ends are resolved by declaration through
// the type checker — the callee as bootstrap.BuildRepositories, the variadic
// argument as this package's calendarFeedFenceOptions — never by name.
func TestMainBuildsTheServerRepositoriesWithTheFenceMountOptions(t *testing.T) {
	const bootstrapPath = "github.com/ovumcy/ovumcy-web/internal/bootstrap"
	loaded, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
	}, ".")
	if err != nil {
		t.Fatalf("type-checking cmd/ovumcy: %v", err)
	}
	if len(loaded) != 1 || len(loaded[0].Errors) > 0 {
		t.Fatalf("cmd/ovumcy must type-check as one package to resolve main()'s calls, got %d package(s): %v", len(loaded), loaded)
	}
	pkg := loaded[0]
	options := pkg.Types.Scope().Lookup("calendarFeedFenceOptions")
	if options == nil {
		t.Fatal("calendarFeedFenceOptions is not declared in cmd/ovumcy")
	}

	var mainFunc *ast.FuncDecl
	for _, file := range pkg.Syntax {
		for _, declaration := range file.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok && function.Recv == nil && function.Name.Name == "main" && function.Body != nil {
				mainFunc = function
			}
		}
	}
	if mainFunc == nil {
		t.Fatal("main() not found in cmd/ovumcy")
	}

	var builds []*ast.CallExpr
	ast.Inspect(mainFunc.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		callee := typeutil.Callee(pkg.TypesInfo, call)
		if callee != nil && callee.Pkg() != nil && callee.Pkg().Path() == bootstrapPath && callee.Name() == "BuildRepositories" {
			builds = append(builds, call)
		}
		return true
	})
	if len(builds) != 1 {
		t.Fatalf("main() must build the server's repositories through bootstrap.BuildRepositories exactly once, found %d call(s)", len(builds))
	}
	build := builds[0]
	last, ok := build.Args[len(build.Args)-1].(*ast.CallExpr)
	if !build.Ellipsis.IsValid() || !ok || typeutil.Callee(pkg.TypesInfo, last) != options {
		t.Fatalf("main() builds the server's repositories without calendarFeedFenceOptions(config)...; with %s set the start would arm a fence on the container's writable layer", security.CalendarFeedFenceRequireMountEnv)
	}
}

// fenceMountBoot loads the runtime config the way main does, with the fence at
// a fresh, writable directory, and builds the server's repositories over a
// fresh database. check stands in for the host's mount check and counts calls.
func fenceMountBoot(t *testing.T, requireMount string, verdict error) (runtimeConfig, *db.Repositories, string, *int, func() (string, error)) {
	t.Helper()
	setValidBootEnv(t)
	fenceDir := t.TempDir()
	t.Setenv(security.CalendarFeedFencePathEnv, filepath.ToSlash(filepath.Join(fenceDir, "calendar-feed.fence")))
	t.Setenv(security.CalendarFeedFenceRequireMountEnv, requireMount)
	t.Setenv("DB_PATH", filepath.Join(t.TempDir(), "fence-mount.db"))

	calls := 0
	original := calendarFeedFenceMountCheck
	calendarFeedFenceMountCheck = func(string) error {
		calls++
		return verdict
	}
	t.Cleanup(func() { calendarFeedFenceMountCheck = original })

	config, err := loadRuntimeConfig(time.UTC)
	if err != nil {
		t.Fatalf("loadRuntimeConfig: %v", err)
	}
	database, err := db.OpenDatabase(config.DatabaseConfig)
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	t.Cleanup(func() { closeDatabase(database) })
	repositories, fence := bootstrap.BuildRepositories(database, config.CalendarFeedFencePath, calendarFeedFenceOptions(config)...)
	enforce := func() (string, error) {
		return enforceCalendarFeedRestoreFence(context.Background(), fence, config.CalendarFeedFenceOptOut)
	}
	return config, repositories, fenceDir, &calls, enforce
}

// TestBootRefusesAFenceDirectoryOnTheWritableLayerBeforeAnyWrite pins WEB-506
// at the boot: with CALENDAR_FEED_FENCE_REQUIRE_MOUNT set (the image sets it)
// and the fence directory judged to be the container's writable layer, the
// start is refused through the restore fence's own refusal — naming
// CALENDAR_FEED_FENCE_PATH and the opt-out — and neither half is written: no
// fence file, no marker, no unanchored stamp.
func TestBootRefusesAFenceDirectoryOnTheWritableLayerBeforeAnyWrite(t *testing.T) {
	writableLayer := fmt.Errorf("%w: /app/fence is on the container's writable layer", security.ErrCalendarFeedFenceNotMounted)
	config, repositories, fenceDir, calls, enforce := fenceMountBoot(t, "true", writableLayer)
	if !config.CalendarFeedFenceRequireMount {
		t.Fatalf("%s=true must reach the runtime config", security.CalendarFeedFenceRequireMountEnv)
	}

	_, err := enforce()
	if !errors.Is(err, errCalendarFeedFenceRequired) {
		t.Fatalf("a fence directory on the writable layer must refuse the boot, got %v", err)
	}
	for _, want := range []string{"writable layer", security.CalendarFeedFencePathEnv, calendarFeedFenceOptOutEnv + "=true"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal must name %q, got %q", want, err)
		}
	}
	if *calls != 1 {
		t.Fatalf("the mount check must run once at boot, ran %d times", *calls)
	}
	if entries, readErr := os.ReadDir(fenceDir); readErr != nil || len(entries) != 0 {
		t.Fatalf("a refused boot must write no fence file, found %d entries (err %v)", len(entries), readErr)
	}
	for _, key := range []string{models.AppStateKeyCalendarFeedRestoreFence, models.AppStateKeyCalendarFeedFenceUnanchored} {
		if _, found, getErr := repositories.AppState.Get(context.Background(), key); getErr != nil || found {
			t.Fatalf("a refused boot must not write %s (found=%v, err=%v)", key, found, getErr)
		}
	}
}

// TestBootArmsTheFenceWhenTheMountCheckAcceptsOrIsOff is the control: the same
// boot with the directory judged mounted arms the fence, and without the flag
// the check never runs and the fence arms as it always did.
func TestBootArmsTheFenceWhenTheMountCheckAcceptsOrIsOff(t *testing.T) {
	for _, tc := range []struct {
		name         string
		requireMount string
		wantCalls    int
	}{
		{"flag set, directory mounted", "true", 1},
		{"flag unset", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refusal := fmt.Errorf("%w: a check that must not run", security.ErrCalendarFeedFenceNotMounted)
			var verdict error
			if tc.requireMount == "" {
				verdict = refusal
			}
			_, repositories, fenceDir, calls, enforce := fenceMountBoot(t, tc.requireMount, verdict)
			message, err := enforce()
			if err != nil {
				t.Fatalf("the boot must start, got %v", err)
			}
			if !strings.Contains(message, "restore fence armed") {
				t.Fatalf("the first boot must arm the fence, got %q", message)
			}
			if *calls != tc.wantCalls {
				t.Fatalf("the mount check ran %d times, want %d", *calls, tc.wantCalls)
			}
			if _, statErr := os.Stat(filepath.Join(fenceDir, "calendar-feed.fence")); statErr != nil {
				t.Fatalf("an armed fence must have its file: %v", statErr)
			}
			if marker := storedFenceMarker(t, repositories); marker == "" {
				t.Fatal("an armed fence must store its marker")
			}
		})
	}
}
