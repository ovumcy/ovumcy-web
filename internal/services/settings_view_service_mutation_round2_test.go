package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestSettingsviewserviceCovExportDateBoundsWhitespacePaddedToEqualsToday
// kills the CONDITIONALS_BOUNDARY mutant at line 282 of settings_view_service.go
// (`compareISODate(availableSummary.DateTo, selectableMax) > 0` -> `>= 0`).
//
// Mechanism: compareISODate TrimSpace-equates " 2026-06-01" with today
// "2026-06-01" and returns 0. Original `> 0` is false, so selectableMax stays
// the CLEAN todayISO "2026-06-01". Mutant `>= 0` is true, so it overrides
// selectableMax with the RAW whitespace-padded availableSummary.DateTo
// " 2026-06-01". selectableMax is returned verbatim into SelectableDateMax
// WITHOUT re-trimming, so the divergence is observable on the returned struct
// field (not logs/markup/errors).
func TestSettingsviewserviceCovExportDateBoundsWhitespacePaddedToEqualsToday(t *testing.T) {
	loader := &stubSettingsViewLoader{
		user: models.User{CycleLength: 28, PeriodLength: 5},
	}
	today := mustParseSettingsViewDay(t, "2026-06-01")
	// DateTo is today surrounded by whitespace: compareISODate trims and
	// returns 0 (equal), so the > 0 override must NOT fire.
	exportBuilder := &stubSettingsViewExportBuilder{
		responses: []ExportSummary{
			{TotalEntries: 1, HasData: true, DateFrom: "2026-06-01", DateTo: " 2026-06-01"},
			{TotalEntries: 1, HasData: true, DateFrom: "2026-06-01", DateTo: "2026-06-01"},
		},
	}
	svc := NewSettingsViewService(loader, exportBuilder, nil)
	user := &models.User{ID: 31, Role: models.RoleOwner}

	viewData, err := svc.BuildSettingsPageViewData(context.Background(), user, "en", SettingsViewInput{}, today, time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Original (> 0): equal-modulo-whitespace DateTo does NOT override
	// selectableMax, so the clean todayISO is emitted.
	// Mutant (>= 0): overrides with the raw " 2026-06-01" (leading space) -> fails here.
	if viewData.Export.SelectableDateMax != "2026-06-01" {
		t.Fatalf("line 282: expected clean SelectableDateMax=2026-06-01 (DateTo equals today modulo whitespace), got %q", viewData.Export.SelectableDateMax)
	}
}
