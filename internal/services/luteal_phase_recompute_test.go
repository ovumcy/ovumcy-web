package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The pass under test repairs a CACHE, so every fixture here states two things
// at once: what the logs support under the corrected inference, and what the old
// convention had stored for those same logs. The stored numbers are chosen so
// that "recompute from the logs" and "subtract one from the stored value" — the
// only correction a SQL migration could express — give different answers wherever
// the two can differ at all. A fixture where they agree would be green about
// nothing.
//
// The logs come from lutealRoundTripLogs, the same builder the round-trip
// regression for the inference fix uses, so these expectations rest on that
// test's arithmetic rather than on a second copy of it.
//
// What each fixture's logs stored under the OLD convention was measured, not
// derived on paper: the pre-fix formula (the calendar span from the ovulation
// date to the next start, filtered and averaged exactly as it was) was replayed
// over these three fixtures and reported 15, 14 and 14 against the corrected
// 14, 18 and 14. That measurement is why the stored values below are what they
// are; it is not re-run here, because a permanent copy of the old formula would
// be a second implementation to keep in step with nothing.

var errLutealRecomputeStub = errors.New("stub storage failure")

// lutealRecomputeOrigin is the first cycle start every fixture is built from.
// Any UTC date works; the inference reads calendar-day differences only.
var lutealRecomputeOrigin = time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)

type stubLutealRecomputeAppState struct {
	values map[string]string
	getErr error
	setErr error
}

func (s *stubLutealRecomputeAppState) Get(_ context.Context, key string) (string, bool, error) {
	if s.getErr != nil {
		return "", false, s.getErr
	}
	value, ok := s.values[key]
	return value, ok, nil
}

func (s *stubLutealRecomputeAppState) Set(_ context.Context, key string, value string) error {
	if s.setErr != nil {
		return s.setErr
	}
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	return nil
}

func (s *stubLutealRecomputeAppState) markerWritten() bool {
	_, ok := s.values[models.AppStateKeyLutealPhaseRecomputeV1]
	return ok
}

type lutealPhaseUpdate struct {
	userID uint
	value  int
}

type stubLutealRecomputeUserStore struct {
	rows      []models.LutealPhaseRecomputeRow
	updates   []lutealPhaseUpdate
	listed    bool
	listErr   error
	updateErr map[uint]error
}

func (s *stubLutealRecomputeUserStore) ListOwnerLutealPhaseRows(_ context.Context) ([]models.LutealPhaseRecomputeRow, error) {
	s.listed = true
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.rows, nil
}

func (s *stubLutealRecomputeUserStore) UpdateByID(_ context.Context, userID uint, updates map[string]any) error {
	if err := s.updateErr[userID]; err != nil {
		return err
	}
	value, ok := updates["luteal_phase"].(int)
	if !ok || len(updates) != 1 {
		return errors.New("the pass may write luteal_phase and nothing else")
	}
	s.updates = append(s.updates, lutealPhaseUpdate{userID: userID, value: value})
	return nil
}

type stubLutealRecomputeLogStore struct {
	logs   map[uint][]models.DailyLog
	read   []uint
	errs   map[uint]error
	shared []models.DailyLog
}

func (s *stubLutealRecomputeLogStore) ListByUser(_ context.Context, userID uint) ([]models.DailyLog, error) {
	s.read = append(s.read, userID)
	if err := s.errs[userID]; err != nil {
		return nil, err
	}
	if s.shared != nil {
		return s.shared, nil
	}
	return s.logs[userID], nil
}

// assertInferenceSupports pins what the fixture's logs mean before the pass is
// asked about them. Without it a fixture built outside the inference's range
// reads as a defect in the pass instead of as a fixture that supplies no signal.
func assertInferenceSupports(t *testing.T, logs []models.DailyLog, wantLuteal int, wantRefined bool) {
	t.Helper()

	luteal, refined := InferUserLutealPhase(logs, time.UTC)
	if refined != wantRefined {
		t.Fatalf("fixture: InferUserLutealPhase refined = %v, want %v", refined, wantRefined)
	}
	if luteal != wantLuteal {
		t.Fatalf("fixture: InferUserLutealPhase = %d, want %d", luteal, wantLuteal)
	}
}

