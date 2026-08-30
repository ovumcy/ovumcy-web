package services

import (
	"context"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// lutealPhaseRecomputeAppState is the narrow app_state surface the one-shot
// pass needs: read the done-marker, write it once.
type lutealPhaseRecomputeAppState interface {
	Get(ctx context.Context, key string) (string, bool, error)
	Set(ctx context.Context, key string, value string) error
}

// lutealPhaseRecomputeUserStore lists the owner rows the pass walks and writes
// the single column it is allowed to write.
type lutealPhaseRecomputeUserStore interface {
	ListOwnerLutealPhaseRows(ctx context.Context) ([]models.LutealPhaseRecomputeRow, error)
	UpdateByID(ctx context.Context, userID uint, updates map[string]any) error
}

// lutealPhaseRecomputeLogStore reads one owner's day logs — the input the
// derivation runs on, and the reason this cannot be done in SQL.
type lutealPhaseRecomputeLogStore interface {
	ListByUser(ctx context.Context, userID uint) ([]models.DailyLog, error)
}

// LutealPhaseRecomputeOutcome reports what one Run pass did, for the
// operator-facing startup line. Counts only: no account and no health value
// may reach a log.
type LutealPhaseRecomputeOutcome struct {
	// AlreadyDone: the marker was present, nothing was scanned.
	AlreadyDone bool
	// Corrected counts rows whose stored estimate differed from the recomputed
	// one and were rewritten. A row the recompute agrees with is not written.
	Corrected int64
	// Failed counts rows whose logs could not be read or whose update failed.
	// They are skipped rather than aborting the pass, and a non-zero count
	// leaves the marker unwritten so the next boot walks everyone again.
	Failed int64
}

// LutealPhaseRecomputer is the one-shot boot pass that rewrites the derived
// users.luteal_phase cache under the corrected personalized inference.
//
// Why the column needs a pass at all. The inference used to measure the
// calendar span from the observed ovulation date to the next period start,
// which counts the ovulation day itself and so runs one day longer than the
// parameter CalcOvulationDay consumes; an ovulation observed on cycle day 15
// predicted cycle day 14. The computation is fixed, but its result is cached in
// users.luteal_phase, and the two writers that refresh that cache
// (DayService and ImportService, after a day save and after a bulk restore) only
// run when the owner writes something. An account whose logs no longer support
// an inference at all — fewer than three observed cycle starts, or fewer than
// two samples surviving the plausibility filter — reads the stored value through
// resolveUserCycleLengths and keeps the day-early prediction indefinitely.
//
// Why it is not a SQL migration. The correction is not a function of the stored
// number, so no UPDATE over the column can express it, in three separate ways:
//
//  1. 14 is ambiguous. It is the seeded default, the value written whenever the
//     inference declines to refine, AND a plausible old-convention estimate that
//     should now become 13. SQL cannot tell those apart.
//  2. The sample set moves. The plausibility filter (minLutealPhaseDays to
//     maxPlausibleLutealPhaseDays) was applied to the shifted quantity, so a
//     cycle whose corrected sample is 20 was dropped as 21 and now counts, while
//     one whose corrected sample is 9 was kept as 10 and now does not. The
//     average is taken over a different set, not over the same set minus one.
//  3. The refine gate flips. An owner left with fewer than two surviving samples
//     falls back to defaultLutealPhaseDays instead of to a shifted value.
//
// Recomputing means re-running InferUserLutealPhase over the owner's logs, which
// is Go — the same shape as the auth-email pass, which cannot be SQL because
// reducing an RFC 5322 form to its addr-spec requires mail.ParseAddress.
//
// Why it runs at boot rather than from a CLI command. Ovumcy is self-hosted: a
// repair that only happens when the operator remembers to run it leaves exactly
// the accounts this pass exists for — the ones that can no longer self-heal —
// on the old convention forever.
//
// Unlike the two boot passes beside it, a failure here does NOT stop the server.
// The column is a derived cache with a safe fallback (ResolveLutealPhase), and
// its worst stale value is a prediction one day early, so a transient storage
// error must not turn into an instance that will not start. Per-row failures are
// counted and skipped; the marker is written only after a pass in which nothing
// failed, so the next boot retries. Every individual rewrite is idempotent — a
// row already carrying the recomputed value is left alone.
//
// Deleting the app_state row named by AppStateKeyLutealPhaseRecomputeV1 forces
// one more pass on the next boot.
type LutealPhaseRecomputer struct {
	appState         lutealPhaseRecomputeAppState
	users            lutealPhaseRecomputeUserStore
	logs             lutealPhaseRecomputeLogStore
	fallbackLocation *time.Location
}

// NewLutealPhaseRecomputer wires the pass. fallbackLocation is the zone used for
// an owner who has no usable stored timezone (see resolveOwnerLocation); nil
// means UTC.
func NewLutealPhaseRecomputer(
	appState lutealPhaseRecomputeAppState,
	users lutealPhaseRecomputeUserStore,
	logs lutealPhaseRecomputeLogStore,
	fallbackLocation *time.Location,
) *LutealPhaseRecomputer {
	if fallbackLocation == nil {
		fallbackLocation = time.UTC
	}
	return &LutealPhaseRecomputer{appState: appState, users: users, logs: logs, fallbackLocation: fallbackLocation}
}

// Run executes the pass and returns the outcome for the startup line. It returns
// an error only when the marker or the owner listing itself is unreadable —
// neither leaves the marker written, so the next boot repeats the whole pass.
func (recomputer *LutealPhaseRecomputer) Run(ctx context.Context) (LutealPhaseRecomputeOutcome, error) {
	_, done, err := recomputer.appState.Get(ctx, models.AppStateKeyLutealPhaseRecomputeV1)
	if err != nil {
		return LutealPhaseRecomputeOutcome{}, err
	}
	if done {
		return LutealPhaseRecomputeOutcome{AlreadyDone: true}, nil
	}

	rows, err := recomputer.users.ListOwnerLutealPhaseRows(ctx)
	if err != nil {
		return LutealPhaseRecomputeOutcome{}, err
	}

	var outcome LutealPhaseRecomputeOutcome
	for _, row := range rows {
		logs, err := recomputer.logs.ListByUser(ctx, row.ID)
		if err != nil {
			outcome.Failed++
			continue
		}

		derived := DeriveUserLutealPhase(logs, resolveOwnerLocation(row.Timezone, recomputer.fallbackLocation))
		if derived == row.LutealPhase {
			continue
		}
		if err := recomputer.users.UpdateByID(ctx, row.ID, map[string]any{"luteal_phase": derived}); err != nil {
			outcome.Failed++
			continue
		}
		outcome.Corrected++
	}

	if outcome.Failed > 0 {
		return outcome, nil
	}
	if err := recomputer.appState.Set(ctx, models.AppStateKeyLutealPhaseRecomputeV1, "done"); err != nil {
		return outcome, err
	}
	return outcome, nil
}
