package services

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// stubFenceAppState is an in-memory app_state with injectable errors and a
// shared call journal (with stubFenceUserStore and stubFenceAnchor) so the
// ordering the fence promises — disarm, then the file, then the row — is
// asserted rather than assumed.
type stubFenceAppState struct {
	values  map[string]string
	getErr  error
	setErr  error
	journal *[]string
}

func (s *stubFenceAppState) Get(_ context.Context, key string) (string, bool, error) {
	if s.getErr != nil {
		return "", false, s.getErr
	}
	value, ok := s.values[key]
	return value, ok, nil
}

func (s *stubFenceAppState) Set(_ context.Context, key string, value string) error {
	if s.setErr != nil {
		return s.setErr
	}
	if s.journal != nil {
		*s.journal = append(*s.journal, "set")
	}
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.values[key] = value
	return nil
}

type stubFenceUserStore struct {
	disarmed  int64
	disarmErr error
	callCount int
	journal   *[]string
}

func (s *stubFenceUserStore) DisarmAllCalendarFeedTokens(_ context.Context) (int64, error) {
	s.callCount++
	if s.journal != nil {
		*s.journal = append(*s.journal, "disarm")
	}
	if s.disarmErr != nil {
		return 0, s.disarmErr
	}
	return s.disarmed, nil
}

type stubFenceAnchor struct {
	value    string
	found    bool
	readErr  error
	writeErr error
	written  string
	journal  *[]string
}

func (s *stubFenceAnchor) Read() (string, bool, error) {
	if s.readErr != nil {
		return "", false, s.readErr
	}
	return s.value, s.found, nil
}

func (s *stubFenceAnchor) Write(value string) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	if s.journal != nil {
		*s.journal = append(*s.journal, "anchor")
	}
	s.written = value
	s.value, s.found = value, true
	return nil
}

const fenceTestToken = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// TestCalendarFeedRestoreFenceFirstBootArmsWithoutDisarming pins the one case
// that must NOT revoke anything: neither half holds a token, so this is a new
// installation or the first start after the fence shipped. Disarming here would
// make the upgrade itself break every armed subscription, which is a different
// defect from the one the fence exists for.
func TestCalendarFeedRestoreFenceFirstBootArmsWithoutDisarming(t *testing.T) {
	appState := &stubFenceAppState{values: map[string]string{}}
	users := &stubFenceUserStore{disarmed: 7}
	anchor := &stubFenceAnchor{}

	outcome, err := NewCalendarFeedRestoreFence(appState, users, anchor).Enforce(context.Background())
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if !outcome.FirstBoot || outcome.ContinuityBroken || outcome.Unanchored {
		t.Fatalf("expected a first-boot outcome, got %+v", outcome)
	}
	if users.callCount != 0 {
		t.Fatalf("first boot must not disarm; disarm ran %d time(s)", users.callCount)
	}
	if anchor.written == "" {
		t.Fatal("first boot must write the fence file")
	}
	if got := appState.values[models.AppStateKeyCalendarFeedRestoreFence]; got != anchor.written {
		t.Fatalf("both halves must hold the same token; file %q, app_state %q", anchor.written, got)
	}
}

// TestCalendarFeedRestoreFenceAgreementIsANoOp pins the routine restart: the
// halves agree, so nothing is disarmed and no token is re-minted. Re-minting on
// every boot would be harmless but would hide a real mismatch behind churn.
func TestCalendarFeedRestoreFenceAgreementIsANoOp(t *testing.T) {
	appState := &stubFenceAppState{values: map[string]string{
		models.AppStateKeyCalendarFeedRestoreFence: fenceTestToken,
	}}
	users := &stubFenceUserStore{disarmed: 7}
	anchor := &stubFenceAnchor{value: fenceTestToken, found: true}

	outcome, err := NewCalendarFeedRestoreFence(appState, users, anchor).Enforce(context.Background())
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if outcome != (CalendarFeedRestoreFenceOutcome{}) {
		t.Fatalf("a matching fence must be a no-op, got %+v", outcome)
	}
	if users.callCount != 0 {
		t.Fatalf("a matching fence must not disarm; disarm ran %d time(s)", users.callCount)
	}
	if anchor.written != "" {
		t.Fatalf("a matching fence must not be rewritten, got %q", anchor.written)
	}
}

