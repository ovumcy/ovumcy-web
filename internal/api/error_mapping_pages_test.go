package api

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func TestMapCalendarViewError(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want APIErrorSpec
	}{
		{
			name: "load logs",
			err:  services.ErrCalendarViewLoadLogs,
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load calendar"),
		},
		{
			name: "load stats",
			err:  services.ErrCalendarViewLoadStats,
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load stats"),
		},
		{
			name: "unknown",
			err:  errors.New("unknown"),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load calendar page"),
		},
	}

	for _, testCase := range testCases {

		t.Run(testCase.name, func(t *testing.T) {
			if got := mapCalendarViewError(testCase.err); got != testCase.want {
				t.Fatalf("unexpected mapped error: got %#v want %#v", got, testCase.want)
			}
		})
	}
}

func TestMapDashboardViewError(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want APIErrorSpec
	}{
		{
			name: "load today log",
			err:  services.ErrDashboardViewLoadTodayLog,
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load today log"),
		},
		{
			name: "load logs",
			err:  services.ErrDashboardViewLoadLogs,
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load symptom history"),
		},
		{
			name: "unknown",
			err:  errors.New("unknown"),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load logs"),
		},
	}

	for _, testCase := range testCases {

		t.Run(testCase.name, func(t *testing.T) {
			if got := mapDashboardViewError(testCase.err); got != testCase.want {
				t.Fatalf("unexpected mapped error: got %#v want %#v", got, testCase.want)
			}
		})
	}
}

func TestMapDayEditorViewError(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want APIErrorSpec
	}{
		{
			name: "load day state",
			err:  services.ErrDashboardViewLoadDayState,
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load day state"),
		},
		{
			name: "load day log",
			err:  services.ErrDashboardViewLoadDayLog,
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load day log"),
		},
		{
			name: "load symptom history",
			err:  services.ErrDashboardViewLoadLogs,
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load symptom history"),
		},
		{
			name: "unknown",
			err:  errors.New("unknown"),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load day"),
		},
	}

	for _, testCase := range testCases {

		t.Run(testCase.name, func(t *testing.T) {
			if got := mapDayEditorViewError(testCase.err); got != testCase.want {
				t.Fatalf("unexpected mapped error: got %#v want %#v", got, testCase.want)
			}
		})
	}
}

func TestMapStatsPageViewError(t *testing.T) {
	testCases := []struct {
		name string
		err  error
		want APIErrorSpec
	}{
		{
			name: "load symptoms",
			err:  services.ErrStatsPageViewLoadSymptoms,
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load symptom stats"),
		},
		{
			name: "unknown",
			err:  errors.New("unknown"),
			want: globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to load stats"),
		},
	}

	for _, testCase := range testCases {

		t.Run(testCase.name, func(t *testing.T) {
			if got := mapStatsPageViewError(testCase.err); got != testCase.want {
				t.Fatalf("unexpected mapped error: got %#v want %#v", got, testCase.want)
			}
		})
	}
}

func TestStatsFetchErrorSpec(t *testing.T) {
	want := globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "failed to fetch stats")
	if got := statsFetchErrorSpec(); got != want {
		t.Fatalf("unexpected mapped error: got %#v want %#v", got, want)
	}
}

// calendarViewSentinelValues is the test's mirror of the sentinels
// BuildCalendarPageViewData can return. A mirror agrees with the
// implementation by construction, so it is never the source of truth here:
// the parse below reads the producer's own declarations and fails when this
// map does not name every one of them.
func calendarViewSentinelValues() map[string]error {
	return map[string]error{
		"ErrCalendarViewLoadLogs":  services.ErrCalendarViewLoadLogs,
		"ErrCalendarViewLoadStats": services.ErrCalendarViewLoadStats,
	}
}

// exportedErrorSentinelNames parses one services source file and returns every
// exported `Err…` package-level variable it declares. Reading the producer's
// source — rather than re-listing the sentinels here — is what makes the
// exhaustiveness claim below survive a third sentinel being added.
func exportedErrorSentinelNames(t *testing.T, path string) []string {
	t.Helper()

	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	var names []string
	for _, declaration := range parsed.Decls {
		generic, ok := declaration.(*ast.GenDecl)
		if !ok || generic.Tok != token.VAR {
			continue
		}
		for _, spec := range generic.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range value.Names {
				if strings.HasPrefix(name.Name, "Err") && ast.IsExported(name.Name) {
					names = append(names, name.Name)
				}
			}
		}
	}
	sort.Strings(names)
	return names
}

// TestMapCalendarViewErrorNamesEverySentinelItsProducerDeclares is the
// exhaustiveness barrier for the calendar mapper. `default` is not a mapping:
// a sentinel served by it gets whichever message the last-written arm happens
// to carry, which is how a calendar failure came to answer "failed to load
// stats". The check is that every declared sentinel resolves to a key of its
// own AND to a key the unmapped-error fallback does not use, so a third
// sentinel added to BuildCalendarPageViewData fails here instead of silently
// inheriting a message about a different subsystem.
//
// Its sibling mapDashboardViewError names all three of its sentinels; this
// keeps the two mappers in one file following one rule.
func TestMapCalendarViewErrorNamesEverySentinelItsProducerDeclares(t *testing.T) {
	declared := exportedErrorSentinelNames(t, filepath.Join("..", "services", "calendar_view_service.go"))
	if len(declared) < 2 {
		t.Fatalf("parsed %d sentinels from calendar_view_service.go; the producer declares more, so this sweep read the wrong file", len(declared))
	}

	mirrored := calendarViewSentinelValues()
	for _, name := range declared {
		if _, ok := mirrored[name]; !ok {
			t.Fatalf("services.%s is declared by the calendar view service but this test does not carry it; add it here and to mapCalendarViewError", name)
		}
	}

	fallbackKey := mapCalendarViewError(errors.New("an error no arm names")).Key
	keysBySentinel := map[string]string{}
	for _, name := range declared {
		// Wrapped exactly the way the producer wraps it, so the mapper is
		// exercised through errors.Is rather than through identity.
		spec := mapCalendarViewError(fmt.Errorf("%w: %v", mirrored[name], errors.New("boom")))
		if spec.Key == fallbackKey {
			t.Errorf("services.%s falls through to the mapper's default arm (key %q); name it in mapCalendarViewError", name, spec.Key)
			continue
		}
		if owner, taken := keysBySentinel[spec.Key]; taken {
			t.Errorf("services.%s and services.%s both map to key %q; an operator cannot tell the two failures apart", name, owner, spec.Key)
			continue
		}
		keysBySentinel[spec.Key] = name
	}
}
