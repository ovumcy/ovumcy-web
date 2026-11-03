package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

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

// calendarFeedRestoreFenceUserStore is the bulk writes the fence performs when
// it cannot prove the database is the one this instance last ran with: the
// feed disarm on every such boot, and the session and webhook revocation on a
// boot that sees a restore (ContinuityBroken).
type calendarFeedRestoreFenceUserStore interface {
	DisarmAllCalendarFeedTokens(ctx context.Context) (int64, error)
	RevokeRestoredAccess(ctx context.Context) (models.RestoreRevocationCounts, error)
}

// calendarFeedRestoreFenceAnchor is the half of the fence that lives outside
// the database. Read reports ("", false, nil) for "no token yet" and an error
// for every state in which continuity cannot be proved — including "no fence
// configured at all". Implementation: security.CalendarFeedFenceFile.
type calendarFeedRestoreFenceAnchor interface {
	Read() (string, bool, error)
	Write(value string) error
}

// calendarFeedFenceFirstBootPrefix marks a file token written by a first boot
// that has not yet stored its database half (WEB-456). The prefix is outside
// the token alphabet, so no completed token carries it. See record.
const calendarFeedFenceFirstBootPrefix = "first-boot-pending:"

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
	// outside the database to put its other half — and the database half of any
	// token still there is DROPPED, so a fence file that outlives this start
	// cannot compare equal on its return. The database is stamped as having
	// booted this way, which is what lets the first boot that finally has a
	// fence — with both halves empty — tell this history from a fresh
	// installation. All of that is Enforce's; from EnforceRequired the same
	// outcome means nothing was written at all, because the boot is refused.
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
	// Revoked counts what a ContinuityBroken pass revoked beside the feeds
	// (WEB-399): every account's sessions, and every stored webhook endpoint.
	// Both live in columns a restore rolls back — auth_session_version and the
	// webhook revocation epoch — so a restore would otherwise validate every
	// cookie minted before its backup point again and deliver to endpoints the
	// owners removed after it. Zero on every other outcome.
	Revoked models.RestoreRevocationCounts
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
	// confirmedAhead is set by a successful AdvanceConfirmed and consumed by
	// the next Advance, AdvanceOrRefuse or AdvanceOrDiverge, which then writes
	// nothing (WEB-429).
	// The operator write that follows the gate advances the fence again from
	// inside its repository; the confirmed advance already precedes that write,
	// and a second one could only add a failure — a file error there moves the
	// database half alone, and the next boot revokes every session, webhook and
	// local password, including the one the operator just set. Guarded by
	// writing.
	confirmedAhead bool
	// batching and pending coalesce concurrent AdvanceOrRefuse calls — every
	// sign-out runs one — into one advance per turn of writing, so a caller
	// waits for at most two file syncs however many arrive with it (WEB-454).
	batching sync.Mutex
	pending  *calendarFeedFenceAdvanceBatch
}

// calendarFeedFenceAdvanceBatch is one coalesced AdvanceOrRefuse: every caller
// that joined it before its leader started writing returns its result.
type calendarFeedFenceAdvanceBatch struct {
	done    chan struct{}
	err     error
	members int
}

// NewCalendarFeedRestoreFence wires the fence.
func NewCalendarFeedRestoreFence(appState calendarFeedRestoreFenceAppState, users calendarFeedRestoreFenceUserStore, anchor calendarFeedRestoreFenceAnchor) *CalendarFeedRestoreFence {
	return &CalendarFeedRestoreFence{appState: appState, users: users, anchor: anchor}
}