// TestCalendarFeedRestoreFenceDisarmsWhenTheHalvesDisagree is the finding
// itself, at the unit level: the file kept the token this run minted while the
// database came back holding an older one, which is what restoring a backup
// taken before a revocation looks like under an UNCHANGED SECRET_KEY. Every
// armed feed goes, and the disarm is ordered before either half of the new
// token is recorded.
func TestCalendarFeedRestoreFenceDisarmsWhenTheHalvesDisagree(t *testing.T) {
	journal := []string{}
	appState := &stubFenceAppState{
		values:  map[string]string{models.AppStateKeyCalendarFeedRestoreFence: "an-older-generation"},
		journal: &journal,
	}
	users := &stubFenceUserStore{disarmed: 3, journal: &journal}
	anchor := &stubFenceAnchor{value: fenceTestToken, found: true, journal: &journal}

	outcome, err := NewCalendarFeedRestoreFence(appState, users, anchor).Enforce(context.Background())
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if !outcome.ContinuityBroken || outcome.DisarmedFeeds != 3 {
		t.Fatalf("expected 3 feeds disarmed on a broken fence, got %+v", outcome)
	}
	if got := appState.values[models.AppStateKeyCalendarFeedRestoreFence]; got == "an-older-generation" || got != anchor.written {
		t.Fatalf("both halves must be re-minted together; file %q, app_state %q", anchor.written, got)
	}
	if want := []string{"disarm", "anchor", "set"}; !equalStrings(journal, want) {
		t.Fatalf("ordering must be %v, got %v", want, journal)
	}
}

// TestCalendarFeedRestoreFenceDisarmsWhenOnlyOneHalfHasAToken covers the two
// asymmetric shapes, both of which mean the database in front of the app is not
// the one the fence was written against. Restoring a pre-fence backup leaves the
// file holding a token and the database holding none; a fence volume the
// operator recreated leaves the mirror image.
func TestCalendarFeedRestoreFenceDisarmsWhenOnlyOneHalfHasAToken(t *testing.T) {
	cases := []struct {
		name   string
		stored map[string]string
		anchor *stubFenceAnchor
	}{
		{
			name:   "a backup predating the fence, restored under a running instance",
			stored: map[string]string{},
			anchor: &stubFenceAnchor{value: fenceTestToken, found: true},
		},
		{
			name:   "the fence volume was recreated while the database stayed",
			stored: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: fenceTestToken},
			anchor: &stubFenceAnchor{},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			appState := &stubFenceAppState{values: testCase.stored}
			users := &stubFenceUserStore{disarmed: 2}

			outcome, err := NewCalendarFeedRestoreFence(appState, users, testCase.anchor).Enforce(context.Background())
			if err != nil {
				t.Fatalf("Enforce: %v", err)
			}
			if !outcome.ContinuityBroken || outcome.DisarmedFeeds != 2 {
				t.Fatalf("expected the feeds disarmed, got %+v", outcome)
			}
			if users.callCount != 1 {
				t.Fatalf("expected exactly one disarm, got %d", users.callCount)
			}
		})
	}
}

// TestCalendarFeedRestoreFenceWithoutAnAnchorDisarmsAndRecordsNothing pins the
// fail-closed default. An unreadable fence — no CALENDAR_FEED_FENCE_PATH, no
// mount behind it — cannot prove this database still holds the revocations this
// instance performed, so every armed feed goes and NO marker is written: writing
// one would make the next boot, still unanchored, look like agreement.
func TestCalendarFeedRestoreFenceWithoutAnAnchorDisarmsAndRecordsNothing(t *testing.T) {
	notConfigured := errors.New("CALENDAR_FEED_FENCE_PATH is not set")
	appState := &stubFenceAppState{values: map[string]string{}}
	users := &stubFenceUserStore{disarmed: 4}

	outcome, err := NewCalendarFeedRestoreFence(appState, users, &stubFenceAnchor{readErr: notConfigured}).Enforce(context.Background())
	if err != nil {
		t.Fatalf("an unusable fence must not fail the boot: %v", err)
	}
	if !outcome.Unanchored || outcome.DisarmedFeeds != 4 {
		t.Fatalf("expected an unanchored disarm of 4, got %+v", outcome)
	}
	if !errors.Is(outcome.UnanchoredCause, notConfigured) {
		t.Fatalf("the outcome must carry the cause for the startup line, got %v", outcome.UnanchoredCause)
	}
	if len(appState.values) != 0 {
		t.Fatalf("an unanchored pass must record no marker, got %v", appState.values)
	}
}

// TestCalendarFeedRestoreFenceUnwritableAnchorDisarmsOnAFirstBoot is the
// upgrade case an operator hits by pulling the new image without adding the
// mount: the missing directory reads as "no token yet", so only the WRITE can
// discover it. The pass must fall back to the same fail-closed disarm rather
// than reporting a first boot it never recorded.
func TestCalendarFeedRestoreFenceUnwritableAnchorDisarmsOnAFirstBoot(t *testing.T) {
	unwritable := errors.New("read-only file system")
	appState := &stubFenceAppState{values: map[string]string{}}
	users := &stubFenceUserStore{disarmed: 5}

	outcome, err := NewCalendarFeedRestoreFence(appState, users, &stubFenceAnchor{writeErr: unwritable}).Enforce(context.Background())
	if err != nil {
		t.Fatalf("an unwritable fence must not fail the boot: %v", err)
	}
	if !outcome.Unanchored || outcome.DisarmedFeeds != 5 {
		t.Fatalf("expected an unanchored disarm of 5, got %+v", outcome)
	}
	if len(appState.values) != 0 {
		t.Fatalf("a fence that was never written must record no marker, got %v", appState.values)
	}
}

