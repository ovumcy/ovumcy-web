package services

import (
	"context"
	"errors"
	"sync"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// calendarFeedRestoreFenceAppState is the narrow app_state surface the fence
// needs: read one marker, upsert one marker.
type calendarFeedRestoreFenceAppState interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key string, value string) error
}

// calendarFeedRestoreFenceUserStore is the single bulk write the fence performs
// when it cannot prove the database is the one this instance last ran with.
type calendarFeedRestoreFenceUserStore interface {
	DisarmAllCalendarFeedTokens(ctx context.Context) (int64, error)
}

// calendarFeedRestoreFenceAnchor is the half of the fence that lives outside
// the database. Read reports ("", false, nil) for "no token yet" and an error
// for every state in which continuity cannot be proved — including "no fence
// configured at all". Implementation: security.CalendarFeedFenceFile.
type calendarFeedRestoreFenceAnchor interface {
	Read() (string, bool, error)
	Write(value string) error
}

// CalendarFeedRestoreFenceOutcome reports what one Enforce pass did, for the
// operator-facing startup log line. At most one of the three flags is set.
type CalendarFeedRestoreFenceOutcome struct {
	// Unanchored is true when the fence could not be read or written: no
	// CALENDAR_FEED_FENCE_PATH, no mount behind it, a read-only or broken path.
	// Continuity is then unprovable on every boot, so every armed feed is
	// disarmed on every boot and no marker is recorded.
	Unanchored bool
	// UnanchoredCause carries why, for the startup line only. Never nil when
	// Unanchored is true.
	UnanchoredCause error
	// FirstBoot is true when neither half held a token: a new installation, or
	// the first boot of an existing one after the fence was introduced. Nothing
	// is disarmed — an upgrade is not a restore, and the feeds an instance is
	// already serving were never revoked.
	FirstBoot bool
	// ContinuityBroken is true when the two halves disagreed: the database was
	// replaced by an older generation of itself (a backup restore), replaced by
	// another database, or the fence directory was recreated. All three mean the
	// revocations this instance performed may be missing from the rows in front
	// of it.
	ContinuityBroken bool
	// DisarmedFeeds counts the armed rows this pass cleared.
	DisarmedFeeds int64
}

// CalendarFeedRestoreFence closes the gap the key-epoch sentinel structurally
// cannot: a backup restore under an UNCHANGED SECRET_KEY.
//
// The sentinel compares the current key epoch against a copy in app_state, and
// that copy is inside the dump a restore replaces. Restoring a backup taken
// before a revocation brings back the feed columns and the epoch together, so
// the two agree, nothing is disarmed, and the subscribe URL the owner revoked
// serves the calendar again. The documented answer used to be a step in the
// operator's post-restore checklist; containment that holds only while an
// operator remembers a manual step is a defect (docs/SECURITY_INVARIANTS.md →
// Calendar feed subscription).
//
// So the fence keeps its second half outside the database, in a file the
// operator mounts from a directory no database backup carries. Both halves hold
// the same opaque token while one instance keeps writing one database; after a
// restore the file holds the token this run minted and the restored app_state
// holds an older one. That disagreement is the restore, and it is visible with
// SECRET_KEY untouched, on SQLite and on Postgres alike.
//
// Like the sentinel it runs once per boot, after migrations and before the
// listener starts, so no feed poll can race the disarm.
type CalendarFeedRestoreFence struct {
	appState calendarFeedRestoreFenceAppState
	users    calendarFeedRestoreFenceUserStore
	anchor   calendarFeedRestoreFenceAnchor
	// writing makes one update of the pair atomic against every other update
	// through THIS fence. Both halves are written one after the other, so two
	// concurrent revocations could otherwise interleave — file from one, marker
	// from the other — and leave the halves holding different tokens, which the
	// next boot reads as a restore and answers by disarming every armed feed.
	//
	// The lock covers one instance, not one path, so a binary must build ONE
	// fence and share it: bootstrap.BuildRepositories returns the fence it
	// attached for exactly that reason, and a second BuildRepositories call over
	// the same path would hand out a second mutex that guards nothing against
	// the first. The operator CLI is a separate process and therefore outside
	// this lock too — rare and operator-driven, where the request paths run
	// concurrently by construction.
	writing sync.Mutex
}

// NewCalendarFeedRestoreFence wires the fence.
func NewCalendarFeedRestoreFence(appState calendarFeedRestoreFenceAppState, users calendarFeedRestoreFenceUserStore, anchor calendarFeedRestoreFenceAnchor) *CalendarFeedRestoreFence {
	return &CalendarFeedRestoreFence{appState: appState, users: users, anchor: anchor}
}

