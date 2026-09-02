package services

import (
	"context"
	"errors"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// stubRotationAppState is an in-memory app_state with injectable errors and a
// shared call journal (with stubRotationUserStore) so ordering — disarm before
// the new epoch is recorded — can be asserted, not assumed.
type stubRotationAppState struct {
	values  map[string]string
	getErr  error
	setErr  error
	journal *[]string
}

func (s *stubRotationAppState) Get(_ context.Context, key string) (string, bool, error) {
	if s.getErr != nil {
		return "", false, s.getErr
	}
	value, ok := s.values[key]
	return value, ok, nil
}

func (s *stubRotationAppState) Set(_ context.Context, key string, value string) error {
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

type stubRotationUserStore struct {
	disarmed  int64
	disarmErr error
	callCount int
	journal   *[]string
}

func (s *stubRotationUserStore) DisarmCalendarFeedTokensWithoutMAC(_ context.Context) (int64, error) {
	s.callCount++
	if s.journal != nil {
		*s.journal = append(*s.journal, "disarm")
	}
	if s.disarmErr != nil {
		return 0, s.disarmErr
	}
	return s.disarmed, nil
}

const rotationSentinelTestKeyA = "rotation-sentinel-test-key-A-0123"
const rotationSentinelTestKeyB = "rotation-sentinel-test-key-B-0123"

// TestCalendarFeedRotationSentinelFirstBootOnANewInstallationDisarmsNothing
// pins the quiet half of the absent-epoch case: an installation that has no
// MAC-less feed has nothing to judge, so the disarm finds zero rows, the
// outcome is a pure first boot, and the startup banner stays silent.
func TestCalendarFeedRotationSentinelFirstBootOnANewInstallationDisarmsNothing(t *testing.T) {
	appState := &stubRotationAppState{}
	users := &stubRotationUserStore{disarmed: 0}
	sentinel := NewCalendarFeedRotationSentinel(appState, users, []byte(rotationSentinelTestKeyA))

	outcome, err := sentinel.Enforce(context.Background())
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if !outcome.FirstBoot || outcome.RotationDetected || outcome.DisarmedFeeds != 0 {
		t.Fatalf("expected pure first-boot outcome, got %+v", outcome)
	}

	want, err := security.CalendarFeedKeyEpoch([]byte(rotationSentinelTestKeyA))
	if err != nil {
		t.Fatalf("CalendarFeedKeyEpoch: %v", err)
	}
	if got := appState.values[models.AppStateKeyCalendarFeedKeyEpoch]; got != want {
		t.Fatalf("stored epoch %q, want the derived epoch %q", got, want)
	}
}

// TestCalendarFeedRotationSentinelFirstBootAfterAnUpgradeDisarmsTheMACLessFeeds
// is the other half, and the defect this arm exists for: an upgrade from a
// release that predates the sentinel reaches the same absent-epoch branch, but
// its pre-032 rows carry no MAC and nothing records which key minted them.
// Adopting them as the baseline would let the first poll backfill a MAC under
// today's key and revive a subscribe URL a rotation in that same window was
// meant to kill, so they are disarmed — before the epoch is recorded, so a
// failure retries the disarm instead of blessing it.
func TestCalendarFeedRotationSentinelFirstBootAfterAnUpgradeDisarmsTheMACLessFeeds(t *testing.T) {
	journal := make([]string, 0, 2)
	appState := &stubRotationAppState{journal: &journal}
	users := &stubRotationUserStore{disarmed: 7, journal: &journal}
	sentinel := NewCalendarFeedRotationSentinel(appState, users, []byte(rotationSentinelTestKeyA))

	outcome, err := sentinel.Enforce(context.Background())
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if !outcome.FirstBoot || outcome.RotationDetected || outcome.DisarmedFeeds != 7 {
		t.Fatalf("expected a first boot that disarmed the legacy rows, got %+v", outcome)
	}
	if users.callCount != 1 {
		t.Fatalf("expected exactly one disarm, got %d", users.callCount)
	}
	if len(journal) != 2 || journal[0] != "disarm" || journal[1] != "set" {
		t.Fatalf("expected the disarm to precede the epoch write, got %v", journal)
	}
}

// TestCalendarFeedRotationSentinelFirstBootKeepsTheEpochUnwrittenWhenTheDisarmFails
// pins the retry: a failed disarm on the absent-epoch path must leave no epoch
// behind, or the next boot would read agreement and never retry.
func TestCalendarFeedRotationSentinelFirstBootKeepsTheEpochUnwrittenWhenTheDisarmFails(t *testing.T) {
	appState := &stubRotationAppState{}
	users := &stubRotationUserStore{disarmErr: errors.New("storage down")}
	sentinel := NewCalendarFeedRotationSentinel(appState, users, []byte(rotationSentinelTestKeyA))

	outcome, err := sentinel.Enforce(context.Background())
	if err == nil {
		t.Fatal("expected the storage failure to surface")
	}
	if !outcome.FirstBoot || outcome.DisarmedFeeds != 0 {
		t.Fatalf("expected a failed first-boot outcome, got %+v", outcome)
	}
	if _, stored := appState.values[models.AppStateKeyCalendarFeedKeyEpoch]; stored {
		t.Fatal("a failed disarm must not record the epoch")
	}
}

// TestCalendarFeedRotationSentinelUnchangedKeyIsANoOp pins the steady state: a
// reboot under the same key neither rewrites the marker nor touches users.
func TestCalendarFeedRotationSentinelUnchangedKeyIsANoOp(t *testing.T) {
	epoch, err := security.CalendarFeedKeyEpoch([]byte(rotationSentinelTestKeyA))
	if err != nil {
		t.Fatalf("CalendarFeedKeyEpoch: %v", err)
	}
	journal := []string{}
	appState := &stubRotationAppState{
		values:  map[string]string{models.AppStateKeyCalendarFeedKeyEpoch: epoch},
		journal: &journal,
	}
	users := &stubRotationUserStore{journal: &journal}
	sentinel := NewCalendarFeedRotationSentinel(appState, users, []byte(rotationSentinelTestKeyA))

	outcome, err := sentinel.Enforce(context.Background())
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if outcome != (CalendarFeedRotationOutcome{}) {
		t.Fatalf("expected zero outcome for an unchanged key, got %+v", outcome)
	}
	if len(journal) != 0 {
		t.Fatalf("expected no writes and no disarms for an unchanged key, journal=%v", journal)
	}
}

// TestCalendarFeedRotationSentinelRotationDisarmsThenRecordsNewEpoch is the
// finding's regression: a stored epoch from key A plus a boot under key B must
// disarm legacy rows FIRST and only then record key B's epoch. With the
// sentinel gone (the pre-fix behavior) nothing disarms those rows and the
// first bcrypt poll re-arms the leaked URL under the new key.
func TestCalendarFeedRotationSentinelRotationDisarmsThenRecordsNewEpoch(t *testing.T) {
	oldEpoch, err := security.CalendarFeedKeyEpoch([]byte(rotationSentinelTestKeyA))
	if err != nil {
		t.Fatalf("CalendarFeedKeyEpoch(old): %v", err)
	}
	journal := []string{}
	appState := &stubRotationAppState{
		values:  map[string]string{models.AppStateKeyCalendarFeedKeyEpoch: oldEpoch},
		journal: &journal,
	}
	users := &stubRotationUserStore{disarmed: 3, journal: &journal}
	sentinel := NewCalendarFeedRotationSentinel(appState, users, []byte(rotationSentinelTestKeyB))

	outcome, err := sentinel.Enforce(context.Background())
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if !outcome.RotationDetected || outcome.FirstBoot || outcome.DisarmedFeeds != 3 {
		t.Fatalf("expected rotation outcome with 3 disarmed rows, got %+v", outcome)
	}
	if len(journal) != 2 || journal[0] != "disarm" || journal[1] != "set" {
		t.Fatalf("rotation must disarm BEFORE recording the new epoch, journal=%v", journal)
	}

	newEpoch, err := security.CalendarFeedKeyEpoch([]byte(rotationSentinelTestKeyB))
	if err != nil {
		t.Fatalf("CalendarFeedKeyEpoch(new): %v", err)
	}
	if got := appState.values[models.AppStateKeyCalendarFeedKeyEpoch]; got != newEpoch {
		t.Fatalf("stored epoch %q, want the rotated key's epoch %q", got, newEpoch)
	}
}

// TestCalendarFeedRotationSentinelFailedDisarmKeepsOldEpoch pins the crash- and
// failure-ordering contract: when the bulk disarm fails, the stored epoch must
// stay at the OLD value so the next boot retries the revocation instead of
// considering the rotation handled.
func TestCalendarFeedRotationSentinelFailedDisarmKeepsOldEpoch(t *testing.T) {
	oldEpoch, err := security.CalendarFeedKeyEpoch([]byte(rotationSentinelTestKeyA))
	if err != nil {
		t.Fatalf("CalendarFeedKeyEpoch(old): %v", err)
	}
	appState := &stubRotationAppState{
		values: map[string]string{models.AppStateKeyCalendarFeedKeyEpoch: oldEpoch},
	}
	users := &stubRotationUserStore{disarmErr: errors.New("disk full")}
	sentinel := NewCalendarFeedRotationSentinel(appState, users, []byte(rotationSentinelTestKeyB))

	outcome, err := sentinel.Enforce(context.Background())
	if err == nil {
		t.Fatal("expected the disarm failure to surface as an error")
	}
	if !outcome.RotationDetected {
		t.Fatalf("expected RotationDetected in the failure outcome, got %+v", outcome)
	}
	if got := appState.values[models.AppStateKeyCalendarFeedKeyEpoch]; got != oldEpoch {
		t.Fatalf("a failed disarm must not advance the stored epoch: got %q, want %q", got, oldEpoch)
	}
}

// TestCalendarFeedRotationSentinelPropagatesStateAndKeyFailures sweeps the
// remaining hard-failure branches: unreadable state, unwritable state on first
// boot, and a missing secret key (which must never degrade into an empty
// epoch).
func TestCalendarFeedRotationSentinelPropagatesStateAndKeyFailures(t *testing.T) {
	t.Run("get error", func(t *testing.T) {
		appState := &stubRotationAppState{getErr: errors.New("db closed")}
		sentinel := NewCalendarFeedRotationSentinel(appState, &stubRotationUserStore{}, []byte(rotationSentinelTestKeyA))
		if _, err := sentinel.Enforce(context.Background()); err == nil {
			t.Fatal("expected the Get failure to propagate")
		}
	})
	t.Run("set error on first boot", func(t *testing.T) {
		appState := &stubRotationAppState{setErr: errors.New("read-only fs")}
		sentinel := NewCalendarFeedRotationSentinel(appState, &stubRotationUserStore{}, []byte(rotationSentinelTestKeyA))
		if _, err := sentinel.Enforce(context.Background()); err == nil {
			t.Fatal("expected the first-boot Set failure to propagate")
		}
	})
	t.Run("set error after rotation reports the disarm that already happened", func(t *testing.T) {
		oldEpoch, err := security.CalendarFeedKeyEpoch([]byte(rotationSentinelTestKeyA))
		if err != nil {
			t.Fatalf("CalendarFeedKeyEpoch: %v", err)
		}
		appState := &stubRotationAppState{
			values: map[string]string{models.AppStateKeyCalendarFeedKeyEpoch: oldEpoch},
			setErr: errors.New("read-only fs"),
		}
		users := &stubRotationUserStore{disarmed: 2}
		sentinel := NewCalendarFeedRotationSentinel(appState, users, []byte(rotationSentinelTestKeyB))
		outcome, err := sentinel.Enforce(context.Background())
		if err == nil {
			t.Fatal("expected the post-disarm Set failure to propagate")
		}
		if !outcome.RotationDetected || outcome.DisarmedFeeds != 2 {
			t.Fatalf("outcome must still report the disarm that already ran, got %+v", outcome)
		}
	})
	t.Run("missing secret key", func(t *testing.T) {
		sentinel := NewCalendarFeedRotationSentinel(&stubRotationAppState{}, &stubRotationUserStore{}, nil)
		if _, err := sentinel.Enforce(context.Background()); !errors.Is(err, security.ErrCalendarFeedKeyEpochKeyMissing) {
			t.Fatalf("expected ErrCalendarFeedKeyEpochKeyMissing, got %v", err)
		}
	})
}