// Enforce performs the boot-time check for an instance that runs with
// CALENDAR_FEED_FENCE_OPTOUT; every other boot runs EnforceRequired, which
// refuses the anchor failures described below instead of booting through them.
//
// Two failure classes are deliberately kept apart. A DATABASE error is returned
// and fails the boot, exactly as the sibling sentinel's does: a database that
// cannot answer will not serve either. An ANCHOR error is not — an unmounted
// fence volume is an ordinary operator state, and refusing to start over it
// would take an instance down for a feature most instances do not use. It fails
// closed instead: disarm everything, record no token and drop the database half
// of any token still there — there is nowhere outside the database to keep its
// other half — and say so on every start until the mount is there. It does
// stamp the database as having booted that way, because that stamp is what the
// first boot WITH a fence needs in order to tell this history from a brand-new
// installation; a stamped database whose two halves are both empty is a restore
// that cannot be ruled out, not a first boot.
//
// Ordering: every disarm and revocation runs BEFORE the DATABASE half of the
// new token is recorded, so a crash in between re-runs the pass on the next
// boot instead of recording a fence whose revocation never happened. The file
// is written first — after the revocations on the unanchored and first-boot
// paths, before them on the ContinuityBroken path (revokeRestored says why) —
// and either way a crash after it leaves the halves disagreeing, which re-runs
// the pass, while the reverse would record agreement the file never reached.
func (fence *CalendarFeedRestoreFence) Enforce(ctx context.Context) (CalendarFeedRestoreFenceOutcome, error) {
	return fence.enforce(ctx, false)
}

// EnforceRequired is Enforce for a boot that refuses to start unanchored (no
// CALENDAR_FEED_FENCE_OPTOUT). Every anchor failure returns the Unanchored
// outcome with NOTHING written in either half: no disarm, no dropped token, no
// stamp (WEB-427). A refused boot serves nothing, so it has nothing to
// contain, while a stamp it left would make the first boot after the operator
// fixes the mount read the gap as a restore and revoke every session, webhook
// and local password on the instance. Every path that can reach the unanchored
// outcome writes the file half before anything else, so the failure is found
// before the first database write — except a first boot's final file write,
// which leaves the pending pair record describes for the next boot to finish.
func (fence *CalendarFeedRestoreFence) EnforceRequired(ctx context.Context) (CalendarFeedRestoreFenceOutcome, error) {
	return fence.enforce(ctx, true)
}

func (fence *CalendarFeedRestoreFence) enforce(ctx context.Context, refuseUnanchored bool) (CalendarFeedRestoreFenceOutcome, error) {
	// Held across the whole pass, so record's two writes cannot interleave with
	// an Advance. Nothing serves yet when this runs, so it never contends;
	// taking it keeps the invariant in one place instead of in a comment about
	// boot ordering that a later caller could invalidate.
	fence.writing.Lock()
	defer fence.writing.Unlock()

	anchored, anchorFound, pending, err := fence.readAnchor()
	if err != nil {
		return fence.disarmUnanchored(ctx, err, refuseUnanchored)
	}

	stored, storedFound, err := fence.appState.Get(ctx, models.AppStateKeyCalendarFeedRestoreFence)
	if err != nil {
		return CalendarFeedRestoreFenceOutcome{}, err
	}

	if pending != "" && storedFound && stored == pending {
		return fence.finishFirstBoot(ctx, pending, refuseUnanchored)
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
			return fence.record(ctx, refuseUnanchored)
		}
		return fence.revokeRestored(ctx, CalendarFeedRestoreFenceOutcome{ContinuityBroken: true, UnanchoredHistory: true}, refuseUnanchored)
	}
	// Equality is the only proof of continuity, and it needs both halves: a
	// database carrying no token at all is a database this fence never wrote,
	// which is what restoring a pre-fence backup looks like.
	if continuityHolds(anchorFound, storedFound, anchored, stored) {
		return CalendarFeedRestoreFenceOutcome{}, nil
	}
	return fence.revokeRestored(ctx, CalendarFeedRestoreFenceOutcome{ContinuityBroken: true}, refuseUnanchored)
}

