package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ovumcy/ovumcy-web/internal/bootstrap"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

// calendarFeedFencePath reads CALENDAR_FEED_FENCE_PATH from the environment.
// buildRepositories is its only caller: every subcommand that needs the path
// takes it from buildRepositories's own return value instead of calling this
// again, so the value a subcommand confirms against is provably the value its
// repositories were built with, and not a second, independently-read copy of
// the same variable.
func calendarFeedFencePath() string {
	return strings.TrimSpace(os.Getenv(security.CalendarFeedFencePathEnv))
}

// buildRepositories is every CLI subcommand's repository set, and the SAME
// fence bootstrap.BuildRepositories attached to it.
//
// It exists so no subcommand reaches db.NewRepositories directly: two of them
// remove calendar-feed access (`reset-password`'s forced clear, `users
// delete`, which takes the row and its feed with it), and a removal recorded
// only inside the database is undone by restoring a backup taken before it.
// Routing all four subcommands through one constructor keeps that from
// depending on which one an operator happened to run.
//
// It also returns the fence path it resolved, so a caller that goes on to
// call confirmOperatorFeedRevocation never has to call calendarFeedFencePath
// itself: two independent reads of the same environment variable is exactly
// the sort of gap that leaves a subcommand confirming against a value that
// silently differs from the one its own repositories were built with.
//
// The fence this returns shares the server's app_state but very often not its
// anchor — an operator's shell rarely has CALENDAR_FEED_FENCE_PATH set to the
// server's own path. confirmOperatorFeedRevocation is what the two
// feed-affecting subcommands call to find out, immediately before the write
// that would remove access: it confirms and advances the SAME fence rather
// than merely warning about it, and refuses the operation outright when it
// cannot.
func buildRepositories(database *gorm.DB) (*db.Repositories, *services.CalendarFeedRestoreFence, string) {
	fencePath := calendarFeedFencePath()
	repositories, fence := bootstrap.BuildRepositories(database, fencePath)
	return repositories, fence, fencePath
}

// confirmOperatorFeedRevocation is the gate every subcommand that revokes an
// owner's calendar feed (`users delete`, a forced `reset-password`) must pass
// immediately before the write that performs the revocation, and only once
// that write is otherwise certain to happen (confirmed, or --yes).
//
// It refuses outright rather than degrading, unlike the server's own
// Advance: an operator-driven removal recorded only inside the database is
// exactly what a restore of an older backup cannot contradict, which is the
// defect the whole fence exists to close. A refusal changes nothing — the
// caller must not proceed to its own write — and every refusal but one ends
// "Nothing was changed": the exception is the half-advanced fence, whose own
// message says plainly that the file side already moved.
func confirmOperatorFeedRevocation(ctx context.Context, fencePath string, fence *services.CalendarFeedRestoreFence, errOutput io.Writer) error {
	fencePath = strings.TrimSpace(fencePath)
	switch {
	case fencePath == "":
		return calendarFeedFenceConfirmRefusal(fencePath, calendarFeedFenceStateNotSet, nil)
	case !rootedPath(fencePath):
		return calendarFeedFenceConfirmRefusal(fencePath, calendarFeedFenceStateRelative, nil)
	case fence == nil:
		// Every production caller gets its fence from buildRepositories, which
		// never hands back nil. Reaching this is a wiring mistake in the
		// caller, not an operator state, so it gets its own plain error rather
		// than the operator-facing refusal template — and, more importantly,
		// never reaches fence.AdvanceConfirmed, whose receiver would panic on
		// a nil fence the moment it touched the mutex it embeds.
		return errors.New("calendar-feed restore fence: no fence was wired for this command (internal error)")
	}

	if err := fence.AdvanceConfirmed(ctx); err != nil {
		var continuity *services.CalendarFeedFenceContinuityError
		switch {
		case errors.As(err, &continuity):
			return calendarFeedFenceConfirmRefusal(fencePath, calendarFeedFenceStateForContinuity(continuity), nil)
		case errors.Is(err, services.ErrCalendarFeedFenceUnreachable):
			return calendarFeedFenceConfirmRefusal(fencePath, calendarFeedFenceStateUnreachable, wrappedCause(err))
		case errors.Is(err, services.ErrCalendarFeedFenceMarkerUnavailable):
			return calendarFeedFenceConfirmRefusal(fencePath, calendarFeedFenceStateMarkerUnavailable, wrappedCause(err))
		case errors.Is(err, services.ErrCalendarFeedFenceHalfAdvanced):
			// The one refusal after which something HAS changed: the fence file
			// moved and app_state did not, so the server's next start disarms
			// every armed feed (fail closed). The caller's own write never
			// ran, and the message must say both rather than the "Nothing was
			// changed" every other refusal ends with. The remedy is a server
			// boot, never a bare retry: re-running this same command reads the
			// file this run just wrote against the SAME stale database marker
			// and only reports the disagreement a second time.
			return fmt.Errorf("%w. The fence file at %s is now ahead of the database marker, so the server's next start disarms every armed calendar feed. "+
				"Start the server once — its boot pass reconciles both halves — then re-run this command. The account was not changed", err, fencePath)
		default:
			// AdvanceConfirmed's contract: every error raised after the file
			// write wraps ErrCalendarFeedFenceHalfAdvanced, so whatever reaches
			// here — in practice only a token that could not be minted — left
			// both halves untouched.
			return fmt.Errorf("%w. Nothing was changed", err) // codecov:ignore -- reached only through the token-mint failure AdvanceConfirmed itself marks unreachable
		}
	}

	_, _ = fmt.Fprintf(errOutput, "calendar-feed restore fence: fence advanced at %s; the removal that follows is recorded outside the database\n", fencePath)
	return nil
}