func TestLutealPhaseRecomputeCorrectsARowWrittenUnderTheOldConvention(t *testing.T) {
	// Three observed starts 28 days apart, ovulation on cycle day 14 in each of
	// the first two: the corrected inference reads 28-14 = 14. The old
	// convention measured the calendar span from the ovulation date to the next
	// start, which counts the ovulation day itself, so it stored 15 — and 15 fed
	// back into CalcOvulationDay predicts cycle day 13 on the same cycle.
	logs := lutealRoundTripLogs(t, lutealRecomputeOrigin, 28, []int{14, 14}, lutealSignalEggWhite)
	assertInferenceSupports(t, logs, 14, true)

	appState := &stubLutealRecomputeAppState{}
	users := &stubLutealRecomputeUserStore{rows: []models.LutealPhaseRecomputeRow{{ID: 7, LutealPhase: 15}}}
	logStore := &stubLutealRecomputeLogStore{logs: map[uint][]models.DailyLog{7: logs}}

	outcome, err := NewLutealPhaseRecomputer(appState, users, logStore, time.UTC).Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(users.updates) != 1 || users.updates[0] != (lutealPhaseUpdate{userID: 7, value: 14}) {
		t.Fatalf("the stale row must be rewritten to 14, got %+v", users.updates)
	}
	if outcome.Corrected != 1 || outcome.Failed != 0 || outcome.AlreadyDone {
		t.Fatalf("outcome = %+v, want one correction and no failure", outcome)
	}
	if !appState.markerWritten() {
		t.Fatal("a completed pass must record the done-marker")
	}
}

func TestLutealPhaseRecomputeLandsOnTheDefaultWhenNoSignalSurvives(t *testing.T) {
	// Two observed starts, so the inference declines before it reads a single
	// ovulation signal — the population that can never self-heal, since neither
	// writer of the cache runs without the owner writing something.
	//
	// The stored 18 is a personalized estimate the account earned when it still
	// had three starts to infer from and kept after it stopped (days re-marked,
	// a partial restore). It is also what separates the two candidate repairs:
	// recomputing lands on defaultLutealPhaseDays (14), while subtracting one
	// from the stored value would leave 17 — a number no derivation produces.
	logs := lutealRoundTripLogs(t, lutealRecomputeOrigin, 28, []int{14}, lutealSignalEggWhite)
	assertInferenceSupports(t, logs, defaultLutealPhaseDays, false)

	appState := &stubLutealRecomputeAppState{}
	users := &stubLutealRecomputeUserStore{rows: []models.LutealPhaseRecomputeRow{{ID: 3, LutealPhase: 18}}}
	logStore := &stubLutealRecomputeLogStore{logs: map[uint][]models.DailyLog{3: logs}}

	if _, err := NewLutealPhaseRecomputer(appState, users, logStore, time.UTC).Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(users.updates) != 1 || users.updates[0].value != defaultLutealPhaseDays {
		t.Fatalf("an account with no usable signal must land on %d, got %+v", defaultLutealPhaseDays, users.updates)
	}
}

func TestLutealPhaseRecomputeIsNotAShiftOfTheStoredValue(t *testing.T) {
	// The case that rules out an arithmetic migration outright. Three cycles of
	// 30 days with ovulation on days 10, 10 and 16 carry corrected samples
	// 20, 20 and 14 — average 18.
	//
	// Under the old convention those same cycles measured 21, 21 and 15, and the
	// plausibility filter drops anything above maxPlausibleLutealPhaseDays, so
	// two of the three samples were thrown away. One sample is below the refine
	// gate, so the old code declined and the writer stored defaultLutealPhaseDays.
	//
	// So the stored 14 must become 18: it moves UP by four, from a value that
	// looks exactly like the seeded default. Subtracting one would give 13, and
	// reading 14 as "already the default, leave it" would give 14. Both are
	// wrong, and only re-running the inference over the logs finds 18.
	logs := lutealRoundTripLogs(t, lutealRecomputeOrigin, 30, []int{10, 10, 16}, lutealSignalEggWhite)
	assertInferenceSupports(t, logs, 18, true)

	appState := &stubLutealRecomputeAppState{}
	users := &stubLutealRecomputeUserStore{rows: []models.LutealPhaseRecomputeRow{{ID: 11, LutealPhase: defaultLutealPhaseDays}}}
	logStore := &stubLutealRecomputeLogStore{logs: map[uint][]models.DailyLog{11: logs}}

	if _, err := NewLutealPhaseRecomputer(appState, users, logStore, time.UTC).Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(users.updates) != 1 || users.updates[0].value != 18 {
		t.Fatalf("the row must be recomputed to 18, got %+v", users.updates)
	}
}