// revokeRestored is every ContinuityBroken boot's answer: disarm every armed
// feed, then revoke every session and clear every webhook endpoint, and only
// then record the new token. Feeds are not the only revocation a restore rolls
// back (WEB-399): a sign-out, password change, operator reset or account
// deletion is recorded in auth_session_version or in the row itself, and a
// webhook removal in webhook_config_version, all inside the dump. Nothing in
// the restored rows says which of those happened after the backup point, so
// all of them are revoked, as all feeds are. Local passwords go too (WEB-408):
// a forced reset after the backup point is undone by the restore, and the
// compromised hash it replaced would sign in again.
//
// The new token reaches the FILE half before anything is revoked, and the
// database half only after. A crash anywhere in between leaves the halves
// disagreeing, so the next boot re-runs this pass, as before. What the order
// adds is that the revocation happens once: an anchor that reads but cannot be
// written degrades to the unanchored outcome here, before any revocation,
// instead of revoking and then failing to record — which, on a fence volume
// that is configured but missing, used to repeat the whole revocation on every
// start, and with passwords in it would lock every owner out on every start.
// That degraded boot is refused outright, writing nothing (EnforceRequired),
// unless CALENDAR_FEED_FENCE_OPTOUT accepts the residual; with the opt-out it
// disarms feeds as every unanchored boot does.
func (fence *CalendarFeedRestoreFence) revokeRestored(ctx context.Context, outcome CalendarFeedRestoreFenceOutcome, refuseUnanchored bool) (CalendarFeedRestoreFenceOutcome, error) {
	token, err := security.NewCalendarFeedFenceToken()
	if err != nil {
		return outcome, err // codecov:ignore -- crypto/rand failure; unreachable without an OS-level entropy fault
	}
	if err := fence.anchor.Write(token); err != nil {
		return fence.disarmUnanchored(ctx, err, refuseUnanchored)
	}
	disarmed, err := fence.users.DisarmAllCalendarFeedTokens(ctx)
	if err != nil {
		return outcome, err
	}
	outcome.DisarmedFeeds = disarmed
	revoked, err := fence.users.RevokeRestoredAccess(ctx)
	if err != nil {
		return outcome, err
	}
	outcome.Revoked = revoked
	return fence.storeToken(ctx, outcome, token)
}

// Advance records that the set of armed calendar feeds just changed, in both
// halves of the fence. It is what makes the boot comparison able to see a
// restore at all: a token minted once per boot agrees with any backup taken
// during that same boot, so restoring one — which is exactly the supported
// procedure, taken with the app stopped — would compare equal and disarm
// nothing. Advancing on the change itself is the same shape the webhook
// revocation epoch uses, for the same reason.
//
// It is for a write that IS the change and that the caller can refuse: an
// owner arming, rotating or revoking their own feed, saving or removing their
// webhook. The two reasons the FILE half can fail are answered differently:
//
//   - NOT CONFIGURED at all. Nothing is recorded, on either half, and nil is
//     returned. An instance with no fence already disarms every armed feed on
//     every boot (Enforce's unanchored path), so containment is complete
//     without this write.
//   - Configured but unwritable — a broken mount, a full disk, a transient I/O
//     error — over halves that agree. Nothing is recorded and
//     ErrCalendarFeedFenceWriteFailed is returned, so the caller refuses its
//     own write (WEB-414): the halves still agree and the row never moves, so
//     there is nothing a restore could undo. Over halves that already disagree
//     (or cannot be read) the next boot runs the full pass anyway, so the
//     database half moves alone, as AdvanceOrDiverge does, and the write
//     proceeds.
//     Moving the database half alone would fail closed too, but the next boot
//     cannot tell that disagreement from a restored backup and answers it with
//     the full ContinuityBroken revocation — every session, webhook and local
//     password on the instance — for a write the owner could simply retry.
//
// A database failure after the file half was written is returned as well, and
// is the one outcome that changes something: the halves disagree, so the next
// boot runs the ContinuityBroken pass. It is priced in rather than avoided: the
// halves are written one after the other with no transaction spanning both,
// and file-then-database is the order whose failure revokes too much instead
// of too little. The reverse would leave the halves agreeing on a token minted
// before a revocation that did happen, which is the defect this whole
// mechanism exists to close.
//
// A write whose feed clear is a side effect it must not be refused over — a
// password change, a sign-out — calls AdvanceOrRefuse instead, an erasure
// AdvanceOrDiverge, and an
// operator process acting on someone ELSE's account calls AdvanceConfirmed.
func (fence *CalendarFeedRestoreFence) Advance(ctx context.Context) error {
	fence.writing.Lock()
	defer fence.writing.Unlock()

	if fence.consumeConfirmedAdvance() {
		return nil
	}
	token, err := security.NewCalendarFeedFenceToken()
	if err != nil {
		return err // codecov:ignore -- crypto/rand failure; unreachable without an OS-level entropy fault
	}
	if err := fence.anchor.Write(token); err != nil {
		if errors.Is(err, security.ErrCalendarFeedFenceNotConfigured) {
			return nil
		}
		// Refuse only where there is continuity to keep. Halves that already
		// disagree — an instance that booted unanchored under the opt-out, its
		// database half dropped — already send the next boot down the full pass,
		// so refusing there would only block an owner revoking a leaked feed.
		if fence.confirmContinuity(ctx) == nil {
			return &CalendarFeedFenceStepError{Sentinel: ErrCalendarFeedFenceWriteFailed, Cause: err}
		}
	}
	return fence.appState.Set(ctx, models.AppStateKeyCalendarFeedRestoreFence, token)
}