// wrappedCause extracts the SECOND error out of a "%w: %w" sentinel wrap —
// the underlying failure a sentinel like ErrCalendarFeedFenceUnreachable
// wraps beside itself — without depending on the sentinel's own message text
// staying stable. fmt.Errorf's multi-%w return implements Unwrap() []error,
// never the single-error Unwrap() error form errors.Unwrap looks for, so a
// plain errors.Unwrap call on it is always nil. Returns nil when err is not
// shaped that way, which callers treat as "no extra detail to show".
func wrappedCause(err error) error {
	multi, ok := err.(interface{ Unwrap() []error })
	if !ok {
		return nil
	}
	wrapped := multi.Unwrap()
	if len(wrapped) != 2 {
		return nil
	}
	return wrapped[1]
}

// calendarFeedFenceConfirmState names the shape confirmOperatorFeedRevocation
// refused in, because the operator remedy differs by shape: a path problem —
// unset, relative, or nothing mounted behind it — is fixed by running the CLI
// somewhere the server's fence is visible; a database marker that cannot be
// read is fixed by making the database answer; while a fence that has never
// recorded a token anywhere, that has recorded one only in the database, or
// that records two different generations, needs the SERVER to reconcile it —
// no CLI process can safely guess which half is right. Every shape here is
// refused with nothing written in either half.
type calendarFeedFenceConfirmState int

const (
	calendarFeedFenceStateNotSet calendarFeedFenceConfirmState = iota
	calendarFeedFenceStateRelative
	calendarFeedFenceStateUnreachable
	calendarFeedFenceStateMarkerUnavailable
	// calendarFeedFenceStateAnchorMissing is the database holding a marker
	// with no fence file visible at all — never fs.ErrNotExist, which
	// CalendarFeedFenceFile.Read already reports as "not found" rather than
	// as an error, but a directory this process cannot see at all, or one
	// that genuinely no longer holds the file the database remembers. Naming
	// it apart from calendarFeedFenceStateDisagree matters because the
	// remedies differ: a disagreement means the file IS visible and just
	// holds a different token, so restarting the server, which will read and
	// reconcile it, is enough; here the server itself may not be able to see
	// the file either, and the remedy has to say so.
	calendarFeedFenceStateAnchorMissing
	calendarFeedFenceStateDisagree
	calendarFeedFenceStateNeverArmed
)

// calendarFeedFenceStateForContinuity classifies a
// *services.CalendarFeedFenceContinuityError by which half(s) it found,
// mirroring the doc comment on that type: both absent is "never armed", the
// anchor alone absent while the database carries a marker gets its own state
// (see calendarFeedFenceStateAnchorMissing), and every other combination —
// the anchor alone present, or both present but different — is treated alike
// as a disagreement the server itself has to reconcile.
func calendarFeedFenceStateForContinuity(continuity *services.CalendarFeedFenceContinuityError) calendarFeedFenceConfirmState {
	switch {
	case !continuity.AnchorFound && !continuity.StoredFound:
		return calendarFeedFenceStateNeverArmed
	case !continuity.AnchorFound && continuity.StoredFound:
		return calendarFeedFenceStateAnchorMissing
	default:
		return calendarFeedFenceStateDisagree
	}
}

