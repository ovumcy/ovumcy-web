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

// calendarFeedFencePath resolves the fence location once, so the value a
// subcommand confirms against is provably the value its repositories were
// built with.
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
// The fence this returns shares the server's app_state but very often not its
// anchor — an operator's shell rarely has CALENDAR_FEED_FENCE_PATH set to the
// server's own path. confirmOperatorFeedRevocation is what the two
// feed-affecting subcommands call to find out, immediately before the write
// that would remove access: it confirms and advances the SAME fence rather
// than merely warning about it, and refuses the operation outright when it
// cannot.
func buildRepositories(database *gorm.DB) (*db.Repositories, *services.CalendarFeedRestoreFence) {
	return bootstrap.BuildRepositories(database, calendarFeedFencePath())
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
// caller must not proceed to its own write — and every refusal ends "Nothing
// was changed."
func confirmOperatorFeedRevocation(ctx context.Context, fencePath string, fence *services.CalendarFeedRestoreFence, errOutput io.Writer) error {
	fencePath = strings.TrimSpace(fencePath)
	if fencePath == "" || !rootedPath(fencePath) {
		return calendarFeedFenceConfirmRefusal(fencePath, calendarFeedFenceStateNotSet)
	}

	if err := fence.AdvanceConfirmed(ctx); err != nil {
		var continuity *services.CalendarFeedFenceContinuityError
		switch {
		case errors.As(err, &continuity):
			state := calendarFeedFenceStateDisagree
			if !continuity.AnchorFound && !continuity.StoredFound {
				state = calendarFeedFenceStateNeverArmed
			}
			return calendarFeedFenceConfirmRefusal(fencePath, state)
		case errors.Is(err, services.ErrCalendarFeedFenceUnreachable):
			return calendarFeedFenceConfirmRefusal(fencePath, calendarFeedFenceStateUnreachable)
		default:
			// A database failure: AdvanceConfirmed's own doc explains why the
			// file may already be ahead of app_state at this point (fail
			// closed — the next boot disarms). Nothing this function could
			// add would be more actionable than the storage error itself.
			return fmt.Errorf("calendar feed restore fence: %w", err)
		}
	}

	if errOutput == nil {
		errOutput = os.Stderr
	}
	_, _ = fmt.Fprintf(errOutput, "calendar-feed restore fence: continuity confirmed at %s; this removal is recorded outside the database\n", fencePath)
	return nil
}

// calendarFeedFenceConfirmState names the shape confirmOperatorFeedRevocation
// refused in, because the operator remedy differs by shape: a path problem is
// fixed by running the CLI somewhere the server's fence is visible, while a
// fence that has never recorded a token anywhere, or that records two
// different generations, needs the SERVER to reconcile it — no CLI process
// can safely guess which half is right.
type calendarFeedFenceConfirmState int

const (
	calendarFeedFenceStateNotSet calendarFeedFenceConfirmState = iota
	calendarFeedFenceStateUnreachable
	calendarFeedFenceStateDisagree
	calendarFeedFenceStateNeverArmed
)

// calendarFeedFenceConfirmRefusal composes the operator-facing refusal:
// CALENDAR_FEED_FENCE_PATH, the state that stopped the write, what a
// completed-but-unrecorded removal would cost, and a remedy — always ending
// "Nothing was changed" so an operator scanning the tail of the message never
// has to wonder whether the command it names already ran.
func calendarFeedFenceConfirmRefusal(fencePath string, state calendarFeedFenceConfirmState) error {
	const consequence = "A removal recorded only inside the database is undone by restoring a backup taken before it."
	const runElsewhere = "Run the operator CLI where the server's fence is visible " +
		"(`docker exec ovumcy /app/ovumcy ...`; `docker compose exec ovumcy /app/ovumcy ...` if the service is already running, " +
		"`docker compose run --rm ovumcy /app/ovumcy ...` if it is not), or set " + security.CalendarFeedFencePathEnv +
		" to the same path the server uses."
	const startTheServer = "Start the server once with this fence configured, then re-run this command."

	var stateSentence, remedy string
	switch state {
	case calendarFeedFenceStateUnreachable:
		stateSentence = security.CalendarFeedFencePathEnv + " points at " + fencePath + ", whose directory does not exist or cannot be written from this process."
		remedy = runElsewhere
	case calendarFeedFenceStateDisagree:
		stateSentence = security.CalendarFeedFencePathEnv + " at " + fencePath + " and the database marker do not agree."
		remedy = startTheServer
	case calendarFeedFenceStateNeverArmed:
		stateSentence = "neither " + security.CalendarFeedFencePathEnv + " at " + fencePath + " nor the database has ever recorded a marker."
		remedy = startTheServer
	default: // calendarFeedFenceStateNotSet
		if fencePath == "" {
			stateSentence = security.CalendarFeedFencePathEnv + " is not set in this shell."
		} else {
			stateSentence = security.CalendarFeedFencePathEnv + " is " + fencePath + ", which resolves against this process's own working directory, not the server's."
		}
		remedy = runElsewhere
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