// TestCalendarFeedRestoreFenceUnwritableAnchorCountsEachRowOnce guards the one
// arithmetic the two disarm sites share: when the write fails AFTER a
// continuity-broken disarm has already run, the second (idempotent) disarm
// reports zero rows and the total must stay the count of rows actually cleared.
func TestCalendarFeedRestoreFenceUnwritableAnchorCountsEachRowOnce(t *testing.T) {
	appState := &stubFenceAppState{values: map[string]string{
		models.AppStateKeyCalendarFeedRestoreFence: "an-older-generation",
	}}
	users := &countingFenceUserStore{counts: []int64{6, 0}}

	outcome, err := NewCalendarFeedRestoreFence(appState, users, &stubFenceAnchor{
		value: fenceTestToken, found: true, writeErr: errors.New("read-only file system"),
	}).Enforce(context.Background())
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if !outcome.Unanchored || outcome.DisarmedFeeds != 6 {
		t.Fatalf("expected 6 rows counted once, got %+v", outcome)
	}
}

// TestCalendarFeedRestoreFencePropagatesDatabaseFailures separates the two
// failure classes the fence deliberately treats differently: a DATABASE error
// is returned and fails the boot, and a failed disarm must leave the stored
// marker untouched so the whole pass retries on the next start.
func TestCalendarFeedRestoreFencePropagatesDatabaseFailures(t *testing.T) {
	readFailure := errors.New("app_state read failed")
	if _, err := NewCalendarFeedRestoreFence(
		&stubFenceAppState{getErr: readFailure},
		&stubFenceUserStore{},
		&stubFenceAnchor{value: fenceTestToken, found: true},
	).Enforce(context.Background()); !errors.Is(err, readFailure) {
		t.Fatalf("expected the app_state read failure, got %v", err)
	}

	disarmFailure := errors.New("disarm failed")
	appState := &stubFenceAppState{values: map[string]string{
		models.AppStateKeyCalendarFeedRestoreFence: "an-older-generation",
	}}
	anchor := &stubFenceAnchor{value: fenceTestToken, found: true}
	if _, err := NewCalendarFeedRestoreFence(appState, &stubFenceUserStore{disarmErr: disarmFailure}, anchor).Enforce(context.Background()); !errors.Is(err, disarmFailure) {
		t.Fatalf("expected the disarm failure, got %v", err)
	}
	if got := appState.values[models.AppStateKeyCalendarFeedRestoreFence]; got != "an-older-generation" {
		t.Fatalf("a failed disarm must keep the old marker, got %q", got)
	}
	if anchor.written != "" {
		t.Fatalf("a failed disarm must not write the fence file, got %q", anchor.written)
	}
}

// TestCalendarFeedRestoreFenceAdvanceMovesBothHalvesTogether pins what makes
// the boot comparison able to see a restore at all. A token minted once per
// boot agrees with any backup taken during that same boot — and the supported
// procedure takes the backup with the app stopped, so that is the ordinary
// case, not a corner. Advancing on the change itself is what puts the backup's
// copy behind the file's.
func TestCalendarFeedRestoreFenceAdvanceMovesBothHalvesTogether(t *testing.T) {
	appState := &stubFenceAppState{values: map[string]string{
		models.AppStateKeyCalendarFeedRestoreFence: fenceTestToken,
	}}
	anchor := &stubFenceAnchor{value: fenceTestToken, found: true}
	fence := NewCalendarFeedRestoreFence(appState, &stubFenceUserStore{}, anchor)

	if err := fence.Advance(context.Background()); err != nil {
		t.Fatalf("Advance: %v", err)
	}

	advanced := appState.values[models.AppStateKeyCalendarFeedRestoreFence]
	if advanced == fenceTestToken || advanced == "" {
		t.Fatalf("the database half must move to a fresh token, got %q", advanced)
	}
	if anchor.written != advanced {
		t.Fatalf("both halves must hold the same token; file %q, app_state %q", anchor.written, advanced)
	}

	// And the boot that follows sees an ordinary restart, not a restore.
	outcome, err := fence.Enforce(context.Background())
	if err != nil {
		t.Fatalf("Enforce after Advance: %v", err)
	}
	if outcome != (CalendarFeedRestoreFenceOutcome{}) {
		t.Fatalf("a boot after an advance must be a no-op, got %+v", outcome)
	}
}

