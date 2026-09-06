package services

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// calendarFeedRestoreFenceAppState is the narrow app_state surface the fence
// needs: read a marker, upsert a marker, drop a marker. Delete is idempotent —
// a key that is not there is not an error — because the fence erases its
// unanchored marker on every boot that records a token, without reading first.
type calendarFeedRestoreFenceAppState interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key string, value string) error
	Delete(ctx context.Context, key string) error
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
// operator-facing startup log line. At most one of the three outcome flags —
// Unanchored, FirstBoot, ContinuityBroken — is set. UnanchoredHistory is not a
// fourth outcome but a qualifier on ContinuityBroken: it says WHICH shape of
// broken continuity this was, because the operator remedy and the surprise
// differ (nothing was restored; a fence was finally mounted).
type CalendarFeedRestoreFenceOutcome struct {
	// Unanchored is true when the fence could not be read or written: no
	// CALENDAR_FEED_FENCE_PATH, no mount behind it, a read-only or broken path.
	// Continuity is then unprovable on every boot, so every armed feed is
	// disarmed on every boot. No fence TOKEN is recorded — there is nowhere
	// outside the database to put its other half — but the database is stamped
	// as having booted this way, which is what lets the first boot that finally
	// has a fence tell this history from a fresh installation.
	Unanchored bool
	// UnanchoredCause carries why, for the startup line only. Never nil when
	// Unanchored is true.
	UnanchoredCause error
	// FirstBoot is true when neither half held a token AND the database carries
	// no unanchored stamp: a new installation, or the first boot of an existing
	// one after the fence was introduced. Nothing is disarmed — an upgrade is
	// not a restore, and the feeds an instance is already serving were never
	// revoked.
	FirstBoot bool
	// ContinuityBroken is true when continuity could not be proved with a fence
	// that itself worked: the two halves disagreed — the database was replaced
	// by an older generation of itself (a backup restore), replaced by another
	// database, or the fence directory was recreated — or both halves were empty
	// over a database stamped as having run unanchored (UnanchoredHistory). All
	// of them mean the revocations this instance performed may be missing from
	// the rows in front of it.
	ContinuityBroken bool
	// UnanchoredHistory qualifies ContinuityBroken: this is the FIRST boot with
	// a working fence over a database that ran without one. Neither half held a
	// token, so nothing disagreed and nothing was necessarily restored; what
	// broke continuity is the gap itself, during which a revocation could have
	// been made and then rolled back by a restore no marker could contradict.
	// The operator sees a disarm on a start where nothing appears to have
	// happened, so the startup line has to say which of the two this was.
	UnanchoredHistory bool
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
// closed instead: disarm everything, record no token — there is nowhere outside
// the database to keep its other half — and say so on every start until the
// mount is there. It does stamp the database as having booted that way, because
// that stamp is what the first boot WITH a fence needs in order to tell this
// history from a brand-new installation; a stamped database whose two halves
// are both empty is a restore that cannot be ruled out, not a first boot.
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
		// Two very different histories look identical here, and only the stamp
		// tells them apart: an instance that never had a fence at all, and one
		// that SERVED without a usable fence and is now being booted with one.
		// In the second, the gap is the hole: an owner could have revoked a feed
		// while nothing outside the database recorded it, a backup taken before
		// that revocation carries the armed row and no fence token, and restoring
		// it lands exactly here — both halves empty — where calling it a first
		// boot would hand the old subscribe URL back. So the stamp is read ONLY
		// in this branch. Reading it anywhere else would turn a stamp left behind
		// by an older unanchored run into a disarm-all on every start, which is
		// the one thing this pass must never become.
		unanchoredHistory, err := fence.unanchoredHistory(ctx)
		if err != nil {
			return CalendarFeedRestoreFenceOutcome{}, err
		}
		if !unanchoredHistory {
			return fence.record(ctx, CalendarFeedRestoreFenceOutcome{FirstBoot: true}, 0)
		}
		disarmed, err := fence.users.DisarmAllCalendarFeedTokens(ctx)
		if err != nil {
			return CalendarFeedRestoreFenceOutcome{ContinuityBroken: true, UnanchoredHistory: true}, err
		}
		return fence.record(ctx, CalendarFeedRestoreFenceOutcome{
			ContinuityBroken:  true,
			UnanchoredHistory: true,
			DisarmedFeeds:     disarmed,
		}, disarmed)
	}
	// Equality is the only proof of continuity, and it needs both halves: a
	// database carrying no token at all is a database this fence never wrote,
	// which is what restoring a pre-fence backup looks like.
	if continuityHolds(anchorFound, storedFound, anchored, stored) {
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
// the same reason. That case has a third outcome, and it is the expensive one:
// the file half has already been written, so the halves disagree and the next
// boot disarms EVERY armed feed on the instance — not only the one whose write
// failed. One transient app_state error therefore costs every owner their
// subscribe URL. It is priced in rather than avoided: the halves are written
// one after the other with no transaction spanning both, so some order has to
// carry the risk, and file-then-database is the order whose failure disarms
// too much instead of too little. The reverse would leave the halves agreeing
// on a token minted before a revocation that did happen, which is the defect
// this whole mechanism exists to close.
//
// Which is also why a caller that has already COMMITTED its own write drops
// this error instead of returning it: by then the fence has failed closed on
// its own, and the operation being reported as failed would be a second,
// larger lie than the missing marker.
//
// This method is the SERVER's own: it is called on behalf of the request that
// is itself arming, rotating or removing the caller's OWN feed, where refusing
// for want of a mounted volume would be the wrong trade. It is never the right
// call for a process acting on someone ELSE's behalf — an operator revoking
// another account's feed (`users delete`, a forced password reset) — because
// there "the database half moves on alone" stops being a safe degradation and
// becomes the defect this mechanism exists to close: a removal recorded
// nowhere a restore of an older backup could contradict. AdvanceConfirmed
// below exists for exactly that caller, and refuses instead.
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

// ErrCalendarFeedFenceUnreachable is returned by AdvanceConfirmed when this
// process cannot prove it is reading and writing the SAME anchor the server
// does: CALENDAR_FEED_FENCE_PATH unset, no mount behind it, or any other
// anchor failure, including one surfaced by the read half. It always wraps
// the underlying cause.
//
// Advance answers the identical failure by letting the database half move on
// alone (see its own doc comment) because an owner's request to change their
// OWN feed must not be refused for want of a mounted volume. AdvanceConfirmed
// exists for the opposite caller — an operator-driven revocation that is not
// itself the fence's owner — where that same degradation would BE the defect
// this whole mechanism exists to close. So on this path AdvanceConfirmed
// writes NOTHING, in either half, and the caller refuses the operation
// entirely instead.
var ErrCalendarFeedFenceUnreachable = errors.New("calendar feed restore fence: could not confirm continuity")

// ErrCalendarFeedFenceMarkerUnavailable is returned by AdvanceConfirmed when
// the DATABASE half could not be read. It is kept apart from
// ErrCalendarFeedFenceUnreachable because the operator remedy differs — this
// is the database not answering, not a path that is unset or unmounted — and
// because, like that error, it guarantees nothing was written in either half.
// It always wraps the storage error.
var ErrCalendarFeedFenceMarkerUnavailable = errors.New("calendar feed restore fence: could not read the database marker")

// ErrCalendarFeedFenceHalfAdvanced is returned by AdvanceConfirmed when the
// file half was written and the database half then could not be. It is the
// one error out of that method after which something HAS changed: the file is
// ahead of app_state, so the server's next boot reads that as a restore and
// disarms every armed feed — fail closed, the same answer Advance's own
// unreachable-anchor path gives, reached through a different door. A caller
// must not tell the operator that nothing changed on this error. It always
// wraps the storage error.
var ErrCalendarFeedFenceHalfAdvanced = errors.New("calendar feed restore fence: the file advanced but the database marker did not")

// CalendarFeedFenceStepError is every AdvanceConfirmed failure that has an
// underlying cause: the Sentinel names WHICH step refused and what it left
// behind, the Cause is the failure that step hit. Both are reachable through
// errors.Is/errors.As, so a caller switching on the sentinels above keeps
// working unchanged while one that wants to SHOW the cause — the operator CLI,
// whose refusal quotes it — asks for this type by name instead of picking a
// member out of an Unwrap() []error slice by position.
type CalendarFeedFenceStepError struct {
	Sentinel error
	Cause    error
}

func (err *CalendarFeedFenceStepError) Error() string {
	return err.Sentinel.Error() + ": " + err.Cause.Error()
}

// Unwrap returns both members, which is what makes errors.Is match the
// sentinel and errors.As reach anything typed inside the cause.
func (err *CalendarFeedFenceStepError) Unwrap() []error {
	return []error{err.Sentinel, err.Cause}
}

// CalendarFeedFenceContinuityError is returned by AdvanceConfirmed when the
// anchor was reachable but the two halves are not the one pair AdvanceConfirmed
// is willing to move forward from: both already holding the SAME token.
// AnchorFound and StoredFound report which half(s) held one, because the
// operator remedy differs by shape: both false means the server has never
// booted with this fence configured at all — arming it is Enforce's job on a
// first boot, never this method's — while any other combination is the same
// disagreement a restored backup produces. Either way AdvanceConfirmed writes
// nothing: a caller that cannot prove continuity must refuse, not guess which
// half is right.
type CalendarFeedFenceContinuityError struct {
	AnchorFound, StoredFound bool
}

func (err *CalendarFeedFenceContinuityError) Error() string {
	return fmt.Sprintf(
		"calendar feed restore fence: the file and the database marker are not a known-agreeing pair (file present=%t, database present=%t)",
		err.AnchorFound, err.StoredFound,
	)
}

// AdvanceConfirmed is Advance's fail-closed sibling for a caller that is not
// the fence's own owner-facing server process — today, the operator CLI
// revoking a feed on someone else's behalf (`users delete`, a forced password
// reset). Both methods answer the same question — "record that the set of
// armed feeds just changed" — but must answer an unreachable or disagreeing
// fence oppositely, for the reason ErrCalendarFeedFenceUnreachable's doc
// comment gives.
//
// It refuses unless the anchor can be read AND the two halves already hold
// the SAME token — the one condition Enforce calls a no-op — and only then
// mints, writes the file, then the database marker, in that order, same as
// Advance. A failure writing the database half after the file has already
// moved is still returned here (unlike Advance, which has nothing left to
// protect by dropping it), wrapped in ErrCalendarFeedFenceHalfAdvanced: the
// file is now ahead of app_state, so the very next boot reads that as a
// restore and disarms every armed feed.
//
// The errors are typed by what they leave behind, and a caller may rely on
// it: every error returned after anchor.Write wraps
// ErrCalendarFeedFenceHalfAdvanced, so an error that does not — an anchor
// failure, an unreadable marker, a pair that does not agree, or the bare
// error of a token that could not be minted — means nothing was written in
// either half.
func (fence *CalendarFeedRestoreFence) AdvanceConfirmed(ctx context.Context) error {
	fence.writing.Lock()
	defer fence.writing.Unlock()

	anchored, anchorFound, err := fence.anchor.Read()
	if err != nil {
		return &CalendarFeedFenceStepError{Sentinel: ErrCalendarFeedFenceUnreachable, Cause: err}
	}

	stored, storedFound, err := fence.appState.Get(ctx, models.AppStateKeyCalendarFeedRestoreFence)
	if err != nil {
		return &CalendarFeedFenceStepError{Sentinel: ErrCalendarFeedFenceMarkerUnavailable, Cause: err}
	}

	if !continuityHolds(anchorFound, storedFound, anchored, stored) {
		return &CalendarFeedFenceContinuityError{AnchorFound: anchorFound, StoredFound: storedFound}
	}

	token, err := security.NewCalendarFeedFenceToken()
	if err != nil {
		return err // codecov:ignore -- crypto/rand failure; unreachable without an OS-level entropy fault
	}
	if err := fence.anchor.Write(token); err != nil {
		return &CalendarFeedFenceStepError{Sentinel: ErrCalendarFeedFenceUnreachable, Cause: err}
	}
	if err := fence.appState.Set(ctx, models.AppStateKeyCalendarFeedRestoreFence, token); err != nil {
		return &CalendarFeedFenceStepError{Sentinel: ErrCalendarFeedFenceHalfAdvanced, Cause: err}
	}
	return nil
}

// continuityHolds reports whether the two fence halves already prove
// continuity: both hold a token, and it is the SAME one. Enforce treats this
// as its no-op case; AdvanceConfirmed treats anything else — either half
// missing, or both present but different — as a refusal. The two calls are a
// De Morgan pair over the same three facts, spelled out here once so they
// cannot drift apart.
func continuityHolds(anchorFound, storedFound bool, anchored, stored string) bool {
	return anchorFound && storedFound && anchored == stored
}

// unanchoredHistory reports whether this database was booted at least once
// without a usable fence. It is only ever asked in the one branch where both
// halves are empty, and only its presence matters — the value is a note for an
// operator reading the row, never an input to this decision.
func (fence *CalendarFeedRestoreFence) unanchoredHistory(ctx context.Context) (bool, error) {
	_, found, err := fence.appState.Get(ctx, models.AppStateKeyCalendarFeedFenceUnanchored)
	return found, err
}

// record mints the next token and stores it in both halves, then clears the
// unanchored stamp. A write failure on the file half is an anchor failure, not
// a boot failure, so it degrades into the unanchored outcome — which is also
// the path a first boot with no mount takes, since a missing directory reads as
// "no token" and only fails on write.
//
// The stamp is erased LAST, after both halves already hold the new token, and
// unconditionally rather than only when one was found: a crash between the two
// leaves a stamp beside halves that agree, which the next boot ignores (the
// stamp is read only where both halves are empty) and the next pass that
// records erases — while the reverse order would clear the evidence first and
// let a crash before the token write leave a database that ran unanchored with
// nothing left saying so. Fail closed on the way in, tidy up on the way out.
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
	if err := fence.appState.Delete(ctx, models.AppStateKeyCalendarFeedFenceUnanchored); err != nil {
		return outcome, err
	}
	return outcome, nil
}

