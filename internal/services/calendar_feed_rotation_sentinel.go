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
	// FirstBoot is true when no epoch was stored yet — a new installation, or
	// the first start after upgrading from a release that predates the
	// sentinel. Nothing is known about the key that came before, so any
	// MAC-less row present is disarmed rather than adopted as the baseline and
	// counted in DisarmedFeeds; on a new installation there are none.
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
//
// A stored epoch that is ABSENT is not a third routine case: see Enforce. It
// means a new installation or the first start after upgrading past the
// sentinel, and only one of those can hold a MAC-less feed — so the same
// disarm runs there, and finds nothing on a new installation.
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
		// No stored epoch has two causes and they must not be answered alike.
		// A new installation has no feed to judge: the disarm below finds
		// nothing and the epoch is simply recorded. An UPGRADE from a release
		// that predates the sentinel does have feeds, and the pre-032 rows
		// among them carry no MAC, so nothing here can tell which key minted
		// them. Recording the current epoch beside them adopts them as the
		// baseline, and the first poll then backfills a MAC derived from
		// today's key — reviving a subscribe URL a rotation in that same
		// maintenance window was meant to kill, which is the containment rule
		// this sentinel exists for. There is no evidence to adopt, so they are
		// disarmed and their owners generate a fresh URL from Settings.
		//
		// Same ordering as the rotation arm below: disarm first, record after,
		// so a crash in between retries instead of recording an epoch whose
		// disarm never happened.
		disarmed, err := sentinel.users.DisarmCalendarFeedTokensWithoutMAC(ctx)
		if err != nil {
			return CalendarFeedRotationOutcome{FirstBoot: true}, err
		}
		if err := sentinel.appState.Set(ctx, models.AppStateKeyCalendarFeedKeyEpoch, epoch); err != nil {
			return CalendarFeedRotationOutcome{FirstBoot: true, DisarmedFeeds: disarmed}, err
		}
		return CalendarFeedRotationOutcome{FirstBoot: true, DisarmedFeeds: disarmed}, nil
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