// TestCalendarFeedRestoreFenceAdvanceRecordsNothingWithoutAFence pins the
// opposite of the case below, and the two only make sense together. A fence
// that is not CONFIGURED needs no record at all: Enforce's unanchored path
// already disarms every armed feed on every boot, so writing the database half
// alone would add a per-request write and a disagreement nothing ever reads.
// A fence that is configured but unwritable is the case below, and there the
// database half moving alone IS the answer.
func TestCalendarFeedRestoreFenceAdvanceRecordsNothingWithoutAFence(t *testing.T) {
	appState := &stubFenceAppState{values: map[string]string{}}
	anchor := &stubFenceAnchor{writeErr: security.ErrCalendarFeedFenceNotConfigured}
	fence := NewCalendarFeedRestoreFence(appState, &stubFenceUserStore{}, anchor)

	if err := fence.Advance(context.Background()); err != nil {
		t.Fatalf("an unconfigured fence must not fail a revocation: %v", err)
	}
	if len(appState.values) != 0 {
		t.Fatalf("an unconfigured fence must record nothing, got %v", appState.values)
	}
	if anchor.written != "" {
		t.Fatalf("an unconfigured fence must write no file, got %q", anchor.written)
	}
}

// TestCalendarFeedRestoreFenceAdvanceBreaksTheFenceWhenTheFileRefuses pins the
// fail-closed half. An owner revoking a feed must not be refused because a
// volume could not be written, and the revocation must not be reported as
// durable when it is not. Letting the database half move on ALONE satisfies
// both: the call succeeds, the halves now disagree, and the next boot answers
// that by disarming every feed.
func TestCalendarFeedRestoreFenceAdvanceBreaksTheFenceWhenTheFileRefuses(t *testing.T) {
	appState := &stubFenceAppState{values: map[string]string{
		models.AppStateKeyCalendarFeedRestoreFence: fenceTestToken,
	}}
	anchor := &stubFenceAnchor{value: fenceTestToken, found: true, writeErr: errors.New("read-only file system")}
	// Row counts per call, as the real bulk disarm reports them: the rows go on
	// the first pass and the second finds none.
	users := &countingFenceUserStore{counts: []int64{2, 0}}
	fence := NewCalendarFeedRestoreFence(appState, users, anchor)

	if err := fence.Advance(context.Background()); err != nil {
		t.Fatalf("a revocation must not fail because the fence file refused: %v", err)
	}
	if anchor.value != fenceTestToken {
		t.Fatalf("the file half must be left where it was, got %q", anchor.value)
	}
	if appState.values[models.AppStateKeyCalendarFeedRestoreFence] == fenceTestToken {
		t.Fatal("the database half must move on alone, so the halves disagree")
	}

	outcome, err := fence.Enforce(context.Background())
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if !outcome.Unanchored || outcome.DisarmedFeeds != 2 {
		t.Fatalf("the next boot must disarm; got %+v", outcome)
	}
}

// TestCalendarFeedRestoreFenceConcurrentAdvancesLeaveTheHalvesAgreeing pins the
// one thing the two writes cannot be trusted to do on their own. They run one
// after the other, so without serialization two concurrent revocations can land
// as the file from one and the marker from the other — halves that disagree
// with nothing restored, which the next boot answers by disarming every armed
// feed on the instance. The doubles carry their own locks, so what fails here
// is the fence's serialization and not a data race inside the test.
func TestCalendarFeedRestoreFenceConcurrentAdvancesLeaveTheHalvesAgreeing(t *testing.T) {
	appState := &serializedFenceAppState{}
	anchor := &serializedFenceAnchor{}
	fence := NewCalendarFeedRestoreFence(appState, &stubFenceUserStore{}, anchor)

	var waiting sync.WaitGroup
	for range 16 {
		waiting.Add(1)
		go func() {
			defer waiting.Done()
			if err := fence.Advance(context.Background()); err != nil {
				t.Errorf("Advance: %v", err)
			}
		}()
	}
	waiting.Wait()

	if appState.last() == "" {
		t.Fatal("no advance reached app_state")
	}
	if anchor.last() != appState.last() {
		t.Fatalf("the halves must end on the same token; file %q, app_state %q", anchor.last(), appState.last())
	}
}

