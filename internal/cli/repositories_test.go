package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// armOperatorFence creates a fence file this test owns and runs the one
// Enforce pass a first boot performs against databasePath, so both fence
// halves already agree by the time a subcommand under test calls
// confirmOperatorFeedRevocation. It returns the fence file's path, which the
// caller passes to the command under test — and which a caller simulating a
// restore can disturb first.
//
// Nothing here touches the environment: the fence path is a parameter all the
// way down, so a caller of this helper is free to run with t.Parallel.
func armOperatorFence(t *testing.T, databasePath string) string {
	t.Helper()

	fencePath := filepath.Join(t.TempDir(), "calendar-feed.fence")

	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("armOperatorFence: open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("armOperatorFence: open sql db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	_, fence := buildRepositories(database, fencePath)
	if _, err := fence.EnforceRequired(context.Background()); err != nil {
		t.Fatalf("armOperatorFence: Enforce: %v", err)
	}
	return fencePath
}

// The classifier itself is tested where it lives, beside the variable it
// judges: security.TestCalendarFeedFencePathRootedAcceptsTheRootsFilepathIsAbsMisses.
// What stays here is CLI conduct — which refusal confirmOperatorFeedRevocation
// produces for a path the classifier rejects, and that it writes nothing.

// TestCalendarFeedFencePathTrimsTheEnvironmentValue pins calendarFeedFencePath
// as the ONE place that trims CALENDAR_FEED_FENCE_PATH: confirmOperatorFeedRevocation
// no longer trims its own copy, so an operator's shell exporting the variable
// with stray whitespace (a trailing newline from a sourced file is the
// realistic shape) has to be cleaned up here or not at all. Without this test,
// removing the TrimSpace below stays green: nothing else in this package sets
// the environment variable itself.
func TestCalendarFeedFencePathTrimsTheEnvironmentValue(t *testing.T) {
	t.Setenv(security.CalendarFeedFencePathEnv, " /app/fence/f ")

	if got := calendarFeedFencePath(); got != "/app/fence/f" {
		t.Fatalf("calendarFeedFencePath: expected surrounding whitespace trimmed, got %q", got)
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
	// onWrite, when set, runs at the instant the file half is written, before
	// it is recorded — so an ordering test can look at the database at that
	// moment and prove the protected row has not committed yet.
	onWrite func()
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
	if f.onWrite != nil {
		f.onWrite()
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

// Delete completes the app_state surface the fence needs. No operator path
// deletes anything — only the server's boot pass erases the unanchored stamp —
// so this exists to satisfy the interface and is deliberately journalled, which
// would make an operator command that started deleting markers visible in the
// ordering assertions rather than silent.
func (f *fakeConfirmFenceAppState) Delete(_ context.Context, key string) error {
	if f.journal != nil {
		*f.journal = append(*f.journal, "app_state_delete")
	}
	delete(f.values, key)
	return nil
}

type fakeConfirmFenceUsers struct{}

func (fakeConfirmFenceUsers) DisarmAllCalendarFeedTokens(_ context.Context) (int64, error) {
	return 0, nil
}

func (fakeConfirmFenceUsers) RevokeRestoredAccess(_ context.Context) (models.RestoreRevocationCounts, error) {
	return models.RestoreRevocationCounts{}, nil
}

const confirmFenceTestToken = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// TestConfirmOperatorFeedRevocationSucceedsAndAdvancesWhenTheHalvesAgree is
// the positive anchor every refusal test below needs: without it, a
// confirmOperatorFeedRevocation that always refused would pass every case
// that only checks for an error.
//
// The gate itself writes nothing (WEB-505): it confirms continuity and arms
// the fail-closed form, and the advance runs inside the operator's own write,
// which records the marker in its transaction. So the gate leaves both halves
// where they were, and the advance that follows hands its commit the fresh
// token the file already holds — never through app_state's own Set, which
// would commit the marker apart from the row.
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
	if anchor.written != "" || appState.values[models.AppStateKeyCalendarFeedRestoreFence] != confirmFenceTestToken {
		t.Fatalf("the gate must write neither half, file %q app_state %q", anchor.written, appState.values[models.AppStateKeyCalendarFeedRestoreFence])
	}

	committed := ""
	if err := fence.Advance(context.Background(), func(token string) error {
		committed = token
		return nil
	}); err != nil {
		t.Fatalf("Advance after the gate: %v", err)
	}
	if committed == "" || committed == confirmFenceTestToken || committed != anchor.written {
		t.Fatalf("the write's commit must receive the fresh token the file already holds, commit %q file %q", committed, anchor.written)
	}
	if got := appState.values[models.AppStateKeyCalendarFeedRestoreFence]; got != confirmFenceTestToken {
		t.Fatalf("the marker belongs to the write's transaction, not app_state.Set, got %q", got)
	}

	line := errOutput.String()
	for _, want := range []string{fencePath, "continuity confirmed", "recorded outside the database"} {
		if !strings.Contains(line, want) {
			t.Fatalf("the success line must contain %q, got %q", want, line)
		}
	}
}

// TestConfirmOperatorFeedRevocationRefusesAFenceThatMovedAfterTheGate pins the
// window between the gate and the write: the gate confirmed an agreeing pair,
// then another writer moved the file before the operator's write advanced. The
// write's advance re-reads the pair, refuses before writing the file or running
// the account write, and the command renders the gate's own refusal, ending
// "Nothing was changed".
func TestConfirmOperatorFeedRevocationRefusesAFenceThatMovedAfterTheGate(t *testing.T) {
	appState := &fakeConfirmFenceAppState{values: map[string]string{
		models.AppStateKeyCalendarFeedRestoreFence: confirmFenceTestToken,
	}}
	anchor := &fakeConfirmFenceAnchor{value: confirmFenceTestToken, found: true}
	fence := services.NewCalendarFeedRestoreFence(appState, fakeConfirmFenceUsers{}, anchor)

	const fencePath = "/app/fence/calendar-feed.fence"
	if err := confirmOperatorFeedRevocation(context.Background(), fencePath, fence, &bytes.Buffer{}); err != nil {
		t.Fatalf("the gate must confirm an agreeing pair: %v", err)
	}
	anchor.value = "moved-by-another-writer"

	committed := false
	writeErr := fence.Advance(context.Background(), func(string) error {
		committed = true
		return nil
	})
	if writeErr == nil || committed {
		t.Fatalf("a pair that moved after the gate must refuse before the write, err %v committed %v", writeErr, committed)
	}
	err := operatorFeedRevocationCommitted(fencePath, writeErr, func(err error) error {
		t.Fatalf("the refusal must not be rendered as the account write's own failure: %v", err)
		return err
	})
	if err == nil {
		t.Fatal("expected a refusal")
	}
	message := err.Error()
	for _, want := range []string{fencePath, "Nothing was changed"} {
		if !strings.Contains(message, want) {
			t.Fatalf("refusal must contain %q, got %q", want, message)
		}
	}
	if anchor.written != "" || appState.values[models.AppStateKeyCalendarFeedRestoreFence] != confirmFenceTestToken {
		t.Fatalf("a refusal must write neither half, file %q app_state %q", anchor.written, appState.values[models.AppStateKeyCalendarFeedRestoreFence])
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
			wantRemedy: "Reconfigure the SERVER",
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
			// errFencePermissionDenied models a genuine Read() failure distinct
			// from "not found" — os.Stat/os.Open on a path this process cannot
			// read, e.g. a directory owned by another uid. Unlike fs.ErrNotExist
			// (which CalendarFeedFenceFile.Read reports as "absent", never an
			// error — see TestConfirmOperatorFeedRevocationNamesTheAnchorMissing...
			// below), this is the "unreachable" arm.
			name:      "the anchor is unreachable",
			fencePath: "/app/fence/calendar-feed.fence",
			appState:  &fakeConfirmFenceAppState{values: map[string]string{}},
			anchor:    &fakeConfirmFenceAnchor{readErr: errFencePermissionDenied},
			wantExtra: []string{
				"points at /app/fence/calendar-feed.fence",
				"cannot read or write",
				"must name a small regular file",
				"permission denied",
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
			wantExtra:  []string{"hold different tokens"},
			wantRemedy: "Start the server once",
		},
		{
			// The mirror of the anchor-missing shape below: here the file IS
			// visible and holds a token, and it is the database that has never
			// recorded one. A restart reconciles it, so the remedy matches the
			// disagreement's — but the sentence must not, or it sends the
			// operator comparing two values one of which does not exist.
			name:       "the file holds a token and the database has no marker",
			fencePath:  "/app/fence/calendar-feed.fence",
			appState:   &fakeConfirmFenceAppState{values: map[string]string{}},
			anchor:     &fakeConfirmFenceAnchor{value: confirmFenceTestToken, found: true},
			wantExtra:  []string{"carries a token but the database has no marker"},
			wantRemedy: "Start the server once",
		},
		{
			name:       "neither half holds a marker",
			fencePath:  "/app/fence/calendar-feed.fence",
			appState:   &fakeConfirmFenceAppState{values: map[string]string{}},
			anchor:     &fakeConfirmFenceAnchor{},
			wantExtra:  []string{"nor the database holds a marker"},
			wantRemedy: "writable fence",
		},
		{
			// The database recorded a marker but the anchor was never found —
			// not the same shape as a disagreement, where the anchor IS found
			// and simply holds a different value. A restart alone reconciles a
			// disagreement; it does nothing for a file this process cannot see
			// at all, so the two must not share a remedy.
			name:      "the database is armed but no fence file has ever been found",
			fencePath: "/app/fence/calendar-feed.fence",
			appState: &fakeConfirmFenceAppState{values: map[string]string{
				models.AppStateKeyCalendarFeedRestoreFence: confirmFenceTestToken,
			}},
			anchor:     &fakeConfirmFenceAnchor{},
			wantExtra:  []string{"no fence value is visible from this process", "missing, or present and empty"},
			wantRemedy: "disarms every armed calendar feed on the instance and rewrites both halves",
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

// halfAdvanceConfirmedFence drives the one operator outcome after which
// something did change: the gate confirms, the advance writes the file, the
// write's transaction fails, and recording the token alone fails too (setErr
// on appState). It returns what the subcommand would hand the operator.
func halfAdvanceConfirmedFence(t *testing.T, fence *services.CalendarFeedRestoreFence, fencePath string) error {
	t.Helper()

	if err := confirmOperatorFeedRevocation(context.Background(), fencePath, fence, &bytes.Buffer{}); err != nil {
		t.Fatalf("the gate must confirm an agreeing pair: %v", err)
	}
	writeErr := fence.Advance(context.Background(), func(string) error { return errors.New("account write rolled back") })
	if !errors.Is(writeErr, services.ErrCalendarFeedFenceHalfAdvanced) {
		t.Fatalf("a failed write whose token could not be recorded alone must be half-advanced, got %v", writeErr)
	}
	return operatorFeedRevocationCommitted(fencePath, writeErr, func(err error) error { return err })
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

	err := halfAdvanceConfirmedFence(t, fence, "/app/fence/calendar-feed.fence")
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

// TestConfirmOperatorFeedRevocationHalfAdvancedRemedyIsAServerBootNotABareRerun
// is the proof that the OLD remedy text — "re-run this command" — was wrong:
// re-running against the exact fence state the first refusal left behind
// reads the file this run just wrote against the still-stale database marker,
// and reports a disagreement, never success. Only a server boot (which reads
// and reconciles both halves) fixes it.
func TestConfirmOperatorFeedRevocationHalfAdvancedRemedyIsAServerBootNotABareRerun(t *testing.T) {
	t.Parallel()

	appState := &fakeConfirmFenceAppState{
		values: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: confirmFenceTestToken},
		setErr: errors.New("database is locked"),
	}
	anchor := &fakeConfirmFenceAnchor{value: confirmFenceTestToken, found: true}
	fence := services.NewCalendarFeedRestoreFence(appState, fakeConfirmFenceUsers{}, anchor)

	firstErr := halfAdvanceConfirmedFence(t, fence, "/app/fence/calendar-feed.fence")
	if firstErr == nil {
		t.Fatal("expected the first call to refuse with a half-advanced fence")
	}
	if !strings.Contains(firstErr.Error(), "Start the server once") {
		t.Fatalf("expected the half-advanced refusal to point at a server boot, got %v", firstErr)
	}
	if strings.Contains(firstErr.Error(), "Re-run this command once the database answers") {
		t.Fatal("the old remedy told the operator to just re-run, which the second call below proves does not work")
	}

	// The database answers again, exactly as the half-advanced message's OLD
	// remedy told the operator to wait for — but the file half is still ahead
	// of it, which only a server boot reconciles.
	// A re-run is a new process, so it builds its own fence over the same two
	// halves.
	appState.setErr = nil
	rerun := services.NewCalendarFeedRestoreFence(appState, fakeConfirmFenceUsers{}, anchor)
	err := confirmOperatorFeedRevocation(context.Background(), "/app/fence/calendar-feed.fence", rerun, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected the second call to still refuse: a bare re-run is not the remedy")
	}
	if !strings.Contains(err.Error(), "hold different tokens") {
		t.Fatalf("expected the disagreement refusal on a bare re-run, got %v", err)
	}
}

// TestConfirmOperatorFeedRevocationNamesTheAnchorMissingWhenTheDatabaseIsArmedButNoFileIsVisible
// drives the REAL security.CalendarFeedFenceFile.Read, paired with a database
// that already carries a marker, through BOTH routes into that state — and
// the second one is why the sentence cannot say "the file is not visible".
//
// A directory that does not exist is the shape an operator's shell is in when
// it cannot see the mount the server uses. A file that exists and is EMPTY is
// a torn write, and Read reports it as absent on purpose (it must never
// compare against nothing), so it arrives here as the same state — with the
// file plainly visible. One sentence has to be true of both.
//
// Before calendarFeedFenceStateAnchorMissing existed, both fell through to
// the "halves disagree" state, whose "Start the server once" remedy would not
// have helped the first: restarting reconciles two VISIBLE, differing values,
// not a file this process cannot see at all.
func TestConfirmOperatorFeedRevocationNamesTheAnchorMissingWhenTheDatabaseIsArmedButNoFileIsVisible(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		fencePath func(t *testing.T) string
	}{
		{
			name: "no directory behind the path",
			fencePath: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "never-mounted", "calendar-feed.fence")
			},
		},
		{
			name: "a torn write left the file empty",
			fencePath: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "calendar-feed.fence")
				if err := os.WriteFile(path, nil, 0o600); err != nil {
					t.Fatalf("write empty fence file: %v", err)
				}
				return path
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			appState := &fakeConfirmFenceAppState{values: map[string]string{
				models.AppStateKeyCalendarFeedRestoreFence: confirmFenceTestToken,
			}}
			fencePath := testCase.fencePath(t)
			anchor := security.NewCalendarFeedFenceFile(fencePath)
			fence := services.NewCalendarFeedRestoreFence(appState, fakeConfirmFenceUsers{}, anchor)

			err := confirmOperatorFeedRevocation(context.Background(), fencePath, fence, &bytes.Buffer{})
			if err == nil {
				t.Fatal("expected a refusal")
			}
			message := err.Error()
			for _, want := range []string{fencePath, "no fence value is visible from this process", "Nothing was changed"} {
				if !strings.Contains(message, want) {
					t.Fatalf("refusal must contain %q, got %q", want, message)
				}
			}
			if strings.Contains(message, "hold different tokens") {
				t.Fatal("a database-only marker with no visible fence value is not the same shape as two visible, differing values")
			}
			if strings.Contains(message, "Start the server once with this fence configured") {
				t.Fatal("must not tell the operator to just restart: the database already carries a marker, and restarting alone answers only one of the two routes here")
			}
		})
	}
}

// TestConfirmOperatorFeedRevocationRejectsANilFenceInsteadOfPanicking covers
// a wiring mistake, not an operator state: fence.ConfirmAdvances' receiver
// touches fence.writing (a sync.Mutex field) before anything else, so calling
// it on a nil *services.CalendarFeedRestoreFence panics with a nil-pointer
// dereference. Every production caller gets its fence from buildRepositories,
// which never returns nil, but a future caller that skipped it would
// otherwise crash the whole process instead of getting an error back.
func TestConfirmOperatorFeedRevocationRejectsANilFenceInsteadOfPanicking(t *testing.T) {
	t.Parallel()

	err := confirmOperatorFeedRevocation(context.Background(), "/app/fence/calendar-feed.fence", nil, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected an error for a nil fence")
	}
}

// TestCalendarFeedFenceConfirmRefusalHasATextForEveryDeclaredState walks the
// enum itself — every value between the invalid zero and
// calendarFeedFenceStateCount — against calendarFeedFenceConfirmRefusals. A
// hand-written list of the states beside the table would agree with the table
// by construction and never notice a state declared without an entry; the
// declaration is the only source that cannot.
//
// Requiring every state's message to be DISTINCT is what would have caught
// calendarFeedFenceStateAnchorMissing silently reusing
// calendarFeedFenceStateNeverArmed's or calendarFeedFenceStateDisagree's text.
func TestCalendarFeedFenceConfirmRefusalHasATextForEveryDeclaredState(t *testing.T) {
	t.Parallel()

	if calendarFeedFenceStateCount <= calendarFeedFenceStateInvalid+1 {
		t.Fatal("the enum declares no states between the invalid zero and the count: this guard would report success over an empty set")
	}

	seen := map[string]calendarFeedFenceConfirmState{}
	for state := calendarFeedFenceStateInvalid + 1; state < calendarFeedFenceStateCount; state++ {
		refusal, ok := calendarFeedFenceConfirmRefusals[state]
		if !ok {
			t.Fatalf("state %d is declared but has no refusal text: an operator reaching it would get a panic, not a remedy", state)
		}
		if strings.TrimSpace(refusal.remedy) == "" {
			t.Fatalf("state %d has no remedy: a refusal that does not say what to do next is half a message", state)
		}

		message := calendarFeedFenceConfirmRefusal("/app/fence/calendar-feed.fence", state, errFencePermissionDenied).Error()
		if !strings.Contains(message, "Nothing was changed") {
			t.Fatalf("state %d: expected every refusal text to end \"Nothing was changed\", got %q", state, message)
		}
		if other, ok := seen[message]; ok {
			t.Fatalf("states %d and %d produced the identical message %q", other, state, message)
		}
		seen[message] = state
	}
}

// TestCalendarFeedFenceConfirmRefusalPanicsOnAStateWithNoText is the
// red-on-defect half of the guard above, and it covers the zero value on
// purpose: calendarFeedFenceStateInvalid is what an unassigned state variable
// holds, and before the enum reserved it the zero value WAS
// calendarFeedFenceStateNotSet — so a state that was never assigned rendered
// "CALENDAR_FEED_FENCE_PATH is not set in this shell" and sent the operator
// after a variable that may well have been set.
func TestCalendarFeedFenceConfirmRefusalPanicsOnAStateWithNoText(t *testing.T) {
	t.Parallel()

	for _, state := range []calendarFeedFenceConfirmState{calendarFeedFenceStateInvalid, calendarFeedFenceStateCount, 999} {
		t.Run(fmt.Sprintf("state %d", state), func(t *testing.T) {
			t.Parallel()

			defer func() {
				if recover() == nil {
					t.Fatalf("expected a panic for state %d, which has no refusal text", state)
				}
			}()
			_ = calendarFeedFenceConfirmRefusal("/app/fence/calendar-feed.fence", state, nil)
		})
	}
}

var errFencePermissionDenied = errors.New("permission denied")
