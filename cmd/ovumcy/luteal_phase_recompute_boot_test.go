package main

import (
	"bytes"
	"context"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// bootRecomputeOrigin is the first cycle start the seeded history is built from.
// Any UTC date works; the inference reads calendar-day differences only.
var bootRecomputeOrigin = time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)

// The seeded history, and why these three numbers. Cycles run 28 days; the
// egg-white peak sits on cycle day 11, so ovulation reads as cycle day 12
// (peak + 1) and the corrected inference derives 28 - 12 = 16.
//
// 16 rather than 14 is the point: defaultLutealPhaseDays is 14, so a history
// that inferred 14 would be indistinguishable from the decline branch, and this
// test would pass unchanged if the logs never reached the derivation at all. The
// old convention measured the span from that ovulation to the next start and
// stored 17, which is what the caller seeds on the account.
const (
	bootRecomputeCycles           = 3
	bootRecomputeCycleLength      = 28
	bootRecomputeOvulationDay     = 12
	bootRecomputeInferredLuteal   = bootRecomputeCycleLength - bootRecomputeOvulationDay
	bootRecomputeStoredUnderOldV1 = bootRecomputeInferredLuteal + 1
)

// seedInferableHistory writes bootRecomputeCycles observed cycle starts and
// plants an ovulation signal in every cycle but the last, which only closes the
// one before it.
//
// It exists so the boot pass is driven through the REAL DailyLogRepository
// rather than a stub: the derivation, the projection and the update are proven
// apart elsewhere, and this is the only place they are proven joined.
func seedInferableHistory(t *testing.T, repositories *db.Repositories, userID uint) {
	t.Helper()

	logs := make([]models.DailyLog, 0, bootRecomputeCycles*3)
	for cycle := range bootRecomputeCycles {
		start := bootRecomputeOrigin.AddDate(0, 0, cycle*bootRecomputeCycleLength)
		logs = append(logs,
			models.DailyLog{UserID: userID, Date: start, IsPeriod: true, CycleStart: true, Flow: models.FlowMedium},
			models.DailyLog{UserID: userID, Date: start.AddDate(0, 0, 1), IsPeriod: true, Flow: models.FlowMedium},
		)
		if cycle == bootRecomputeCycles-1 {
			continue // the last start only closes the previous cycle
		}
		logs = append(logs, models.DailyLog{
			UserID: userID,
			// Peak day: ovulation is peak + 1, and the offset is zero-based.
			Date:          start.AddDate(0, 0, bootRecomputeOvulationDay-2),
			CervicalMucus: models.CervicalMucusEggWhite,
		})
	}
	if err := repositories.DailyLogs.CreateBatch(context.Background(), logs); err != nil {
		t.Fatalf("seed logs for user %d: %v", userID, err)
	}
}

func seedOwner(t *testing.T, repositories *db.Repositories, email string, lutealPhase int) models.User {
	t.Helper()

	user := models.User{
		Email:               email,
		PasswordHash:        "hash",
		RecoveryCodeHash:    "recovery",
		Role:                models.RoleOwner,
		LocalAuthEnabled:    true,
		OnboardingCompleted: true,
		CycleLength:         28,
		PeriodLength:        5,
		LutealPhase:         lutealPhase,
	}
	if err := repositories.Users.Create(context.Background(), &user); err != nil {
		t.Fatalf("seed %s: %v", email, err)
	}
	return user
}

func lutealPhaseOf(t *testing.T, repositories *db.Repositories, userID uint) int {
	t.Helper()

	user, err := repositories.Users.FindByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("reload user %d: %v", userID, err)
	}
	return user.LutealPhase
}

// TestRecomputeDerivedLutealPhasesAcrossBoots drives the real boot wrapper
// against a migrated SQLite database seeded with the two populations that
// matter, so neither outcome can be produced by the wrong mechanism.
//
// The inferable account still has the history that earns a personalized
// estimate: it must land on what the corrected inference reads from those logs,
// which only a real read of daily_logs can produce. That value is deliberately
// NOT the 14-day default, so the assertion fails rather than passes if the logs
// never reach the derivation.
//
// The stranded account has a stale personalized value and no history left to
// support it — the population that can never self-heal, because both other
// writers of the cache run only when the owner writes something. It must land on
// the 14-day default rather than on 17, the value an arithmetic repair of the
// stored number would leave.
//
// The second boot must then leave the marker and both values alone.
func TestRecomputeDerivedLutealPhasesAcrossBoots(t *testing.T) {
	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "luteal-recompute-boot.db")})
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	t.Cleanup(func() { closeDatabase(database) })
	repositories := db.NewRepositories(database)
	ctx := context.Background()

	inferable := seedOwner(t, repositories, "boot-luteal-inferable@example.com", bootRecomputeStoredUnderOldV1)
	seedInferableHistory(t, repositories, inferable.ID)
	stranded := seedOwner(t, repositories, "boot-luteal-stranded@example.com", 18)

	recomputeDerivedLutealPhases(repositories, time.UTC)

	if got := lutealPhaseOf(t, repositories, inferable.ID); got != bootRecomputeInferredLuteal {
		t.Fatalf("the inferable account landed on %d, want %d read back from its own logs (14 would mean the logs never reached the derivation)", got, bootRecomputeInferredLuteal)
	}
	if got := lutealPhaseOf(t, repositories, stranded.ID); got != 14 {
		t.Fatalf("the stranded account landed on %d, want 14 (17 would be the stored value shifted, which no derivation produces)", got)
	}
	first, found, err := repositories.AppState.Get(ctx, models.AppStateKeyLutealPhaseRecomputeV1)
	if err != nil || !found || first == "" {
		t.Fatalf("first boot must record the done-marker (found=%v, err=%v, value=%q)", found, err, first)
	}

	recomputeDerivedLutealPhases(repositories, time.UTC)

	second, _, err := repositories.AppState.Get(ctx, models.AppStateKeyLutealPhaseRecomputeV1)
	if err != nil || second != first {
		t.Fatalf("second boot must keep the marker (err=%v, before=%q, after=%q)", err, first, second)
	}
	if got := lutealPhaseOf(t, repositories, inferable.ID); got != bootRecomputeInferredLuteal {
		t.Fatalf("second boot moved the inferable account to %d", got)
	}
	if got := lutealPhaseOf(t, repositories, stranded.ID); got != 14 {
		t.Fatalf("second boot moved the stranded account to %d", got)
	}
}