// TestCalendarFeedRestoreFenceAdvanceIsMutuallyExclusive states the property
// the end-state test above can only sample: no two advances are ever inside the
// pair of writes at once. It is the positive anchor for that test, because a
// pair of interleaved writes can happen to end on the same token and read as a
// pass — the anchor asserts the mechanism instead of one of its outcomes.
//
// The anchor's Write announces that it was entered and then waits, briefly, for
// a second entrant. A serialized fence cannot produce one, because the second
// caller is still queued on the lock, so the wait times out and that timeout IS
// the proof. An unserialized fence produces it immediately and fails here.
func TestCalendarFeedRestoreFenceAdvanceIsMutuallyExclusive(t *testing.T) {
	anchor := newOverlapDetectingFenceAnchor()
	fence := NewCalendarFeedRestoreFence(&serializedFenceAppState{}, &stubFenceUserStore{}, anchor)

	var waiting sync.WaitGroup
	for range 2 {
		waiting.Add(1)
		go func() {
			defer waiting.Done()
			if err := fence.Advance(context.Background()); err != nil {
				t.Errorf("Advance: %v", err)
			}
		}()
	}

	// Wait for the first caller to be inside, then give the second one the same
	// chance to arrive. Serialized, it cannot: it is queued on the fence's lock
	// while this one is held here.
	<-anchor.arrived
	select {
	case <-anchor.arrived:
		anchor.overlapped.Store(true)
	case <-time.After(500 * time.Millisecond):
	}
	close(anchor.release)
	waiting.Wait()

	if anchor.overlapped.Load() {
		t.Fatal("two advances were inside the pair of writes at once: they can then leave the file holding one token and app_state another, which the next boot disarms every armed feed for")
	}
	// The counter has to have moved, or the anchor was never entered and the
	// assertion above is about nothing.
	if anchor.entries.Load() != 2 {
		t.Fatalf("expected both advances to reach the anchor, got %d", anchor.entries.Load())
	}
}

// overlapDetectingFenceAnchor counts how many callers are inside Write at once,
// and holds the first one there until the test releases it. The wait is a
// handshake rather than a sleep on purpose: a fixed delay would let an
// unserialized fence pass whenever the second goroutine happened to be
// scheduled late, which is the one direction this guard must not be wrong in.
// The bound exists only so a serialized run terminates.
type overlapDetectingFenceAnchor struct {
	mutex      sync.Mutex
	value      string
	inside     atomic.Int32
	entries    atomic.Int32
	overlapped atomic.Bool
	arrived    chan struct{}
	release    chan struct{}
}

func newOverlapDetectingFenceAnchor() *overlapDetectingFenceAnchor {
	return &overlapDetectingFenceAnchor{
		arrived: make(chan struct{}, 2),
		release: make(chan struct{}),
	}
}

func (s *overlapDetectingFenceAnchor) Read() (string, bool, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.value, s.value != "", nil
}

func (s *overlapDetectingFenceAnchor) Write(value string) error {
	s.entries.Add(1)
	if s.inside.Add(1) > 1 {
		s.overlapped.Store(true)
	}
	s.arrived <- struct{}{}
	// Wait to be released, so the first caller is still INSIDE while the test
	// looks. A serialized fence keeps the second caller on the lock and never
	// closes this, hence the bound.
	select {
	case <-s.release:
	case <-time.After(2 * time.Second):
	}
	s.inside.Add(-1)

	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.value = value
	return nil
}

type serializedFenceAppState struct {
	mutex sync.Mutex
	value string
}

func (s *serializedFenceAppState) Get(_ context.Context, _ string) (string, bool, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.value, s.value != "", nil
}

func (s *serializedFenceAppState) Set(_ context.Context, _ string, value string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.value = value
	return nil
}

func (s *serializedFenceAppState) last() string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.value
}

type serializedFenceAnchor struct {
	mutex sync.Mutex
	value string
}

func (s *serializedFenceAnchor) Read() (string, bool, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.value, s.value != "", nil
}

func (s *serializedFenceAnchor) Write(value string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.value = value
	return nil
}

func (s *serializedFenceAnchor) last() string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.value
}

// countingFenceUserStore returns a different row count per call, so a test can
// tell one disarm's rows from the next call's.
type countingFenceUserStore struct {
	counts []int64
	calls  int
}

func (s *countingFenceUserStore) DisarmAllCalendarFeedTokens(_ context.Context) (int64, error) {
	count := int64(0)
	if s.calls < len(s.counts) {
		count = s.counts[s.calls]
	}
	s.calls++
	return count, nil
}

// TestCalendarFeedRestoreFenceEnforceFailsTheBootWhenTheMarkerCannotBeStored
// pins the boot pass's DATABASE arm, the one failure class Enforce is supposed
// to escalate rather than absorb. The file half has already been written when
// this happens, so the halves now disagree and the next boot would disarm
// anyway — but the boot must still fail loudly here, because a database that
// cannot record a marker after migrations have run is not one this instance
// should start serving from.
func TestCalendarFeedRestoreFenceEnforceFailsTheBootWhenTheMarkerCannotBeStored(t *testing.T) {
	refused := errors.New("app_state is unavailable")
	appState := &stubFenceAppState{values: map[string]string{}, setErr: refused}
	anchor := &stubFenceAnchor{}
	users := &stubFenceUserStore{}
	fence := NewCalendarFeedRestoreFence(appState, users, anchor)

	outcome, err := fence.Enforce(context.Background())
	if !errors.Is(err, refused) {
		t.Fatalf("a refused app_state write must fail the boot, got %v", err)
	}
	if !outcome.FirstBoot {
		t.Fatalf("the outcome must still name the pass it was in, got %+v", outcome)
	}
	// The file half went out before the database half was attempted, which is
	// what leaves the two disagreeing rather than agreeing on a marker the file
	// never received.
	if anchor.written == "" {
		t.Fatal("the file half must be written before the database half is attempted")
	}
	if users.callCount != 0 {
		t.Fatalf("a first boot disarms nothing: an upgrade is not a restore, got %d disarm(s)", users.callCount)
	}
}