// AdvanceOrRefuse is Advance for a write that revokes sessions or changes a
// credential or the login identity — a sign-out, a password change or reset, a
// TOTP toggle, an OIDC link or unlink, recovery-code rotation, reset-token
// redemption, the operator email re-home — whose revocation must survive a
// restore taken before it.
//
// Where Advance refuses on the first failed file write, this retries it a
// bounded number of times (calendarFeedFenceWriteRetryDelays), so a momentary
// ENOSPC or EIO does not cost an owner a refused sign-out. When every attempt
// fails it refuses exactly as Advance does (WEB-464): nothing written in either
// half, ErrCalendarFeedFenceWriteFailed returned, and the caller's row write
// must not run. Moving the database half alone is NOT safe here: a backup
// taken before this write carries the old token in app_state, the file still
// holds that same token, and restoring the backup before the next boot leaves a
// pair that agrees — the revoked cookie or replaced credential resolves again.
// Over halves that already disagree durably — a file holding another token, or
// a database half already dropped — the next boot runs the full pass whatever
// is written, so the database half moves alone and the write proceeds
// (durableDisagreement). A file that reads as absent, or a read that fails, is
// refused like a failed write: an unmounted fence volume reads as absent, and
// once mounted again its old token agrees with a restored pre-write backup. A
// database failure after the file moved is returned wrapped in
// ErrCalendarFeedFenceHalfAdvanced, so a caller can tell either refusal from
// an error of its own write.
//
// Concurrent calls are coalesced. A caller joins the batch that is still
// waiting for writing, or opens one; the batch's leader detaches it only once
// it holds writing, so every member called before the advance began, which is
// all the ordering its row write needs. A member waits for the result whatever
// its ctx does, so its row write never runs ahead of an advance it joined. The
// write runs under the leader's ctx, as every fence write runs under its
// caller's: a cancelled leader refuses the whole batch, or — once the file has
// moved — fails the database half for it, which leaves the file ahead the way
// Advance's database failure does. Every member refuses its row write either
// way. The lock and the batch are released on every exit, a panic included, or
// each later session writer would wait forever.
func (fence *CalendarFeedRestoreFence) AdvanceOrRefuse(ctx context.Context) error {
	fence.batching.Lock()
	if batch := fence.pending; batch != nil {
		batch.members++
		fence.batching.Unlock()
		<-batch.done
		return batch.err
	}
	batch := &calendarFeedFenceAdvanceBatch{done: make(chan struct{}), members: 1, err: errCalendarFeedFenceAdvanceAbandoned}
	fence.pending = batch
	fence.batching.Unlock()

	fence.writing.Lock()
	defer func() {
		fence.writing.Unlock()
		close(batch.done)
	}()
	fence.batching.Lock()
	fence.pending = nil
	fence.batching.Unlock()
	batch.err = fence.advanceRetryingLocked(ctx, false)
	return batch.err
}

// errCalendarFeedFenceAdvanceAbandoned is what the members of a batch whose
// leader panicked mid-write receive.
var errCalendarFeedFenceAdvanceAbandoned = errors.New("calendar feed restore fence: the shared advance was abandoned")

// AdvanceOrDiverge is AdvanceOrRefuse for a write that proceeds whatever the
// fence does: clear-data and account erasure. Refusing those over a fence
// volume would keep health data the owner asked to have gone, while the hole a
// divergence leaves — a pre-erasure backup restored before the next boot —
// brings that data back with the restore itself, which no fence prevents.
//
// It retries the file write as AdvanceOrRefuse does. When every attempt fails
// it moves the database half on ALONE: the halves disagree, and the next boot
// answers with the full ContinuityBroken pass, passwords included. Only these
// writes pay that, and only while the file stays unwritable. It is not
// coalesced: erasures are rare, and a batch shared with refusing callers would
// have to answer one persistent failure two opposite ways.
func (fence *CalendarFeedRestoreFence) AdvanceOrDiverge(ctx context.Context) error {
	fence.writing.Lock()
	defer fence.writing.Unlock()
	return fence.advanceRetryingLocked(ctx, true)
}