// TestBootRecomputeFixtureEarnsItsExpectation pins what the seeded history means
// before the boot test is asked about it: a fixture built outside the inference's
// range would send the account down the decline branch, and this says so in one
// line instead of leaving the boot test to report a wrong luteal value.
//
// The boot test no longer DEPENDS on this one — its expected value is not the
// default, so a fixture that supplied no signal fails it directly. This narrows
// the failure to its cause rather than carrying the assertion.
func TestBootRecomputeFixtureEarnsItsExpectation(t *testing.T) {
	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "luteal-recompute-fixture.db")})
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	t.Cleanup(func() { closeDatabase(database) })
	repositories := db.NewRepositories(database)

	user := seedOwner(t, repositories, "boot-luteal-fixture@example.com", bootRecomputeStoredUnderOldV1)
	seedInferableHistory(t, repositories, user.ID)

	logs, err := repositories.DailyLogs.ListByUser(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("ListByUser: %v", err)
	}
	luteal, refined := services.InferUserLutealPhase(logs, time.UTC)
	if !refined {
		t.Fatal("fixture: the seeded history must support an inference; built outside the inference's range it supplies no signal at all")
	}
	if luteal != bootRecomputeInferredLuteal {
		t.Fatalf("fixture: the seeded history infers %d, want %d", luteal, bootRecomputeInferredLuteal)
	}
}

// TestRecomputeDerivedLutealPhasesLetsTheServerStartWhenStorageFails is the
// test for the one decision that separates this wrapper from the two beside it.
// mustEnforceCalendarFeedKeyRotation and mustRenormalizeAuthEmails call
// log.Fatalf, because a feed left armed or an account left unable to sign in is
// worse than a stopped instance. A luteal-phase estimate is a derived cache with
// a safe fallback, so the same failure here must NOT stop the server.
//
// Driven against a database closed out from under the pass, which is the real
// shape of the failure — the marker read is the pass's first statement. The
// assertion is that control returns at all, and that the operator is told why
// and that it will be retried; a wrapper that grew a log.Fatalf later would kill
// the test process instead of failing this test, which is the loudest possible
// way to notice.
func TestRecomputeDerivedLutealPhasesLetsTheServerStartWhenStorageFails(t *testing.T) {
	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "luteal-recompute-closed.db")})
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	repositories := db.NewRepositories(database)
	closeDatabase(database)

	var captured bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&captured)
	t.Cleanup(func() { log.SetOutput(previous) })

	recomputeDerivedLutealPhases(repositories, time.UTC)

	logged := captured.String()
	if !strings.Contains(logged, "luteal-phase recompute failed") {
		t.Fatalf("the failure must reach the operator, got %q", logged)
	}
	if !strings.Contains(logged, "retried on the next start") {
		t.Fatalf("the line must say the pass is retried, got %q", logged)
	}
}

// TestLutealPhaseRecomputeStartupMessage pins the operator-facing log line:
// silent for the routine outcomes, counts only — a per-account luteal-phase
// estimate is health data and must not reach logs.
func TestLutealPhaseRecomputeStartupMessage(t *testing.T) {
	for name, testCase := range map[string]struct {
		outcome services.LutealPhaseRecomputeOutcome
		want    string
	}{
		"already done stays silent": {
			outcome: services.LutealPhaseRecomputeOutcome{AlreadyDone: true},
			want:    "",
		},
		"a pass with nothing to correct stays silent": {
			outcome: services.LutealPhaseRecomputeOutcome{},
			want:    "",
		},
		"corrections are reported as a count": {
			outcome: services.LutealPhaseRecomputeOutcome{Corrected: 3},
			want:    "luteal-phase recompute: 3 stored estimate(s) corrected",
		},
		"a skipped row says the next boot retries and names the durable case": {
			outcome: services.LutealPhaseRecomputeOutcome{Corrected: 2, Failed: 1},
			want:    "luteal-phase recompute: 2 stored estimate(s) corrected, 1 account(s) could not be read or written — retried on the next start, and a count that repeats across starts is a durable fault to investigate",
		},
		"a pass that corrected nothing but failed still reports": {
			outcome: services.LutealPhaseRecomputeOutcome{Failed: 4},
			want:    "luteal-phase recompute: 0 stored estimate(s) corrected, 4 account(s) could not be read or written — retried on the next start, and a count that repeats across starts is a durable fault to investigate",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := lutealPhaseRecomputeStartupMessage(testCase.outcome); got != testCase.want {
				t.Fatalf("message = %q, want %q", got, testCase.want)
			}
		})
	}
}