// TestCalendarFeedRestoreFenceAdvanceConfirmedAdvancesBothHalvesWhenTheyAgree
// is the one case AdvanceConfirmed is willing to move forward from: the two
// halves already hold the SAME token, so nothing stands between this call and
// the server's own boot-time no-op (TestCalendarFeedRestoreFenceAgreementIsANoOp).
func TestCalendarFeedRestoreFenceAdvanceConfirmedAdvancesBothHalvesWhenTheyAgree(t *testing.T) {
	appState := &stubFenceAppState{values: map[string]string{
		models.AppStateKeyCalendarFeedRestoreFence: fenceTestToken,
	}}
	anchor := &stubFenceAnchor{value: fenceTestToken, found: true}
	fence := NewCalendarFeedRestoreFence(appState, &stubFenceUserStore{}, anchor)

	if err := fence.AdvanceConfirmed(context.Background()); err != nil {
		t.Fatalf("AdvanceConfirmed: %v", err)
	}

	advanced := appState.values[models.AppStateKeyCalendarFeedRestoreFence]
	if advanced == fenceTestToken || advanced == "" {
		t.Fatalf("the database half must move to a fresh token, got %q", advanced)
	}
	if anchor.written != advanced {
		t.Fatalf("both halves must hold the same token; file %q, app_state %q", anchor.written, advanced)
	}
}

// TestCalendarFeedRestoreFenceAdvanceConfirmedRefusesWithoutAnAnchorAndWritesNothing
// pins the state buildRepositories handed every subcommand before this fix: an
// operator's shell with CALENDAR_FEED_FENCE_PATH unset. Unlike Advance, this
// must be a hard refusal — a caller acting on someone else's behalf must never
// let the database half move on alone.
func TestCalendarFeedRestoreFenceAdvanceConfirmedRefusesWithoutAnAnchorAndWritesNothing(t *testing.T) {
	appState := &stubFenceAppState{values: map[string]string{}}
	anchor := &stubFenceAnchor{readErr: security.ErrCalendarFeedFenceNotConfigured}
	fence := NewCalendarFeedRestoreFence(appState, &stubFenceUserStore{}, anchor)

	err := fence.AdvanceConfirmed(context.Background())
	if !errors.Is(err, ErrCalendarFeedFenceUnreachable) || !errors.Is(err, security.ErrCalendarFeedFenceNotConfigured) {
		t.Fatalf("expected ErrCalendarFeedFenceUnreachable wrapping the not-configured cause, got %v", err)
	}
	if len(appState.values) != 0 {
		t.Fatalf("an unreachable anchor must record no marker, got %v", appState.values)
	}
	if anchor.written != "" {
		t.Fatalf("an unreachable anchor must write no file, got %q", anchor.written)
	}
}

// TestCalendarFeedRestoreFenceAdvanceConfirmedRefusesWhenTheAnchorWriteFailsAndLeavesTheDatabaseHalfAlone
// is the one behaviour that must NOT match Advance: TestCalendarFeedRestoreFenceAdvanceBreaksTheFenceWhenTheFileRefuses
// lets the database half move on alone on the same fake shape, and that is
// exactly the defect AdvanceConfirmed exists to close for an operator-driven
// caller — a removal recorded only inside the database, with the file left
// unable to contradict it.
func TestCalendarFeedRestoreFenceAdvanceConfirmedRefusesWhenTheAnchorWriteFailsAndLeavesTheDatabaseHalfAlone(t *testing.T) {
	appState := &stubFenceAppState{values: map[string]string{
		models.AppStateKeyCalendarFeedRestoreFence: fenceTestToken,
	}}
	anchor := &stubFenceAnchor{value: fenceTestToken, found: true, writeErr: errors.New("read-only file system")}
	fence := NewCalendarFeedRestoreFence(appState, &stubFenceUserStore{}, anchor)

	err := fence.AdvanceConfirmed(context.Background())
	if !errors.Is(err, ErrCalendarFeedFenceUnreachable) {
		t.Fatalf("expected ErrCalendarFeedFenceUnreachable, got %v", err)
	}
	if got := appState.values[models.AppStateKeyCalendarFeedRestoreFence]; got != fenceTestToken {
		t.Fatalf("the database half must NOT move on alone, got %q", got)
	}
	if anchor.value != fenceTestToken {
		t.Fatalf("the file half must be left where it was, got %q", anchor.value)
	}
}

