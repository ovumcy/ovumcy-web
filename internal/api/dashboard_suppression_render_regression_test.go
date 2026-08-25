package api

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

// The dashboard publishes its fertility verdict three times — the status
// header's `data-fertility-status`, the fertile-window item beside the phase,
// and the journal grid's `data-fertility` — and only the first two were gated
// at all. The gate they shared carried one disjunct of the shared predicate
// (the zero-completed-cycle floor), so an owner in unpredictable-cycle mode or
// on a pregnancy pause read "Fertile window" on the same line as the notice
// saying the account's predictions are off, and the grid published the raw
// classification for every tier.
//
// This is the render half of the fix: the three hooks are asserted together,
// against an account that genuinely classifies as fertile, so a suppression
// that only reached the attribute the previous regression named cannot pass.

// dashboardSuppressionSeed is the shared baseline: a 28-day account whose
// running cycle is on day 12 — inside the fertile window (ovulation day 14,
// window days 9-14) with a day of timezone slack on either side — with two
// completed cycles behind it.
func dashboardSuppressionSeed(t *testing.T, database *gorm.DB, userID uint, columns map[string]any) {
	t.Helper()

	today := services.DateAtLocation(time.Now().UTC(), time.UTC)
	updates := map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"luteal_phase":      14,
		"last_period_start": today.AddDate(0, 0, -11),
	}
	for column, value := range columns {
		updates[column] = value
	}
	if err := database.Model(&models.User{}).Where("id = ?", userID).Updates(updates).Error; err != nil {
		t.Fatalf("seed cycle baseline: %v", err)
	}
	for _, offsetDays := range []int{-67, -39, -11} {
		if err := database.Create(&models.DailyLog{
			UserID:     userID,
			Date:       today.AddDate(0, 0, offsetDays),
			IsPeriod:   true,
			CycleStart: true,
		}).Error; err != nil {
			t.Fatalf("seed cycle start %d: %v", offsetDays, err)
		}
	}
}

func TestDashboardWithholdsEveryFertilityClaimTheSharedGateSuppresses(t *testing.T) {
	for name, testCase := range map[string]struct {
		account         string
		columns         map[string]any
		pregnancyPaused bool
		wantFertile     bool
	}{
		"nothing suppressed: the measured window renders": {
			account:     "anchor",
			wantFertile: true,
		},
		"unpredictable-cycle mode": {
			account: "unpredictable",
			columns: map[string]any{"unpredictable_cycle": true},
		},
		"pregnancy pause": {
			account:         "pregnancy-pause",
			pregnancyPaused: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			app, database := newOnboardingTestApp(t)
			user := createOnboardingTestUser(t, database, "dashboard-suppression-"+testCase.account+"@example.com", "StrongPass1", true)
			dashboardSuppressionSeed(t, database, user.ID, testCase.columns)
			if testCase.pregnancyPaused {
				// A positive test after the running cycle's start with no later
				// start: ResolvePregnancyPause reports an active pause.
				if err := database.Create(&models.DailyLog{
					UserID:        user.ID,
					Date:          services.DateAtLocation(time.Now().UTC(), time.UTC).AddDate(0, 0, -2),
					PregnancyTest: models.PregnancyTestPositive,
				}).Error; err != nil {
					t.Fatalf("seed positive pregnancy test: %v", err)
				}
			}

			authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
			document := mustParseHTMLDocument(t, mustRenderDashboard(t, app, authCookie, "en"))

			header := dashboardElementByDataAttr(document, "data-dashboard-status-header")
			if header == nil {
				t.Fatal("expected the dashboard status header")
			}
			wantStatus := "unknown"
			if testCase.wantFertile {
				wantStatus = "fertile"
			}
			if got := htmlAttr(header, "data-fertility-status"); got != wantStatus {
				t.Errorf("status header declares fertility %q, want %q", got, wantStatus)
			}

			statusLine := dashboardElementByDataAttr(document, "data-dashboard-status-line")
			if statusLine == nil {
				t.Fatal("expected the dashboard status line")
			}
			if got := htmlFindElement(statusLine, htmlNodeHasAttr("data-fertile-window")) != nil; got != testCase.wantFertile {
				t.Errorf("fertile-window item rendered = %v, want %v", got, testCase.wantFertile)
			}

			// The journal grid republishes the same classification for the
			// scripts and stylesheet rules that read it, and had no gate at all.
			editor := dashboardElementByDataAttr(document, "data-dashboard-editor")
			if editor == nil {
				t.Fatal("expected the dashboard journal grid")
			}
			if got := htmlAttr(editor, "data-fertility"); got != wantStatus {
				t.Errorf("journal grid publishes data-fertility=%q, want %q", got, wantStatus)
			}
		})
	}
}

// TestDashboardPausesTheNextPeriodEstimateForAnOverdueIrregularAccount is the
// render regression for the branch-order half. An overdue cycle carries no
// evidence about when the next one starts, so the header names no date — but
// the thin-history "needs more cycles" branch claimed the display first for an
// irregular account with fewer than three completed cycles, and that account
// read a named date with a qualifier where the floor is suppression.
//
// The account inside its reference length is the positive anchor: the
// needs-data message is a real state and must survive the fix.
func TestDashboardPausesTheNextPeriodEstimateForAnOverdueIrregularAccount(t *testing.T) {
	for name, testCase := range map[string]struct {
		account     string
		startOffset int
		wantPaused  bool
	}{
		"overdue past the reference length": {account: "overdue", startOffset: -54, wantPaused: true},
		"inside the reference length":       {account: "current", startOffset: -11},
	} {
		t.Run(name, func(t *testing.T) {
			app, database := newOnboardingTestApp(t)
			user := createOnboardingTestUser(t, database, "dashboard-overdue-irregular-"+testCase.account+"@example.com", "StrongPass1", true)
			today := services.DateAtLocation(time.Now().UTC(), time.UTC)
			// Irregular mode with a single completed 28-day cycle behind it:
			// fewer than three, so dashboardNeedsNextPeriodData is armed.
			if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
				"irregular_cycle":   true,
				"cycle_length":      28,
				"period_length":     5,
				"luteal_phase":      14,
				"last_period_start": today.AddDate(0, 0, testCase.startOffset),
			}).Error; err != nil {
				t.Fatalf("seed cycle baseline: %v", err)
			}
			for _, offsetDays := range []int{testCase.startOffset - 28, testCase.startOffset} {
				if err := database.Create(&models.DailyLog{
					UserID:     user.ID,
					Date:       today.AddDate(0, 0, offsetDays),
					IsPeriod:   true,
					CycleStart: true,
				}).Error; err != nil {
					t.Fatalf("seed cycle start %d: %v", offsetDays, err)
				}
			}

			authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
			document := mustParseHTMLDocument(t, mustRenderDashboard(t, app, authCookie, "en"))

			paused := htmlFindElement(document, htmlNodeHasAttr("data-dashboard-next-period-paused"))
			if got := paused != nil; got != testCase.wantPaused {
				t.Errorf("paused next-period notice rendered = %v, want %v", got, testCase.wantPaused)
			}
			named := htmlFindElement(document, htmlNodeHasAttr("data-dashboard-next-period"))
			if got := named != nil; got == testCase.wantPaused {
				t.Errorf("named next-period estimate rendered = %v while paused = %v; an overdue cycle may name no date", got, testCase.wantPaused)
			}
		})
	}
}