// Enforce performs the boot-time check.
//
// Two failure classes are deliberately kept apart. A DATABASE error is returned
// and fails the boot, exactly as the sibling sentinel's does: a database that
// cannot answer will not serve either. An ANCHOR error is not — an unmounted
// fence volume is an ordinary operator state, and refusing to start over it
// would take an instance down for a feature most instances do not use. It fails
// closed instead: disarm everything, record nothing, and say so on every start
// until the mount is there.
//
// Ordering matches the sibling: the disarm runs BEFORE either half of the new
// token is recorded, so a crash in between re-runs the disarm on the next boot
// (zero rows the second time) instead of recording a fence whose revocation
// never happened. Between the two halves the file is written first: a crash
// after it leaves the halves disagreeing, which re-runs the pass — while the
// reverse would record agreement the file never reached.
func (fence *CalendarFeedRestoreFence) Enforce(ctx context.Context) (CalendarFeedRestoreFenceOutcome, error) {
	// Held across the whole pass, so record's two writes cannot interleave with
	// an Advance. Nothing serves yet when this runs, so it never contends;
	// taking it keeps the invariant in one place instead of in a comment about
	// boot ordering that a later caller could invalidate.
	fence.writing.Lock()
	defer fence.writing.Unlock()

	anchored, anchorFound, err := fence.anchor.Read()
	if err != nil {
		return fence.disarmUnanchored(ctx, err, 0)
	}

	stored, storedFound, err := fence.appState.Get(ctx, models.AppStateKeyCalendarFeedRestoreFence)
	if err != nil {
		return CalendarFeedRestoreFenceOutcome{}, err
	}

	if !anchorFound && !storedFound {
		return fence.record(ctx, CalendarFeedRestoreFenceOutcome{FirstBoot: true}, 0)
	}
	// Equality is the only proof of continuity, and it needs both halves: a
	// database carrying no token at all is a database this fence never wrote,
	// which is what restoring a pre-fence backup looks like.
	if anchorFound && storedFound && anchored == stored {
		return CalendarFeedRestoreFenceOutcome{}, nil
	}

	disarmed, err := fence.users.DisarmAllCalendarFeedTokens(ctx)
	if err != nil {
		return CalendarFeedRestoreFenceOutcome{ContinuityBroken: true}, err
	}
	return fence.record(ctx, CalendarFeedRestoreFenceOutcome{ContinuityBroken: true, DisarmedFeeds: disarmed}, disarmed)
}

// Advance records that the set of armed calendar feeds just changed, in both
// halves of the fence. It is what makes the boot comparison able to see a
// restore at all: a token minted once per boot agrees with any backup taken
// during that same boot, so restoring one — which is exactly the supported
// procedure, taken with the app stopped — would compare equal and disarm
// nothing. Advancing on the change itself is the same shape the webhook
// revocation epoch uses, for the same reason.
//
// A failure to write the FILE half is never returned — an owner's revocation
// must not be refused because a volume could not be written — but the two
// reasons it can fail are answered differently:
//
//   - NOT CONFIGURED at all. Nothing is recorded, on either half. An instance
//     with no fence already disarms every armed feed on every boot (Enforce's
//     unanchored path), so containment is complete without this write, and
//     moving the database half alone would only add a per-request write and a
//     disagreement the boot pass never reads.
//   - Configured but unwritable — a broken mount, a full disk. The database
//     half moves on ALONE, deliberately: the halves now disagree, and the next
//     boot answers that by disarming. A fence that cannot record a revocation
//     has to fail closed rather than report success.
//
// Only a database failure reaches the caller, whose own write is failing for
// the same reason.
func (fence *CalendarFeedRestoreFence) Advance(ctx context.Context) error {
	fence.writing.Lock()
	defer fence.writing.Unlock()

	token, err := security.NewCalendarFeedFenceToken()
	if err != nil {
		return err // codecov:ignore -- crypto/rand failure; unreachable without an OS-level entropy fault
	}
	if err := fence.anchor.Write(token); errors.Is(err, security.ErrCalendarFeedFenceNotConfigured) {
		return nil
	}
	return fence.appState.Set(ctx, models.AppStateKeyCalendarFeedRestoreFence, token)
}

// record mints the next token and stores it in both halves. A write failure on
// the file half is an anchor failure, not a boot failure, so it degrades into
// the unanchored outcome — which is also the path a first boot with no mount
// takes, since a missing directory reads as "no token" and only fails on write.
func (fence *CalendarFeedRestoreFence) record(ctx context.Context, outcome CalendarFeedRestoreFenceOutcome, disarmed int64) (CalendarFeedRestoreFenceOutcome, error) {
	token, err := security.NewCalendarFeedFenceToken()
	if err != nil {
		return outcome, err // codecov:ignore -- crypto/rand failure; unreachable without an OS-level entropy fault
	}
	if err := fence.anchor.Write(token); err != nil {
		return fence.disarmUnanchored(ctx, err, disarmed)
	}
	if err := fence.appState.Set(ctx, models.AppStateKeyCalendarFeedRestoreFence, token); err != nil {
		return outcome, err
	}
	return outcome, nil
}

// disarmUnanchored is the fail-closed path: without a usable fence nothing can
// prove this database still holds the revocations this instance performed, so
// every armed feed goes. alreadyDisarmed carries the count from a disarm that
// ran earlier in the same pass, so the reported total counts each row once.
func (fence *CalendarFeedRestoreFence) disarmUnanchored(ctx context.Context, cause error, alreadyDisarmed int64) (CalendarFeedRestoreFenceOutcome, error) {
	disarmed, err := fence.users.DisarmAllCalendarFeedTokens(ctx)
	return CalendarFeedRestoreFenceOutcome{
		Unanchored:      true,
		UnanchoredCause: cause,
		DisarmedFeeds:   alreadyDisarmed + disarmed,
	}, err
}
