package cli

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// armOperatorFence points CALENDAR_FEED_FENCE_PATH at a file this test owns
// and runs the one Enforce pass a first boot performs against databasePath,
// so both fence halves already agree by the time a subcommand under test
// calls confirmOperatorFeedRevocation. It returns the fence file's path, for
// a test that needs to disturb it afterward (simulating a restore).
//
// t.Setenv panics after t.Parallel: every caller of this helper runs without
// t.Parallel for that reason.
func armOperatorFence(t *testing.T, databasePath string) string {
	t.Helper()

	fencePath := filepath.Join(t.TempDir(), "calendar-feed.fence")
	t.Setenv(security.CalendarFeedFencePathEnv, fencePath)

	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("armOperatorFence: open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("armOperatorFence: open sql db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	_, fence := buildRepositories(database)
	if _, err := fence.Enforce(context.Background()); err != nil {
		t.Fatalf("armOperatorFence: Enforce: %v", err)
	}
	return fencePath
}

// TestRootedPathAcceptsTheRootsFilepathIsAbsMisses runs on the classifier
// directly, touching no filesystem. The two rooted shapes are deliberate,
// because one of them alone would guard only the platform it was written on.
// A leading slash is IsAbs on Linux, so putting filepath.IsAbs back would
// still pass there — on a Linux CI the regression would go through green. A
// leading backslash is IsAbs on NEITHER platform, so it is the case that
// fails everywhere the suite runs.
func TestRootedPathAcceptsTheRootsFilepathIsAbsMisses(t *testing.T) {
	for _, fencePath := range []string{
		"/app/fence/calendar-feed.fence",
		`\app\fence\calendar-feed.fence`,
	} {
		if !rootedPath(fencePath) {
			t.Fatalf("%q names a location no working directory changes: judging it by filepath.IsAbs alone silences the check on the value an operator copies out of the compose file", fencePath)
		}
	}
	if rootedPath(filepath.Join("state", "calendar-feed.fence")) {
		t.Fatal("a path with no root must stay unjudged: it resolves against a working directory that is not the server's")
	}
}

// fakeConfirmFenceAnchor and fakeConfirmFenceAppState let this package build a
// real *services.CalendarFeedRestoreFence over doubles it controls, without
// touching a filesystem or a database: Go interface satisfaction is
// structural, so a type defined here satisfies services' unexported anchor
// and app_state interfaces just by having the right methods.
type fakeConfirmFenceAnchor struct {
	value    string
	found    bool
	readErr  error
	writeErr error
	written  string
	// journal is shared with fakeConfirmFenceAppState (and, in the ordering
	// test, with a marker the test inserts by hand) so a caller can prove WHEN
	// each write happened relative to the others, not merely that it happened.
	journal *[]string
}

func (f *fakeConfirmFenceAnchor) Read() (string, bool, error) {
	if f.readErr != nil {
		return "", false, f.readErr
	}
	return f.value, f.found, nil
}

func (f *fakeConfirmFenceAnchor) Write(value string) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	if f.journal != nil {
		*f.journal = append(*f.journal, "anchor")
	}
	f.written = value
	f.value, f.found = value, true
	return nil
}

type fakeConfirmFenceAppState struct {
	values  map[string]string
	getErr  error
	setErr  error
	journal *[]string
}

func (f *fakeConfirmFenceAppState) Get(_ context.Context, key string) (string, bool, error) {
	if f.getErr != nil {
		return "", false, f.getErr
	}
	value, ok := f.values[key]
	return value, ok, nil
}

func (f *fakeConfirmFenceAppState) Set(_ context.Context, key string, value string) error {
	if f.setErr != nil {
		return f.setErr
	}
	if f.journal != nil {
		*f.journal = append(*f.journal, "app_state")
	}
	if f.values == nil {
		f.values = map[string]string{}
	}
	f.values[key] = value
	return nil
}

type fakeConfirmFenceUsers struct{}

func (fakeConfirmFenceUsers) DisarmAllCalendarFeedTokens(_ context.Context) (int64, error) {
	return 0, nil
}