// TestCalendarFeedRestoreFenceAdvanceConfirmedRefusesWhenTheHalvesAreNotAKnownAgreeingPairAndWritesNothing
// covers every shape that is not "both halves already hold the same token":
// a real disagreement, either asymmetric shape, and both halves empty — which
// means the server has never booted with this fence configured, and arming it
// is Enforce's job on a first boot, never an operator command's.
func TestCalendarFeedRestoreFenceAdvanceConfirmedRefusesWhenTheHalvesAreNotAKnownAgreeingPairAndWritesNothing(t *testing.T) {
	cases := []struct {
		name            string
		stored          map[string]string
		anchor          *stubFenceAnchor
		wantAnchorFound bool
		wantStoredFound bool
	}{
		{
			name:            "the halves disagree",
			stored:          map[string]string{models.AppStateKeyCalendarFeedRestoreFence: "an-older-generation"},
			anchor:          &stubFenceAnchor{value: fenceTestToken, found: true},
			wantAnchorFound: true,
			wantStoredFound: true,
		},
		{
			name:            "only the database half has a token",
			stored:          map[string]string{models.AppStateKeyCalendarFeedRestoreFence: fenceTestToken},
			anchor:          &stubFenceAnchor{},
			wantAnchorFound: false,
			wantStoredFound: true,
		},
		{
			name:            "only the file half has a token",
			stored:          map[string]string{},
			anchor:          &stubFenceAnchor{value: fenceTestToken, found: true},
			wantAnchorFound: true,
			wantStoredFound: false,
		},
		{
			name:            "both halves are empty",
			stored:          map[string]string{},
			anchor:          &stubFenceAnchor{},
			wantAnchorFound: false,
			wantStoredFound: false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			appState := &stubFenceAppState{values: testCase.stored}
			fence := NewCalendarFeedRestoreFence(appState, &stubFenceUserStore{}, testCase.anchor)

			before := map[string]string{}
			for key, value := range testCase.stored {
				before[key] = value
			}

			err := fence.AdvanceConfirmed(context.Background())
			var continuity *CalendarFeedFenceContinuityError
			if !errors.As(err, &continuity) {
				t.Fatalf("expected *CalendarFeedFenceContinuityError, got %v", err)
			}
			if continuity.AnchorFound != testCase.wantAnchorFound || continuity.StoredFound != testCase.wantStoredFound {
				t.Fatalf("expected AnchorFound=%t StoredFound=%t, got %+v", testCase.wantAnchorFound, testCase.wantStoredFound, continuity)
			}
			if got := appState.values; len(got) != len(before) {
				t.Fatalf("a refused advance must write nothing to app_state, before=%v after=%v", before, got)
			}
			for key, value := range before {
				if appState.values[key] != value {
					t.Fatalf("a refused advance must write nothing to app_state, before=%v after=%v", before, appState.values)
				}
			}
			if testCase.anchor.written != "" {
				t.Fatalf("a refused advance must write no file, got %q", testCase.anchor.written)
			}
		})
	}
}

// TestCalendarFeedRestoreFenceAdvanceConfirmedPropagatesADatabaseFailureAfterTheFileHasAlreadyMoved
// pins the one failure this method cannot avoid recording partially: unlike
// the unreachable-anchor path, a database failure here happens AFTER the file
// half has already moved, so the two now disagree — and AdvanceConfirmed
// returns the raw error (never wrapped in ErrCalendarFeedFenceUnreachable,
// which names an ANCHOR failure) because the caller's operation must be
// reported as failed, and the next boot's Enforce is what actually answers
// the disagreement by disarming.
func TestCalendarFeedRestoreFenceAdvanceConfirmedPropagatesADatabaseFailureAfterTheFileHasAlreadyMoved(t *testing.T) {
	refused := errors.New("app_state is unavailable")
	appState := &stubFenceAppState{values: map[string]string{
		models.AppStateKeyCalendarFeedRestoreFence: fenceTestToken,
	}, setErr: refused}
	anchor := &stubFenceAnchor{value: fenceTestToken, found: true}
	fence := NewCalendarFeedRestoreFence(appState, &stubFenceUserStore{}, anchor)

	err := fence.AdvanceConfirmed(context.Background())
	if !errors.Is(err, refused) {
		t.Fatalf("expected the raw app_state failure, got %v", err)
	}
	if !errors.Is(err, ErrCalendarFeedFenceHalfAdvanced) {
		t.Fatalf("a database write failure after the file moved must say so, got %v", err)
	}
	if errors.Is(err, ErrCalendarFeedFenceUnreachable) || errors.Is(err, ErrCalendarFeedFenceMarkerUnavailable) {
		t.Fatal("a database write failure must not be reported as an anchor failure or an unreadable marker")
	}
	if anchor.written == "" || anchor.written == fenceTestToken {
		t.Fatalf("the file half must already have moved to a fresh token, got %q", anchor.written)
	}
	if got := appState.values[models.AppStateKeyCalendarFeedRestoreFence]; got != fenceTestToken {
		t.Fatalf("the database half must be left at its old value, got %q", got)
	}
}

