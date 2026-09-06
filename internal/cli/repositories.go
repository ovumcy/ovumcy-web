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
// Each subcommand calls it exactly once, at its own entry point, and threads
// the resolved value on to buildRepositories and — for the two subcommands
// that gate on it — to confirmOperatorFeedRevocation. Two independent reads of
// the same variable inside one command is exactly the sort of gap that leaves
// it confirming against a value that silently differs from the one its own
// repositories were built with.
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
// The fence path is a parameter rather than something this function resolves
// for itself, so the value a caller confirms against is provably the value its
// own repositories were built with — and so a test can drive a whole command
// without setting a process-wide environment variable.
//
// The fence this returns shares the server's app_state but very often not its
// anchor — an operator's shell rarely has CALENDAR_FEED_FENCE_PATH set to the
// server's own path. confirmOperatorFeedRevocation is what the two
// feed-affecting subcommands call to find out, immediately before the write
// that would remove access: it confirms and advances the SAME fence rather
// than merely warning about it, and refuses the operation outright when it
// cannot.
func buildRepositories(database *gorm.DB, fencePath string) (*db.Repositories, *services.CalendarFeedRestoreFence) {
	return bootstrap.BuildRepositories(database, fencePath)
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
//
// fencePath is used exactly as received, with no trim of its own: trimming
// happens once, in calendarFeedFencePath, so the value this switch judges and
// the value buildRepositories already built the fence's anchor from are
// provably the same string, not two copies a future third read could let
// drift apart.
func confirmOperatorFeedRevocation(ctx context.Context, fencePath string, fence *services.CalendarFeedRestoreFence, errOutput io.Writer) error {
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
		var step *services.CalendarFeedFenceStepError
		var cause error
		if errors.As(err, &step) {
			cause = step.Cause
		}
		switch {
		case errors.As(err, &continuity):
			return calendarFeedFenceConfirmRefusal(fencePath, calendarFeedFenceStateForContinuity(continuity), nil)
		case errors.Is(err, services.ErrCalendarFeedFenceUnreachable):
			return calendarFeedFenceConfirmRefusal(fencePath, calendarFeedFenceStateUnreachable, cause)
		case errors.Is(err, services.ErrCalendarFeedFenceMarkerUnavailable):
			return calendarFeedFenceConfirmRefusal(fencePath, calendarFeedFenceStateMarkerUnavailable, cause)
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

// operatorFeedRevocationCommitted is what a subcommand wraps its own write
// failure in once confirmOperatorFeedRevocation has already returned nil. The
// fence advance is one-shot and it has already happened, so a bare write error
// reads as "the command did nothing" — false about the fence, true about the
// account, and the operator needs both stated.
//
// Both halves of the fence hold the same fresh token, so nothing is left
// inconsistent and the server's next start disarms nothing on account of it.
// Re-running the command is safe, and is the whole remedy.
func operatorFeedRevocationCommitted(err error) error {
	return fmt.Errorf("%w. The calendar-feed restore fence was already advanced for this command, in both halves, so no feed is disarmed by it "+
		"and the server's next start has nothing to reconcile — but the account itself was not changed. Re-run the command", err)
}

// calendarFeedFenceConfirmState names the shape confirmOperatorFeedRevocation
// refused in, because the operator remedy differs by shape: a path problem —
// unset, relative, or one this process cannot read or write — is fixed by
// running the CLI somewhere the server's fence is visible; a database marker
// that cannot be read is fixed by making the database answer; while a fence
// that has never recorded a token anywhere, that has recorded one in only one
// half, or that records two different generations, needs the SERVER to
// reconcile it — no CLI process can safely guess which half is right. Every
// shape here is refused with nothing written in either half.
type calendarFeedFenceConfirmState int

const (
	// calendarFeedFenceStateInvalid is the zero value, and deliberately has no
	// refusal text: a state variable that was never assigned must reach the
	// panic in calendarFeedFenceConfirmRefusal rather than render whichever
	// sentence happens to sit first in the table.
	calendarFeedFenceStateInvalid calendarFeedFenceConfirmState = iota
	calendarFeedFenceStateNotSet
	calendarFeedFenceStateRelative
	calendarFeedFenceStateUnreachable
	calendarFeedFenceStateMarkerUnavailable
	// calendarFeedFenceStateAnchorMissing is the database holding a marker
	// while no fence VALUE is visible from this process. Two different states
	// of the world read that way, and neither is an error
	// CalendarFeedFenceFile.Read returns: a path whose directory this process
	// cannot see at all, which is what an unmounted volume looks like, and a
	// file that exists but is empty or blank — a torn write, which Read
	// deliberately reports as absent so the caller never compares against
	// nothing. Naming this apart from calendarFeedFenceStateDisagree matters
	// because the remedies differ: a disagreement means the file IS visible
	// and just holds a different token, so restarting the server, which will
	// read and reconcile it, is enough; here the server itself may see the
	// same nothing, and the remedy has to say so.
	calendarFeedFenceStateAnchorMissing
	// calendarFeedFenceStateMarkerMissing is the mirror shape: the file holds
	// a token and the database has none. A restart reconciles it, so it shares
	// the disagreement's remedy — but not its sentence, which would send the
	// operator looking for two differing values, one of which does not exist.
	calendarFeedFenceStateMarkerMissing
	calendarFeedFenceStateDisagree
	calendarFeedFenceStateNeverArmed
	// calendarFeedFenceStateCount bounds the enum. It is what lets a test walk
	// every declared state against the table below without re-listing them: a
	// hand-written mirror of the table agrees with it by construction and
	// would never notice a state added without an entry.
	calendarFeedFenceStateCount
)

// calendarFeedFenceStateForContinuity classifies a
// *services.CalendarFeedFenceContinuityError by which half(s) it found,
// mirroring the doc comment on that type: both absent is "never armed", each
// half missing on its own gets its own state — the sentence an operator acts
// on has to name which half is empty — and both present but different is the
// disagreement a restored backup produces.
func calendarFeedFenceStateForContinuity(continuity *services.CalendarFeedFenceContinuityError) calendarFeedFenceConfirmState {
	switch {
	case !continuity.AnchorFound && !continuity.StoredFound:
		return calendarFeedFenceStateNeverArmed
	case !continuity.AnchorFound:
		return calendarFeedFenceStateAnchorMissing
	case !continuity.StoredFound:
		return calendarFeedFenceStateMarkerMissing
	default:
		return calendarFeedFenceStateDisagree
	}
}

// calendarFeedFenceRefusal is one state's operator-facing half: the sentence
// that says what was found, and the remedy that follows from it.
type calendarFeedFenceRefusal struct {
	// sentence renders the state. It takes the resolved path and the
	// underlying cause (nil for the states that carry none) rather than being
	// a constant, because the states that do carry a cause have to place it
	// inside the sentence, not after it.
	sentence func(fencePath string, cause error) string
	remedy   string
}

const (
	// dockerCLIForms are the three ways to reach the binary inside the
	// container, spelled once so two remedies cannot drift into naming
	// different ones.
	dockerCLIForms = "(`docker exec ovumcy /app/ovumcy ...`; `docker compose exec ovumcy /app/ovumcy ...` if the service is already running, " +
		"`docker compose run --rm ovumcy /app/ovumcy ...` if it is not)"
	runElsewhere = "Run the operator CLI where the server's fence is visible " + dockerCLIForms + ", or set " +
		security.CalendarFeedFencePathEnv + " to the same path the server uses — the server's own file, never a copy of it."
	// A relative CALENDAR_FEED_FENCE_PATH is unsafe no matter which shell
	// reads it, including one that copied the exact string the server itself
	// was given: it still resolves against a working directory, and telling
	// the operator to "set it to the same path the server uses" is either
	// what they already did or leads them to copy a value that will keep
	// resolving wrong in whichever shell reads it next. The server is the
	// side that has to change.
	reconfigureTheServer = "A relative path cannot safely name the server's fence from any shell, including one already set to the exact string the " +
		"server was given — it resolves against a working directory, and this process's is not the server's. Reconfigure the SERVER's own " +
		security.CalendarFeedFencePathEnv + " to an absolute path, restart it once, then point this command at that same absolute path and re-run."
	startTheServer = "Start the server once with this fence configured — its boot pass disarms every armed calendar feed on the " +
		"instance before it reconciles both halves — then re-run this command."
	// Distinct from startTheServer: a fence that has NEVER recorded a marker
	// anywhere may mean the server has never had a writable fence at all —
	// the compose image sets CALENDAR_FEED_FENCE_PATH unconditionally, so an
	// operator who skipped mounting the volume sees exactly this shape rather
	// than an obvious "no variable set" refusal — and "start the server" on
	// its own leaves that operator restarting into the same refusal forever.
	giveTheServerAWritableFence = "Give the server a writable fence — mount the `ovumcy_fence` volume as the bundled compose stacks do, or point " +
		security.CalendarFeedFencePathEnv + " at a writable directory — start it once, then re-run this command."
	makeTheDatabaseAnswer = "Re-run this command once the database answers."
	// Named apart from runElsewhere/startTheServer because neither on its own
	// is honest here: the database DOES carry a marker, so a bare restart
	// risks nothing extra, but it will not help if this process simply cannot
	// see a file the server can — and if the server sees the same missing or
	// empty file, or has never been able to write one, the step differs again.
	// Every route into this state gets its own clause.
	anchorMissingRemedy = "Either this process cannot see the server's own fence file — run the operator CLI where that file is visible " +
		dockerCLIForms + ", or set " + security.CalendarFeedFencePathEnv + " to the same path the server uses — or the server sees the same missing " +
		"or empty file, in which case starting it once disarms every armed calendar feed on the instance and rewrites both halves, and this " +
		"command can then be re-run. If the server has never been able to write a fence at all (no volume mounted), give it a writable one first."
)

// calendarFeedFenceConfirmRefusals is the whole refusal vocabulary, one entry
// per state. A table rather than a switch, so the exhaustiveness guard can
// walk the declared states against it instead of against a hand-written mirror
// of itself, and so a state added without an entry reaches
// calendarFeedFenceConfirmRefusal's panic rather than silently borrowing a
// neighbouring sentence and handing the operator a remedy for a refusal that
// never happened.
var calendarFeedFenceConfirmRefusals = map[calendarFeedFenceConfirmState]calendarFeedFenceRefusal{
	calendarFeedFenceStateNotSet: {
		sentence: func(string, error) string {
			return security.CalendarFeedFencePathEnv + " is not set in this shell."
		},
		remedy: runElsewhere,
	},
	calendarFeedFenceStateRelative: {
		sentence: func(fencePath string, _ error) string {
			return security.CalendarFeedFencePathEnv + " is " + fencePath +
				", a relative path that resolves against this process's own working directory, not the server's."
		},
		remedy: reconfigureTheServer,
	},
	calendarFeedFenceStateUnreachable: {
		// A path whose directory does not exist is NOT this state: Read
		// reports a missing path as absent, never as an error. What does
		// arrive here is a path this process may not read, a path naming a
		// directory or some other non-regular file, a file too large to be a
		// fence token, and — once the two halves already agreed — a write
		// refused by a read-only mount or a full disk. The sentence names
		// those rather than a missing directory, and the remedy follows them.
		sentence: func(fencePath string, cause error) string {
			sentence := security.CalendarFeedFencePathEnv + " points at " + fencePath + ", which this process cannot read or write"
			if cause != nil {
				sentence += ": " + cause.Error()
			}
			return sentence + ". The path must name a small regular file this process may read and write."
		},
		remedy: runElsewhere,
	},
	calendarFeedFenceStateMarkerUnavailable: {
		sentence: func(fencePath string, cause error) string {
			sentence := "the database marker paired with " + security.CalendarFeedFencePathEnv + " at " + fencePath + " could not be read"
			if cause != nil {
				sentence += ": " + cause.Error()
			}
			return sentence + "."
		},
		remedy: makeTheDatabaseAnswer,
	},
	calendarFeedFenceStateAnchorMissing: {
		sentence: func(fencePath string, _ error) string {
			return "the database carries a fence marker, but no fence value is visible from this process at " + fencePath +
				" (the file is missing, or present and empty)."
		},
		remedy: anchorMissingRemedy,
	},
	calendarFeedFenceStateMarkerMissing: {
		sentence: func(fencePath string, _ error) string {
			return "the fence file " + security.CalendarFeedFencePathEnv + " names at " + fencePath +
				" carries a token but the database has no marker."
		},
		remedy: startTheServer,
	},
	calendarFeedFenceStateDisagree: {
		sentence: func(fencePath string, _ error) string {
			return security.CalendarFeedFencePathEnv + " at " + fencePath + " and the database marker hold different tokens."
		},
		remedy: startTheServer,
	},
	calendarFeedFenceStateNeverArmed: {
		sentence: func(fencePath string, _ error) string {
			return "neither " + security.CalendarFeedFencePathEnv + " at " + fencePath + " nor the database has ever recorded a marker."
		},
		remedy: giveTheServerAWritableFence,
	},
}

// calendarFeedFenceConfirmRefusal composes the operator-facing refusal:
// CALENDAR_FEED_FENCE_PATH, the state that stopped the write, what a
// completed-but-unrecorded removal would cost, and a remedy — always ending
// "Nothing was changed" so an operator scanning the tail of the message never
// has to wonder whether the command it names already ran.
//
// A state with no table entry panics rather than falling back to one.
// calendarFeedFenceConfirmRefusal never runs across an operator's own mistake,
// so the panic is a programming error caught in tests, not something a real
// shell can trigger.
func calendarFeedFenceConfirmRefusal(fencePath string, state calendarFeedFenceConfirmState, cause error) error {
	const consequence = "A removal recorded only inside the database is undone by restoring a backup taken before it."

	refusal, ok := calendarFeedFenceConfirmRefusals[state]
	if !ok {
		panic(fmt.Sprintf("calendar-feed restore fence: confirmOperatorFeedRevocation state %d has no refusal text", state))
	}

	// No trailing period (staticcheck ST1005): Go error strings end without
	// punctuation, even a deliberately long, multi-sentence operator one.
	return fmt.Errorf("calendar-feed restore fence: %s %s %s Nothing was changed", refusal.sentence(fencePath, cause), consequence, refusal.remedy)
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
