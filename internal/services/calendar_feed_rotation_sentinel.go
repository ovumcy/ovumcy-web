package services

import (
	"context"
	"crypto/subtle"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// calendarFeedRotationAppState is the narrow app_state surface the sentinel
// needs: read one marker, upsert one marker.
type calendarFeedRotationAppState interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key string, value string) error
}

// calendarFeedRotationUserStore is the single bulk write the sentinel performs
// when it detects a rotation.
type calendarFeedRotationUserStore interface {
	DisarmCalendarFeedTokensWithoutMAC(ctx context.Context) (int64, error)
}

// CalendarFeedRotationOutcome reports what one Enforce pass did, for the
// operator-facing startup log line.
type CalendarFeedRotationOutcome struct {
	// FirstBoot is true when no epoch was stored yet: the current one was
	// recorded without disarming anything, because nothing is known about the
	// key that came before. (This is also why rotating SECRET_KEY in the same
	// maintenance window as upgrading to the release that introduced the
	// sentinel is not detectable — the runbook tells the operator to revoke
	// feeds manually in that one case.)
	FirstBoot bool
	// RotationDetected is true when the stored epoch did not match the current
	// key's — SECRET_KEY was rotated (or the feed-MAC label set was bumped)
	// since the previous boot.
	RotationDetected bool
	// DisarmedFeeds counts the legacy pre-032 rows cleared by this pass.
	DisarmedFeeds int64
}

// CalendarFeedRotationSentinel closes the one gap in "a SECRET_KEY rotation
// disarms armed calendar feeds": rows minted before migration 032 carry no
// verifier MAC, so the feed verifies them through bcrypt — which does not
// depend on SECRET_KEY — and the first successful poll would then backfill a
// MAC derived from the NEW key, silently re-arming a leaked subscribe URL the
// rotation was meant to kill.
//
// The sentinel runs once per server boot, after migrations and before the
// listener starts (so no poll can race it): it derives the current
// calendar-feed key epoch (security.CalendarFeedKeyEpoch — irreversible, and
// it also changes on a feed-MAC label bump), compares it with the epoch stored
// in app_state, and on a mismatch disarms every armed row without a MAC. Rows
// WITH a MAC are left alone: the rotated key already makes their verification
// a hard refusal, and not touching them keeps a boot with a mistyped key from
// irreversibly clearing anything beyond the legacy rows.
type CalendarFeedRotationSentinel struct {
	appState  calendarFeedRotationAppState
	users     calendarFeedRotationUserStore
	secretKey []byte
}

// NewCalendarFeedRotationSentinel wires the sentinel. secretKey is the same
// key material every other derived domain uses; an empty key fails Enforce
// hard rather than producing an empty epoch.
func NewCalendarFeedRotationSentinel(appState calendarFeedRotationAppState, users calendarFeedRotationUserStore, secretKey []byte) *CalendarFeedRotationSentinel {
	return &CalendarFeedRotationSentinel{appState: appState, users: users, secretKey: secretKey}
}

// Enforce performs the boot-time check. Ordering is deliberate: on a detected
// rotation the disarm runs BEFORE the new epoch is recorded, so a crash in
// between re-runs the disarm on the next boot (zero rows the second time,
// harmless) instead of recording an epoch whose revocation never happened. A
// failed disarm therefore leaves the stored epoch untouched and the whole pass
// retries on the next start.
func (sentinel *CalendarFeedRotationSentinel) Enforce(ctx context.Context) (CalendarFeedRotationOutcome, error) {
	epoch, err := security.CalendarFeedKeyEpoch(sentinel.secretKey)
	if err != nil {
		return CalendarFeedRotationOutcome{}, err
	}

	stored, found, err := sentinel.appState.Get(ctx, models.AppStateKeyCalendarFeedKeyEpoch)
	if err != nil {
		return CalendarFeedRotationOutcome{}, err
	}
	if !found {
		if err := sentinel.appState.Set(ctx, models.AppStateKeyCalendarFeedKeyEpoch, epoch); err != nil {
			return CalendarFeedRotationOutcome{}, err
		}
		return CalendarFeedRotationOutcome{FirstBoot: true}, nil
	}
	// Constant-time by house rule for key-derived comparisons; the timing of a
	// local boot-path compare is not attacker-observable, so this is hygiene,
	// not a load-bearing control.
	if subtle.ConstantTimeCompare([]byte(stored), []byte(epoch)) == 1 {
		return CalendarFeedRotationOutcome{}, nil
	}

	disarmed, err := sentinel.users.DisarmCalendarFeedTokensWithoutMAC(ctx)
	if err != nil {
		return CalendarFeedRotationOutcome{RotationDetected: true}, err
	}
	if err := sentinel.appState.Set(ctx, models.AppStateKeyCalendarFeedKeyEpoch, epoch); err != nil {
		return CalendarFeedRotationOutcome{RotationDetected: true, DisarmedFeeds: disarmed}, err
	}
	return CalendarFeedRotationOutcome{RotationDetected: true, DisarmedFeeds: disarmed}, nil
}