// TestCalendarFeedRestoreFenceAdvanceConfirmedRefusesWhenTheMarkerCannotBeReadAndWritesNothing
// is the read-side counterpart: the anchor is fine, the database cannot
// answer, and the refusal must be typed as exactly that — not as an anchor
// failure, whose remedy (run the CLI elsewhere) would send the operator the
// wrong way — with neither half written.
func TestCalendarFeedRestoreFenceAdvanceConfirmedRefusesWhenTheMarkerCannotBeReadAndWritesNothing(t *testing.T) {
	unreadable := errors.New("app_state read failed")
	appState := &stubFenceAppState{getErr: unreadable}
	anchor := &stubFenceAnchor{value: fenceTestToken, found: true}
	fence := NewCalendarFeedRestoreFence(appState, &stubFenceUserStore{}, anchor)

	err := fence.AdvanceConfirmed(context.Background())
	if !errors.Is(err, ErrCalendarFeedFenceMarkerUnavailable) || !errors.Is(err, unreadable) {
		t.Fatalf("expected ErrCalendarFeedFenceMarkerUnavailable wrapping the read failure, got %v", err)
	}
	if errors.Is(err, ErrCalendarFeedFenceUnreachable) || errors.Is(err, ErrCalendarFeedFenceHalfAdvanced) {
		t.Fatal("an unreadable marker must not be reported as an anchor failure or a half-advanced fence")
	}
	if anchor.written != "" {
		t.Fatalf("an unreadable marker must write no file, got %q", anchor.written)
	}
	if len(appState.values) != 0 {
		t.Fatalf("an unreadable marker must record nothing, got %v", appState.values)
	}
}

// TestCalendarFeedRestoreFenceAdvanceConfirmedSharesTheMutexWithAdvance proves
// the CLI-facing method and the server-facing one serialize against the SAME
// lock. Mixing 8 Advance calls with 8 AdvanceConfirmed calls, unsynchronized,
// could pair a mint from one call with a write from the other and leave the
// halves disagreeing with nothing restored — the same two-writer race
// TestCalendarFeedRestoreFenceConcurrentAdvancesLeaveTheHalvesAgreeing already
// covers for two Advance calls alone. The pair is seeded to already agree,
// because AdvanceConfirmed correctly refusing a never-armed fence is not the
// property under test here and would be indistinguishable from a real bug in
// the loop below.
func TestCalendarFeedRestoreFenceAdvanceConfirmedSharesTheMutexWithAdvance(t *testing.T) {
	appState := &serializedFenceAppState{}
	anchor := &serializedFenceAnchor{}
	fence := NewCalendarFeedRestoreFence(appState, &stubFenceUserStore{}, anchor)

	if err := fence.Advance(context.Background()); err != nil {
		t.Fatalf("seed Advance: %v", err)
	}

	var waiting sync.WaitGroup
	errs := make(chan error, 16)
	for i := range 16 {
		waiting.Add(1)
		go func(i int) {
			defer waiting.Done()
			var err error
			if i%2 == 0 {
				err = fence.Advance(context.Background())
			} else {
				err = fence.AdvanceConfirmed(context.Background())
			}
			if err != nil {
				errs <- err
			}
		}(i)
	}
	waiting.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent advance: %v", err)
	}

	if appState.last() == "" || anchor.last() != appState.last() {
		t.Fatalf("the halves must end agreeing; file %q, app_state %q", anchor.last(), appState.last())
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

// TestCalendarFeedFenceContinuityErrorNamesWhichHalfHeldAMarker pins the text
// the operator CLI falls back to when it cannot classify the shape itself:
// which half held a marker is what tells a never-booted fence from a restored
// one, so the message must carry both flags.
func TestCalendarFeedFenceContinuityErrorNamesWhichHalfHeldAMarker(t *testing.T) {
	got := (&CalendarFeedFenceContinuityError{AnchorFound: true, StoredFound: false}).Error()
	want := "calendar feed restore fence: the file and the database marker are not a known-agreeing pair (file present=true, database present=false)"
	if got != want {
		t.Fatalf("continuity error text:\n got %q\nwant %q", got, want)
	}
}