const confirmFenceTestToken = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// TestConfirmOperatorFeedRevocationSucceedsAndAdvancesWhenTheHalvesAgree is
// the positive anchor every refusal test below needs: without it, a
// confirmOperatorFeedRevocation that always refused would pass every case
// that only checks for an error.
func TestConfirmOperatorFeedRevocationSucceedsAndAdvancesWhenTheHalvesAgree(t *testing.T) {
	appState := &fakeConfirmFenceAppState{values: map[string]string{
		models.AppStateKeyCalendarFeedRestoreFence: confirmFenceTestToken,
	}}
	anchor := &fakeConfirmFenceAnchor{value: confirmFenceTestToken, found: true}
	fence := services.NewCalendarFeedRestoreFence(appState, fakeConfirmFenceUsers{}, anchor)

	const fencePath = "/app/fence/calendar-feed.fence"
	var errOutput bytes.Buffer
	if err := confirmOperatorFeedRevocation(context.Background(), fencePath, fence, &errOutput); err != nil {
		t.Fatalf("confirmOperatorFeedRevocation: %v", err)
	}

	if advanced := appState.values[models.AppStateKeyCalendarFeedRestoreFence]; advanced == confirmFenceTestToken || advanced == "" {
		t.Fatalf("a confirmed revocation must advance the database half, got %q", advanced)
	}
	if anchor.written == "" || anchor.written != appState.values[models.AppStateKeyCalendarFeedRestoreFence] {
		t.Fatalf("both halves must hold the same fresh token, file %q app_state %q", anchor.written, appState.values[models.AppStateKeyCalendarFeedRestoreFence])
	}

	line := errOutput.String()
	for _, want := range []string{fencePath, "continuity confirmed", "recorded outside the database"} {
		if !strings.Contains(line, want) {
			t.Fatalf("the success line must contain %q, got %q", want, line)
		}
	}
}