// calendarFeedFenceWriteRetryDelays are the waits between the file-write
// attempts of AdvanceOrRefuse and AdvanceOrDiverge: four attempts within about
// half a second, held under writing, which the batch spreads over every
// sign-out that arrives meanwhile.
var calendarFeedFenceWriteRetryDelays = []time.Duration{25 * time.Millisecond, 100 * time.Millisecond, 400 * time.Millisecond}

// advanceRetryingLocked is one AdvanceOrRefuse or AdvanceOrDiverge write. The
// caller holds fence.writing.
func (fence *CalendarFeedRestoreFence) advanceRetryingLocked(ctx context.Context, diverge bool) error {
	if fence.consumeConfirmedAdvance() {
		return nil
	}
	token, err := security.NewCalendarFeedFenceToken()
	if err != nil {
		return err // codecov:ignore -- crypto/rand failure; unreachable without an OS-level entropy fault
	}
	writeErr := fence.writeAnchorRetrying(ctx, token)
	if errors.Is(writeErr, security.ErrCalendarFeedFenceNotConfigured) {
		return nil
	}
	if writeErr != nil && !diverge && !fence.durableDisagreement(ctx, fence.confirmContinuity(ctx)) {
		return &CalendarFeedFenceStepError{Sentinel: ErrCalendarFeedFenceWriteFailed, Cause: writeErr}
	}
	if err := fence.appState.Set(ctx, models.AppStateKeyCalendarFeedRestoreFence, token); err != nil {
		if writeErr == nil {
			return &CalendarFeedFenceStepError{Sentinel: ErrCalendarFeedFenceHalfAdvanced, Cause: err}
		}
		return err
	}
	return nil
}

// durableDisagreement reports whether confirmContinuity's answer is a
// disagreement the next boot reads whatever a restore does in between: a file
// that holds a token other than the database's, or a database half already
// dropped (an unanchored opt-out boot, an unfinished first boot). Only that
// waives AdvanceOrRefuse's refusal. A file that reads as absent is NOT one — an
// unmounted fence volume reads exactly so, and once it is mounted again its old
// token agrees with a restored pre-write backup — and neither is a read that
// failed outright (WEB-464), unless the database half is already gone: an
// unanchored opt-out boot drops it, and with no token there is nothing a
// restore could make agree. The caller holds fence.writing.
func (fence *CalendarFeedRestoreFence) durableDisagreement(ctx context.Context, err error) bool {
	var continuity *CalendarFeedFenceContinuityError
	if errors.As(err, &continuity) {
		return continuity.AnchorFound || !continuity.StoredFound
	}
	if errors.Is(err, ErrCalendarFeedFenceUnreachable) {
		_, storedFound, getErr := fence.appState.Get(ctx, models.AppStateKeyCalendarFeedRestoreFence)
		return getErr == nil && !storedFound
	}
	return false
}

// writeAnchorRetrying writes token to the file half, retrying a failed write
// after each of calendarFeedFenceWriteRetryDelays. An unconfigured fence is not
// retried, and a done ctx ends the retries with the last write error.
func (fence *CalendarFeedRestoreFence) writeAnchorRetrying(ctx context.Context, token string) error {
	err := fence.anchor.Write(token)
	for _, delay := range calendarFeedFenceWriteRetryDelays {
		if err == nil || errors.Is(err, security.ErrCalendarFeedFenceNotConfigured) {
			return err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return err
		case <-timer.C:
		}
		err = fence.anchor.Write(token)
	}
	return err
}

// ErrCalendarFeedFenceWriteFailed is returned by Advance when the fence is
// configured and its file could not be written. Nothing was written in either
// half, so the caller's own write must not run, and a retry is safe. It always
// wraps the anchor error.
var ErrCalendarFeedFenceWriteFailed = errors.New("calendar feed restore fence: could not write the fence file")