func TestLutealPhaseRecomputeLeavesAnAgreeingRowUnwritten(t *testing.T) {
	logs := lutealRoundTripLogs(t, lutealRecomputeOrigin, 28, []int{14, 14}, lutealSignalEggWhite)

	appState := &stubLutealRecomputeAppState{}
	users := &stubLutealRecomputeUserStore{rows: []models.LutealPhaseRecomputeRow{{ID: 5, LutealPhase: 14}}}
	logStore := &stubLutealRecomputeLogStore{logs: map[uint][]models.DailyLog{5: logs}}

	outcome, err := NewLutealPhaseRecomputer(appState, users, logStore, time.UTC).Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(users.updates) != 0 {
		t.Fatalf("a row the recompute agrees with must not be written, got %+v", users.updates)
	}
	// Not writing and not looking print the same nothing, so the row's logs
	// having been read is what makes the assertion above mean anything.
	if len(logStore.read) != 1 || logStore.read[0] != 5 {
		t.Fatalf("the pass must still read the row's logs, read = %v", logStore.read)
	}
	if outcome.Corrected != 0 || !appState.markerWritten() {
		t.Fatalf("outcome = %+v, marker written = %v; want a completed pass with no correction", outcome, appState.markerWritten())
	}
}

func TestLutealPhaseRecomputeSkipsTheScanOnceTheMarkerIsPresent(t *testing.T) {
	appState := &stubLutealRecomputeAppState{values: map[string]string{models.AppStateKeyLutealPhaseRecomputeV1: "done"}}
	users := &stubLutealRecomputeUserStore{rows: []models.LutealPhaseRecomputeRow{{ID: 1, LutealPhase: 15}}}
	logStore := &stubLutealRecomputeLogStore{}

	outcome, err := NewLutealPhaseRecomputer(appState, users, logStore, time.UTC).Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if !outcome.AlreadyDone {
		t.Fatalf("outcome = %+v, want AlreadyDone", outcome)
	}
	if users.listed || len(logStore.read) != 0 || len(users.updates) != 0 {
		t.Fatalf("a marked instance must scan nothing (listed=%v, read=%v, updates=%+v)", users.listed, logStore.read, users.updates)
	}
}

func TestLutealPhaseRecomputeKeepsTheMarkerUnwrittenWhenARowFails(t *testing.T) {
	logs := lutealRoundTripLogs(t, lutealRecomputeOrigin, 28, []int{14, 14}, lutealSignalEggWhite)

	appState := &stubLutealRecomputeAppState{}
	users := &stubLutealRecomputeUserStore{
		rows:      []models.LutealPhaseRecomputeRow{{ID: 1, LutealPhase: 15}, {ID: 2, LutealPhase: 15}, {ID: 3, LutealPhase: 15}},
		updateErr: map[uint]error{3: errLutealRecomputeStub},
	}
	logStore := &stubLutealRecomputeLogStore{
		logs: map[uint][]models.DailyLog{1: logs, 2: logs, 3: logs},
		errs: map[uint]error{2: errLutealRecomputeStub},
	}

	outcome, err := NewLutealPhaseRecomputer(appState, users, logStore, time.UTC).Run(context.Background())
	if err != nil {
		t.Fatalf("a per-row failure must not abort the pass: %v", err)
	}

	// One row repaired, one unreadable, one unwritable — and the pass walked all
	// three rather than stopping at the first failure.
	if outcome.Corrected != 1 || outcome.Failed != 2 {
		t.Fatalf("outcome = %+v, want 1 corrected and 2 failed", outcome)
	}
	if appState.markerWritten() {
		t.Fatal("a pass that skipped a row must leave the marker unwritten so the next boot retries")
	}
}

func TestLutealPhaseRecomputeAbortsWithoutMarkerOnStorageFailure(t *testing.T) {
	for name, appStateAndUsers := range map[string]struct {
		appState *stubLutealRecomputeAppState
		users    *stubLutealRecomputeUserStore
	}{
		"marker unreadable": {
			appState: &stubLutealRecomputeAppState{getErr: errLutealRecomputeStub},
			users:    &stubLutealRecomputeUserStore{},
		},
		"owner listing unreadable": {
			appState: &stubLutealRecomputeAppState{},
			users:    &stubLutealRecomputeUserStore{listErr: errLutealRecomputeStub},
		},
	} {
		t.Run(name, func(t *testing.T) {
			appState, users := appStateAndUsers.appState, appStateAndUsers.users
			logStore := &stubLutealRecomputeLogStore{}

			if _, err := NewLutealPhaseRecomputer(appState, users, logStore, time.UTC).Run(context.Background()); !errors.Is(err, errLutealRecomputeStub) {
				t.Fatalf("Run error = %v, want the storage failure", err)
			}
			if appState.markerWritten() {
				t.Fatal("an aborted pass must leave the marker unwritten")
			}
		})
	}
}

