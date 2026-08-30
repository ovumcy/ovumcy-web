package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestRecomputeDerivedLutealPhasesAcrossBoots drives the real boot wrapper
// against a migrated SQLite database seeded with one account carrying a stale
// personalized estimate and no logs left to support it — the account that can
// never self-heal, because both other writers of the cache run only when the
// owner writes something.
//
// The first boot must land it on the 14-day default rather than on 17, the
// value an arithmetic repair of the stored number would have produced, and
// record the done-marker; the second must leave both the marker and the value
// alone. (Row-level derivation semantics are proven in internal/services, the
// projection in internal/db.)
func TestRecomputeDerivedLutealPhasesAcrossBoots(t *testing.T) {
	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "luteal-recompute-boot.db")})
	if err != nil {
		t.Fatalf("OpenDatabase: %v", err)
	}
	t.Cleanup(func() { closeDatabase(database) })
	repositories := db.NewRepositories(database)
	ctx := context.Background()

	stale := models.User{
		Email:               "boot-luteal@example.com",
		PasswordHash:        "hash",
		RecoveryCodeHash:    "recovery",
		Role:                models.RoleOwner,
		LocalAuthEnabled:    true,
		OnboardingCompleted: true,
		CycleLength:         28,
		PeriodLength:        5,
		LutealPhase:         18,
	}
	if err := repositories.Users.Create(ctx, &stale); err != nil {
		t.Fatalf("seed stale user: %v", err)
	}

	recomputeDerivedLutealPhases(repositories, time.UTC)

	repaired, err := repositories.Users.FindByID(ctx, stale.ID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if repaired.LutealPhase != 14 {
		t.Fatalf("first boot left luteal_phase = %d, want 14 (17 would be the stored value shifted, which no derivation produces)", repaired.LutealPhase)
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
	settled, err := repositories.Users.FindByID(ctx, stale.ID)
	if err != nil || settled.LutealPhase != 14 {
		t.Fatalf("second boot must leave the repaired value alone (err=%v, luteal_phase=%d)", err, settled.LutealPhase)
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
		"a skipped row says the next boot retries": {
			outcome: services.LutealPhaseRecomputeOutcome{Corrected: 2, Failed: 1},
			want:    "luteal-phase recompute: 2 stored estimate(s) corrected, 1 account(s) could not be read or written — retried on the next start",
		},
		"a pass that corrected nothing but failed still reports": {
			outcome: services.LutealPhaseRecomputeOutcome{Failed: 4},
			want:    "luteal-phase recompute: 0 stored estimate(s) corrected, 4 account(s) could not be read or written — retried on the next start",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := lutealPhaseRecomputeStartupMessage(testCase.outcome); got != testCase.want {
				t.Fatalf("message = %q, want %q", got, testCase.want)
			}
		})
	}
}