// ErrCalendarFeedFenceUnreachable is returned by AdvanceConfirmed when this
// process cannot prove it is reading and writing the SAME anchor the server
// does: CALENDAR_FEED_FENCE_PATH unset, no mount behind it, or any other
// anchor failure, including one surfaced by the read half. It always wraps
// the underlying cause.
//
// Advance, AdvanceOrRefuse and AdvanceOrDiverge treat an UNSET path as "no fence" and proceed,
// because an instance with no fence disarms every feed on every boot anyway.
// AdvanceConfirmed exists for the opposite caller — an operator process whose
// shell may simply not be looking at the server's fence — where proceeding
// would record the removal nowhere a restore of an older backup could
// contradict. So on this path AdvanceConfirmed writes NOTHING, in either half,
// and the caller refuses the operation entirely instead.
var ErrCalendarFeedFenceUnreachable = errors.New("calendar feed restore fence: could not confirm continuity")

// ErrCalendarFeedFenceMarkerUnavailable is returned by AdvanceConfirmed when
// the DATABASE half could not be read. It is kept apart from
// ErrCalendarFeedFenceUnreachable because the operator remedy differs — this
// is the database not answering, not a path that is unset or unmounted — and
// because, like that error, it guarantees nothing was written in either half.
// It always wraps the storage error.
var ErrCalendarFeedFenceMarkerUnavailable = errors.New("calendar feed restore fence: could not read the database marker")

// ErrCalendarFeedFenceHalfAdvanced is returned by AdvanceConfirmed — and by
// AdvanceOrRefuse and AdvanceOrDiverge — when the
// file half was written and the database half then could not be. It is the
// one error out of that method after which something HAS changed: the file is
// ahead of app_state, so the server's next boot reads that as a restore and
// disarms every armed feed — fail closed, the same answer AdvanceOrDiverge's
// unwritable-anchor path gives, reached through a different door. A caller
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
// operator remedy differs by shape: both false means the server has not booted
// with this fence since it last held a token — never at all, an unfenced start
// dropped the database half and the file went with the volume, or a first boot
// left its pending pair unfinished (WEB-456) — and
// arming it is Enforce's job on that next boot, never this method's — while any
// other combination is the same disagreement a restored backup produces. Either way AdvanceConfirmed writes
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
// reset) or saving their webhook (`webhook set`). Both methods answer the same question — "record that the set of
// armed feeds just changed" — but must answer an unreachable or disagreeing
// fence oppositely, for the reason ErrCalendarFeedFenceUnreachable's doc
// comment gives.
//
// It refuses unless the anchor can be read AND the two halves already hold
// the SAME token — the one condition Enforce calls a no-op — and only then
// mints, writes the file, then the database marker, in that order, same as
// Advance. A failure writing the database half after the file has already
// moved is returned wrapped in ErrCalendarFeedFenceHalfAdvanced: the
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

	if err := fence.confirmContinuity(ctx); err != nil {
		return err
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
	fence.confirmedAhead = true
	return nil
}

// consumeConfirmedAdvance reports whether a confirmed advance already covers
// the write about to happen, and clears the mark. The caller holds
// fence.writing.
func (fence *CalendarFeedRestoreFence) consumeConfirmedAdvance() bool {
	covered := fence.confirmedAhead
	fence.confirmedAhead = false
	return covered
}

// CheckContinuity is AdvanceConfirmed's refusal half alone: the same errors,
// for the same states, and nothing written in either half on any outcome. It
// is for a process that must not act on a database the server has not yet
// booted over since a restore — `ovumcy notify`, whose delivery would reach a
// webhook endpoint that only the server's ContinuityBroken pass clears
// (WEB-408).
func (fence *CalendarFeedRestoreFence) CheckContinuity(ctx context.Context) error {
	fence.writing.Lock()
	defer fence.writing.Unlock()
	return fence.confirmContinuity(ctx)
}