// TestConfirmOperatorFeedRevocationRefusesAndWritesNothing covers every
// refusal shape and, for each, that the message names the variable, the
// state, the consequence of proceeding anyway, a remedy, and ends "Nothing
// was changed." — and that nothing was actually written to either half.
func TestConfirmOperatorFeedRevocationRefusesAndWritesNothing(t *testing.T) {
	cases := []struct {
		name       string
		fencePath  string
		appState   *fakeConfirmFenceAppState
		anchor     *fakeConfirmFenceAnchor
		wantExtra  []string
		wantRemedy string
	}{
		{
			name:       "the path is empty",
			fencePath:  "",
			appState:   &fakeConfirmFenceAppState{values: map[string]string{}},
			anchor:     &fakeConfirmFenceAnchor{},
			wantExtra:  []string{"is not set in this shell"},
			wantRemedy: "docker exec",
		},
		{
			name:       "the path is not rooted",
			fencePath:  filepath.Join("state", "calendar-feed.fence"),
			appState:   &fakeConfirmFenceAppState{values: map[string]string{}},
			anchor:     &fakeConfirmFenceAnchor{},
			wantExtra:  []string{filepath.Join("state", "calendar-feed.fence"), "a relative path", "working directory"},
			wantRemedy: "docker exec",
		},
		{
			name:      "the database marker cannot be read",
			fencePath: "/app/fence/calendar-feed.fence",
			appState:  &fakeConfirmFenceAppState{values: map[string]string{}, getErr: errors.New("database is locked")},
			anchor:    &fakeConfirmFenceAnchor{value: confirmFenceTestToken, found: true},
			wantExtra: []string{
				"could not be read",
				"database is locked",
			},
			wantRemedy: "once the database answers",
		},
		{
			name:      "the anchor is unreachable",
			fencePath: "/app/fence/calendar-feed.fence",
			appState:  &fakeConfirmFenceAppState{values: map[string]string{}},
			anchor:    &fakeConfirmFenceAnchor{readErr: errNotAMount},
			wantExtra: []string{
				"points at /app/fence/calendar-feed.fence",
				"does not exist or cannot be written",
			},
			wantRemedy: "docker exec",
		},
		{
			name:      "the halves disagree",
			fencePath: "/app/fence/calendar-feed.fence",
			appState: &fakeConfirmFenceAppState{values: map[string]string{
				models.AppStateKeyCalendarFeedRestoreFence: "an-older-generation",
			}},
			anchor:     &fakeConfirmFenceAnchor{value: confirmFenceTestToken, found: true},
			wantExtra:  []string{"do not agree"},
			wantRemedy: "Start the server once",
		},
		{
			name:       "neither half has ever recorded a marker",
			fencePath:  "/app/fence/calendar-feed.fence",
			appState:   &fakeConfirmFenceAppState{values: map[string]string{}},
			anchor:     &fakeConfirmFenceAnchor{},
			wantExtra:  []string{"has ever recorded a marker"},
			wantRemedy: "Start the server once",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fence := services.NewCalendarFeedRestoreFence(testCase.appState, fakeConfirmFenceUsers{}, testCase.anchor)
			before := map[string]string{}
			for key, value := range testCase.appState.values {
				before[key] = value
			}

			err := confirmOperatorFeedRevocation(context.Background(), testCase.fencePath, fence, &bytes.Buffer{})
			if err == nil {
				t.Fatal("expected a refusal")
			}
			message := err.Error()
			for _, want := range append([]string{security.CalendarFeedFencePathEnv, testCase.wantRemedy, "Nothing was changed"}, testCase.wantExtra...) {
				if !strings.Contains(message, want) {
					t.Fatalf("refusal must contain %q, got %q", want, message)
				}
			}

			if len(testCase.appState.values) != len(before) {
				t.Fatalf("a refusal must write nothing to app_state, before=%v after=%v", before, testCase.appState.values)
			}
			for key, value := range before {
				if testCase.appState.values[key] != value {
					t.Fatalf("a refusal must write nothing to app_state, before=%v after=%v", before, testCase.appState.values)
				}
			}
			if testCase.anchor.written != "" {
				t.Fatalf("a refusal must write no file, got %q", testCase.anchor.written)
			}
		})
	}
}

// TestConfirmOperatorFeedRevocationNamesTheHalfAdvancedFenceInsteadOfClaimingNothingChanged
// pins the one refusal after which something did change: the file half moved
// and the database write then failed. Every other refusal ends "Nothing was
// changed"; this one must not, must name what the next server start will do,
// and must still say the account itself was left alone.
func TestConfirmOperatorFeedRevocationNamesTheHalfAdvancedFenceInsteadOfClaimingNothingChanged(t *testing.T) {
	appState := &fakeConfirmFenceAppState{
		values: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: confirmFenceTestToken},
		setErr: errors.New("database is locked"),
	}
	anchor := &fakeConfirmFenceAnchor{value: confirmFenceTestToken, found: true}
	fence := services.NewCalendarFeedRestoreFence(appState, fakeConfirmFenceUsers{}, anchor)

	err := confirmOperatorFeedRevocation(context.Background(), "/app/fence/calendar-feed.fence", fence, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected a refusal")
	}
	message := err.Error()
	if strings.Contains(message, "Nothing was changed") {
		t.Fatalf("the fence file moved, so the refusal must not claim nothing changed: %q", message)
	}
	for _, want := range []string{
		"/app/fence/calendar-feed.fence",
		"database is locked",
		"next start disarms every armed calendar feed",
		"The account was not changed",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("refusal must contain %q, got %q", want, message)
		}
	}
	if anchor.written == "" || anchor.written == confirmFenceTestToken {
		t.Fatalf("the file half must have moved, got %q", anchor.written)
	}
	if got := appState.values[models.AppStateKeyCalendarFeedRestoreFence]; got != confirmFenceTestToken {
		t.Fatalf("the database half must be left at its old value, got %q", got)
	}
}

var errNotAMount = errors.New("read-only file system")