// calendarFeedFenceConfirmRefusal composes the operator-facing refusal:
// CALENDAR_FEED_FENCE_PATH, the state that stopped the write, what a
// completed-but-unrecorded removal would cost, and a remedy — always ending
// "Nothing was changed" so an operator scanning the tail of the message never
// has to wonder whether the command it names already ran.
//
// The switch below carries an explicit case for every defined state and a
// default that panics rather than falls back to one of them: a state added
// later without its own case would otherwise silently borrow whichever
// sentence the switch happened to fall through to, and hand the operator a
// remedy for a refusal that never happened. calendarFeedFenceConfirmRefusal
// itself never runs across an operator's own mistake, so the panic is a
// programming error caught in tests, not something a real shell can trigger.
func calendarFeedFenceConfirmRefusal(fencePath string, state calendarFeedFenceConfirmState, cause error) error {
	const consequence = "A removal recorded only inside the database is undone by restoring a backup taken before it."
	const runElsewhere = "Run the operator CLI where the server's fence is visible " +
		"(`docker exec ovumcy /app/ovumcy ...`; `docker compose exec ovumcy /app/ovumcy ...` if the service is already running, " +
		"`docker compose run --rm ovumcy /app/ovumcy ...` if it is not), or set " + security.CalendarFeedFencePathEnv +
		" to the same path the server uses — the server's own file, never a copy of it."
	// A relative CALENDAR_FEED_FENCE_PATH is unsafe no matter which shell
	// reads it, including one that copied the exact string the server itself
	// was given: it still resolves against a working directory, and telling
	// the operator to "set it to the same path the server uses" is either
	// what they already did or leads them to copy a value that will keep
	// resolving wrong in whichever shell reads it next. The server is the
	// side that has to change.
	const reconfigureTheServer = "A relative path cannot safely name the server's fence from any shell, including one already set to the exact string the " +
		"server was given — it resolves against a working directory, and this process's is not the server's. Reconfigure the SERVER's own " +
		security.CalendarFeedFencePathEnv + " to an absolute path, restart it once, then point this command at that same absolute path and re-run."
	const startTheServer = "Start the server once with this fence configured, then re-run this command."
	// Distinct from startTheServer: a fence that has NEVER recorded a marker
	// anywhere may mean the server has never had a writable fence at all —
	// the compose image sets CALENDAR_FEED_FENCE_PATH unconditionally, so an
	// operator who skipped mounting the volume sees exactly this shape rather
	// than an obvious "no variable set" refusal — and "start the server" on
	// its own leaves that operator restarting into the same refusal forever.
	const giveTheServerAWritableFence = "Give the server a writable fence — mount the `ovumcy_fence` volume as the bundled compose stacks do, or point " +
		security.CalendarFeedFencePathEnv + " at a writable directory — start it once, then re-run this command."
	const makeTheDatabaseAnswer = "Re-run this command once the database answers."
	// Named apart from runElsewhere/startTheServer because neither on its own
	// is honest here: the database DOES carry a marker, so a bare restart
	// risks nothing extra, but it will not help if this process simply
	// cannot see a file the server can — and if the server itself has never
	// been able to write one, restarting changes nothing at all.
	const anchorMissingRemedy = "Either this process cannot see the server's own fence file — run the operator CLI where that file is visible " +
		"(`docker exec ovumcy /app/ovumcy ...`; `docker compose exec ovumcy /app/ovumcy ...` if the service is already running, " +
		"`docker compose run --rm ovumcy /app/ovumcy ...` if it is not), or set " + security.CalendarFeedFencePathEnv +
		" to the same path the server uses — or the file has genuinely been lost, in which case starting the server once rewrites both halves and this " +
		"command can then be re-run. If the server itself has never been able to write a fence at all (no volume mounted), give it a writable one first."

	var stateSentence, remedy string
	switch state {
	case calendarFeedFenceStateNotSet:
		stateSentence = security.CalendarFeedFencePathEnv + " is not set in this shell."
		remedy = runElsewhere
	case calendarFeedFenceStateRelative:
		stateSentence = security.CalendarFeedFencePathEnv + " is " + fencePath + ", a relative path that resolves against this process's own working directory, not the server's."
		remedy = reconfigureTheServer
	case calendarFeedFenceStateUnreachable:
		stateSentence = security.CalendarFeedFencePathEnv + " points at " + fencePath + ", whose directory does not exist or cannot be written from this process"
		if cause != nil {
			stateSentence += ": " + cause.Error()
		}
		stateSentence += "."
		remedy = runElsewhere
	case calendarFeedFenceStateMarkerUnavailable:
		stateSentence = "the database marker paired with " + security.CalendarFeedFencePathEnv + " at " + fencePath + " could not be read"
		if cause != nil {
			stateSentence += ": " + cause.Error()
		}
		stateSentence += "."
		remedy = makeTheDatabaseAnswer
	case calendarFeedFenceStateAnchorMissing:
		stateSentence = "the database carries a fence marker, but " + security.CalendarFeedFencePathEnv + " at " + fencePath + " is not visible from this process."
		remedy = anchorMissingRemedy
	case calendarFeedFenceStateDisagree:
		stateSentence = security.CalendarFeedFencePathEnv + " at " + fencePath + " and the database marker do not agree."
		remedy = startTheServer
	case calendarFeedFenceStateNeverArmed:
		stateSentence = "neither " + security.CalendarFeedFencePathEnv + " at " + fencePath + " nor the database has ever recorded a marker."
		remedy = giveTheServerAWritableFence
	default:
		panic(fmt.Sprintf("calendar-feed restore fence: confirmOperatorFeedRevocation state %d has no refusal text", state))
	}

	// No trailing period (staticcheck ST1005): Go error strings end without
	// punctuation, even a deliberately long, multi-sentence operator one.
	return fmt.Errorf("calendar-feed restore fence: %s %s %s Nothing was changed", stateSentence, consequence, remedy)
}

// rootedPath reports whether the path names a location independent of the
// working directory. filepath.IsAbs is not enough on its own: this CLI is
// developed on Windows, where it demands a drive letter and therefore calls
// `/app/fence/calendar-feed.fence` relative — which is precisely the value an
// operator copies out of a compose file, and precisely the case the check
// exists to catch. A leading separator settles it on either platform.
func rootedPath(path string) bool {
	return filepath.IsAbs(path) || strings.HasPrefix(path, "/") || strings.HasPrefix(path, `\`)
}