func TestLutealPhaseRecomputeReadsEveryOwnerAtTheirOwnStoredTimezone(t *testing.T) {
	// Date-only values persist as UTC-midnight and every comparison the inference
	// makes re-anchors both operands, so the owner's zone cannot move the answer —
	// which is what lets a boot pass with no request agree with the day-save
	// writer that has one. Pinned rather than assumed: an empty column, a real
	// IANA name, and the "Local" token resolveOwnerLocation refuses by input must
	// all reach the same corrected value.
	logs := lutealRoundTripLogs(t, lutealRecomputeOrigin, 28, []int{14, 14}, lutealSignalEggWhite)

	appState := &stubLutealRecomputeAppState{}
	users := &stubLutealRecomputeUserStore{rows: []models.LutealPhaseRecomputeRow{
		{ID: 1, Timezone: "", LutealPhase: 15},
		{ID: 2, Timezone: "America/New_York", LutealPhase: 15},
		{ID: 3, Timezone: "Pacific/Kiritimati", LutealPhase: 15},
		{ID: 4, Timezone: "Local", LutealPhase: 15},
	}}
	logStore := &stubLutealRecomputeLogStore{shared: logs}

	if _, err := NewLutealPhaseRecomputer(appState, users, logStore, time.UTC).Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(users.updates) != 4 {
		t.Fatalf("every owner must be corrected, got %+v", users.updates)
	}
	for _, update := range users.updates {
		if update.value != 14 {
			t.Fatalf("owner %d landed on %d, want 14 — the zone must not move the derivation", update.userID, update.value)
		}
	}
}

func TestDeriveUserLutealPhaseIsTheOneRuleTheCacheIsWrittenBy(t *testing.T) {
	// The helper the two request-driven writers and the boot pass share. Its
	// contract is exactly two branches, and both are pinned here so a future
	// edit cannot quietly change what an un-inferable account stores.
	refinable := lutealRoundTripLogs(t, lutealRecomputeOrigin, 28, []int{14, 14}, lutealSignalEggWhite)
	if got := deriveUserLutealPhase(refinable, time.UTC); got != 14 {
		t.Fatalf("deriveUserLutealPhase on refinable logs = %d, want 14", got)
	}
	if got := deriveUserLutealPhase(nil, time.UTC); got != defaultLutealPhaseDays {
		t.Fatalf("deriveUserLutealPhase with no logs = %d, want %d", got, defaultLutealPhaseDays)
	}
}

func TestLutealPhaseRecomputeTreatsANilFallbackLocationAsUTC(t *testing.T) {
	// The constructor's nil guard. A caller with no server zone to hand over —
	// any test, and any future wiring that resolves the zone later — must not
	// reach resolveOwnerLocation with a nil fallback, which would hand a nil
	// *time.Location to the derivation. Pinned by behaviour rather than by
	// reading the field back: the pass must correct the row exactly as it does
	// under an explicit UTC.
	logs := lutealRoundTripLogs(t, lutealRecomputeOrigin, 28, []int{14, 14}, lutealSignalEggWhite)

	appState := &stubLutealRecomputeAppState{}
	users := &stubLutealRecomputeUserStore{rows: []models.LutealPhaseRecomputeRow{{ID: 9, LutealPhase: 15}}}
	logStore := &stubLutealRecomputeLogStore{logs: map[uint][]models.DailyLog{9: logs}}

	if _, err := NewLutealPhaseRecomputer(appState, users, logStore, nil).Run(context.Background()); err != nil {
		t.Fatalf("Run with a nil fallback location: %v", err)
	}

	if len(users.updates) != 1 || users.updates[0].value != 14 {
		t.Fatalf("a nil fallback location must behave as UTC, got %+v", users.updates)
	}
}

func TestLutealPhaseRecomputeReportsAMarkerItCouldNotWrite(t *testing.T) {
	// The corrections have already landed when Set fails, so the pass returns
	// both the error AND the count: the operator line must still say what was
	// repaired, and the caller must still learn the marker is missing. The next
	// boot then re-walks, agrees with every row and writes the marker on its own,
	// which is why an unwritten marker costs a scan and never a wrong value.
	logs := lutealRoundTripLogs(t, lutealRecomputeOrigin, 28, []int{14, 14}, lutealSignalEggWhite)

	appState := &stubLutealRecomputeAppState{setErr: errLutealRecomputeStub}
	users := &stubLutealRecomputeUserStore{rows: []models.LutealPhaseRecomputeRow{{ID: 4, LutealPhase: 15}}}
	logStore := &stubLutealRecomputeLogStore{logs: map[uint][]models.DailyLog{4: logs}}

	outcome, err := NewLutealPhaseRecomputer(appState, users, logStore, time.UTC).Run(context.Background())
	if !errors.Is(err, errLutealRecomputeStub) {
		t.Fatalf("Run error = %v, want the marker write failure", err)
	}
	if outcome.Corrected != 1 {
		t.Fatalf("outcome = %+v, want the correction that already landed to be reported", outcome)
	}
	if appState.markerWritten() {
		t.Fatal("a failed Set must leave no marker behind")
	}
}