// confirmContinuity is the read both CheckContinuity and AdvanceConfirmed
// refuse on. The caller holds fence.writing.
func (fence *CalendarFeedRestoreFence) confirmContinuity(ctx context.Context) error {
	anchored, anchorFound, pending, err := fence.readAnchor()
	if err != nil {
		return &CalendarFeedFenceStepError{Sentinel: ErrCalendarFeedFenceUnreachable, Cause: err}
	}

	stored, storedFound, err := fence.appState.Get(ctx, models.AppStateKeyCalendarFeedRestoreFence)
	if err != nil {
		return &CalendarFeedFenceStepError{Sentinel: ErrCalendarFeedFenceMarkerUnavailable, Cause: err}
	}

	if pending != "" && storedFound && stored == pending {
		// An unfinished first boot is the "not armed yet" shape: the server has
		// not completed a boot with this fence, and its next start finishes it.
		return &CalendarFeedFenceContinuityError{}
	}
	if !continuityHolds(anchorFound, storedFound, anchored, stored) {
		return &CalendarFeedFenceContinuityError{AnchorFound: anchorFound, StoredFound: storedFound}
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

// readAnchor is anchor.Read with a first boot's pending token taken apart: a
// pending file token reads as no file token at all (found false), and the
// token it stands for is returned separately for the one comparison that may
// finish that first boot.
func (fence *CalendarFeedRestoreFence) readAnchor() (anchored string, found bool, pending string, err error) {
	anchored, found, err = fence.anchor.Read()
	if err != nil || !found {
		return anchored, found, "", err
	}
	if token, isPending := strings.CutPrefix(anchored, calendarFeedFenceFirstBootPrefix); isPending {
		return "", false, token, nil
	}
	return anchored, true, "", nil
}

// record is the first boot: it mints the next token and stores it in both
// halves, then clears the unanchored stamp. A write failure on the file half is
// an anchor failure, not a boot failure, so it degrades into the unanchored
// outcome — which is also the path a first boot with no mount takes, since a
// missing directory reads as "no token" and only fails on write.
//
// The file half is written twice: first as a PENDING token, then — after the
// database half holds the token — as the token itself (WEB-456). A lone file
// token beside an empty database is what restoring a pre-fence backup looks
// like, so a first boot that wrote the plain token and then failed its database
// write left the next boot revoking every session, webhook and local password
// on an instance that never served. A pending token reads as "no file token
// yet" instead: beside an empty, unstamped database the next boot is a first
// boot again; beside the very token it carries, the next boot only finishes
// the file. Neither state can follow a boot that served, because no boot serves
// until the plain token is in the file, so a pending token vouches for nothing
// a restore could have rolled back. Everywhere else it is no token: beside a
// different database token or the unanchored stamp the full pass still runs.
//
// The stamp is erased LAST, after both halves already hold the new token, and
// unconditionally rather than only when one was found: a crash between the two
// leaves a stamp beside halves that agree, which the next boot ignores (the
// stamp is read only where both halves are empty) and the next pass that
// records erases — while the reverse order would clear the evidence first and
// let a crash before the token write leave a database that ran unanchored with
// nothing left saying so. Fail closed on the way in, tidy up on the way out.
func (fence *CalendarFeedRestoreFence) record(ctx context.Context, refuseUnanchored bool) (CalendarFeedRestoreFenceOutcome, error) {
	outcome := CalendarFeedRestoreFenceOutcome{FirstBoot: true}
	token, err := security.NewCalendarFeedFenceToken()
	if err != nil {
		return outcome, err // codecov:ignore -- crypto/rand failure; unreachable without an OS-level entropy fault
	}
	if err := fence.anchor.Write(calendarFeedFenceFirstBootPrefix + token); err != nil {
		return fence.disarmUnanchored(ctx, err, refuseUnanchored)
	}
	if err := fence.appState.Set(ctx, models.AppStateKeyCalendarFeedRestoreFence, token); err != nil {
		return outcome, err
	}
	return fence.finishFirstBoot(ctx, token, refuseUnanchored)
}

// finishFirstBoot replaces a first boot's pending file token with the token the
// database half already holds, then clears the unanchored stamp. A failed file
// write here is an anchor failure like any other: refused under
// EnforceRequired, with the pending pair left for the next boot to finish.
// Under CALENDAR_FEED_FENCE_OPTOUT it disarms, drops the database half and
// stamps, so the next fenced boot takes the UnanchoredHistory pass — the same
// declared residual as any other transient anchor failure under the opt-out.
func (fence *CalendarFeedRestoreFence) finishFirstBoot(ctx context.Context, token string, refuseUnanchored bool) (CalendarFeedRestoreFenceOutcome, error) {
	if err := fence.anchor.Write(token); err != nil {
		return fence.disarmUnanchored(ctx, err, refuseUnanchored)
	}
	outcome := CalendarFeedRestoreFenceOutcome{FirstBoot: true}
	if err := fence.appState.Delete(ctx, models.AppStateKeyCalendarFeedFenceUnanchored); err != nil {
		return outcome, err
	}
	return outcome, nil
}

// storeToken is revokeRestored's database half: the token the file already
// holds, then the unanchored stamp's erasure, in that order. The first boot
// does not use it; record says why.
func (fence *CalendarFeedRestoreFence) storeToken(ctx context.Context, outcome CalendarFeedRestoreFenceOutcome, token string) (CalendarFeedRestoreFenceOutcome, error) {
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
// every armed feed goes. Every path reaches it before any disarm of its own has
// run, so the count it reports is its own.
//
// It also stamps the database, which is the only durable trace this boot can
// leave: the fence's own token needs a half outside the database and there is
// none here, so without the stamp a backup taken now is indistinguishable from
// one taken by an instance that never served at all, and the first boot after
// a fence is finally mounted would adopt its armed rows instead of disarming.
//
// The database half of the token is dropped as well. A fence file that outlives
// this start — the variable dropped and later restored over the same volume —
// would otherwise still agree with the database: nothing during a period with
// no fence configured can advance either half (Advance records nothing when not
// configured), so a backup taken then, restored beside the untouched file,
// compares equal and the stamp is never consulted. A fence that is configured
// but unusable is the other flavour of unanchored: there Advance refuses every
// owner feed change while AdvanceOrRefuse and AdvanceOrDiverge, over halves
// that already disagree, move the database half alone, so the
// gap either changed nothing or left a disagreement the next fenced boot reads. The drop still runs for that flavour, on purpose — the two are
// told apart only by the error, and a drop keyed on it would leave the file
// agreeing with a database half nothing wrote across a gap the pass just
// declared it could not vouch for. With the database half gone, the file's return
// reads as a disagreement (anchor found, database empty → ContinuityBroken) and
// the feeds are disarmed once more before re-arming; with the file gone too,
// both halves are empty and the stamp answers. Delete is idempotent — a missing
// key is not an error — so every unanchored start after the first pays one
// no-op.
//
// The order is disarm → drop the token → stamp, and each reversal is worse.
// Token before stamp: a crash between the two leaves the database with no token
// and no stamp, which the next fenced boot reads as a disagreement (fail
// closed), while the reverse leaves a stamp beside a token that still matches
// the file — exactly the state this drop exists to prevent, and the stamp is not
// read there. Disarm before either write: a boot that fails at the disarm writes
// neither, so where nothing had been disarmed yet (the anchor-read failure, a
// first boot whose file write failed) the database is exactly as the boot found
// it and the fence's next start with the file back reads continuity — a failed
// boot served nothing and lost nothing — instead of a disagreement that would
// cost every owner a subscribe URL for a restore that never happened.
//
// A failure to write the stamp is RETURNED, not swallowed, which fails the
// boot. That is the same answer the pass already gives every other app_state
// failure, and the right one here: the anchor error this path exists for is an
// ordinary operator state the instance boots through, but a database that
// cannot record what just happened is not — an instance that served unanchored
// and left no evidence is precisely the state this stamp exists to prevent, and
// starting anyway would produce it silently. The disarm runs first, so the
// feeds are down either way and a failed boot never leaves them served.
//
// With refuseUnanchored the boot is about to be refused and nothing is written
// at all; EnforceRequired says why.
func (fence *CalendarFeedRestoreFence) disarmUnanchored(ctx context.Context, cause error, refuseUnanchored bool) (CalendarFeedRestoreFenceOutcome, error) {
	if refuseUnanchored {
		return CalendarFeedRestoreFenceOutcome{Unanchored: true, UnanchoredCause: cause}, nil
	}
	disarmed, err := fence.users.DisarmAllCalendarFeedTokens(ctx)
	outcome := CalendarFeedRestoreFenceOutcome{
		Unanchored:      true,
		UnanchoredCause: cause,
		DisarmedFeeds:   disarmed,
	}
	if err != nil {
		return outcome, err
	}
	if err := fence.appState.Delete(ctx, models.AppStateKeyCalendarFeedRestoreFence); err != nil {
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