// disarmUnanchored is the fail-closed path: without a usable fence nothing can
// prove this database still holds the revocations this instance performed, so
// every armed feed goes. alreadyDisarmed carries the count from a disarm that
// ran earlier in the same pass, so the reported total counts each row once.
//
// It also stamps the database, which is the only durable trace this boot can
// leave: the fence's own token needs a half outside the database and there is
// none here, so without the stamp a backup taken now is indistinguishable from
// one taken by an instance that never served at all, and the first boot after
// a fence is finally mounted would adopt its armed rows instead of disarming.
//
// A failure to write the stamp is RETURNED, not swallowed, which fails the
// boot. That is the same answer the pass already gives every other app_state
// failure, and the right one here: the anchor error this path exists for is an
// ordinary operator state the instance boots through, but a database that
// cannot record what just happened is not — an instance that served unanchored
// and left no evidence is precisely the state this stamp exists to prevent, and
// starting anyway would produce it silently. The disarm runs first, so the
// feeds are down either way and a failed boot never leaves them served.
func (fence *CalendarFeedRestoreFence) disarmUnanchored(ctx context.Context, cause error, alreadyDisarmed int64) (CalendarFeedRestoreFenceOutcome, error) {
	disarmed, err := fence.users.DisarmAllCalendarFeedTokens(ctx)
	outcome := CalendarFeedRestoreFenceOutcome{
		Unanchored:      true,
		UnanchoredCause: cause,
		DisarmedFeeds:   alreadyDisarmed + disarmed,
	}
	if err != nil {
		return outcome, err
	}
	return outcome, fence.appState.Set(ctx, models.AppStateKeyCalendarFeedFenceUnanchored, unanchoredStampValue(cause))
}

// unanchoredStampValue is what an operator finds in the row. Only the key's
// presence is ever read, so this text is free to be for a human: the cause is
// the fence failure itself (an unset variable, a missing mount), never a
// selector, a token or anything about an owner.
func unanchoredStampValue(cause error) string {
	return fmt.Sprintf("booted without a usable calendar-feed restore fence: %v", cause)
}
