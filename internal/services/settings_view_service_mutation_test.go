package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestSettingsviewserviceCovExportDateBoundsWhitespacePaddedFromEqualsToday
// kills the CONDITIONALS_BOUNDARY mutant at line 278 of settings_view_service.go
// (`compareISODate(availableSummary.DateFrom, selectableMin) < 0` -> `<= 0`).
//
// Mechanism: compareISODate TrimSpace-equates " 2026-06-01" with today
// "2026-06-01" and returns 0. Original `< 0` is false, so selectableMin stays
// the CLEAN todayISO "2026-06-01". Mutant `<= 0` is true, so it overrides
// selectableMin with the RAW whitespace-padded availableSummary.DateFrom
// " 2026-06-01". Both selectableMin and defaultFrom are returned verbatim into
// SelectableDateMin/DefaultDateFrom WITHOUT re-trimming, so the divergence is
// observable on returned struct fields (not logs/markup/errors).
func TestSettingsviewserviceCovExportDateBoundsWhitespacePaddedFromEqualsToday(t *testing.T) {
	loader := &stubSettingsViewLoader{
		user: models.User{CycleLength: 28, PeriodLength: 5},
	}
	today := mustParseSettingsViewDay(t, "2026-06-01")
	// DateFrom is today surrounded by whitespace: compareISODate trims and
	// returns 0 (equal), so the < 0 override must NOT fire.
	exportBuilder := &stubSettingsViewExportBuilder{
		responses: []ExportSummary{
			{TotalEntries: 1, HasData: true, DateFrom: " 2026-06-01", DateTo: "2026-06-01"},
			{TotalEntries: 1, HasData: true, DateFrom: "2026-06-01", DateTo: "2026-06-01"},
		},
	}
	svc := NewSettingsViewService(loader, exportBuilder, nil)
	user := &models.User{ID: 30, Role: models.RoleOwner}

	viewData, err := svc.BuildSettingsPageViewData(context.Background(), user, "en", SettingsViewInput{}, today, time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Original (< 0): equal-modulo-whitespace DateFrom does NOT override
	// selectableMin, so the clean todayISO is emitted.
	// Mutant (<= 0): overrides with the raw " 2026-06-01" (leading space) -> fails here.
	if viewData.Export.SelectableDateMin != "2026-06-01" {
		t.Fatalf("line 278: expected clean SelectableDateMin=2026-06-01 (DateFrom equals today modulo whitespace), got %q", viewData.Export.SelectableDateMin)
	}
	// DefaultDateFrom is the same selectableMin string per resolveOwnerExportDateBounds;
	// asserting it too makes the kill robust to either return slot being inspected.
	if viewData.Export.DefaultDateFrom != "2026-06-01" {
		t.Fatalf("line 278: expected clean DefaultDateFrom=2026-06-01, got %q", viewData.Export.DefaultDateFrom)
	}
}
