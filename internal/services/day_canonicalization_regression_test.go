package services

import (
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestDayServiceUpsertCanonicalizesDateToUTCMidnight is the regression lock
// for issue #49. After the BeforeSave hook + DayRange fix, every newly
// written DailyLog.Date must persist as UTC-midnight on disk regardless of
// the request location, and the same round-trip via FetchLogByDate must
// find the row from the local-calendar-day perspective. Without the
// DayRange UTC bounds, the FetchLogByDate query in UTC-minus zones drifts
// past the row and returns no match (the failure mode flagged in the
// issue: 8 tests broke when only the BeforeSave hook landed).
func TestDayServiceUpsertCanonicalizesDateToUTCMidnight(t *testing.T) {
	toronto, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Skipf("zoneinfo for America/Toronto unavailable: %v", err)
	}
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Skipf("zoneinfo for Asia/Tokyo unavailable: %v", err)
	}

	tests := []struct {
		name     string
		location *time.Location
		email    string
	}{
		{name: "America/Toronto UTC-5", location: toronto, email: "canonicalize-toronto@example.com"},
		{name: "Asia/Tokyo UTC+9", location: tokyo, email: "canonicalize-tokyo@example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, database := newDayServiceIntegration(t)
			user := createDayServiceTestUser(t, database, tt.email)

			localCalendarDay := time.Date(2026, time.February, 10, 0, 0, 0, 0, tt.location)
			now := localCalendarDay.Add(8 * time.Hour)

			if _, err := service.UpsertDayEntryWithAutoFillAt(user.ID, localCalendarDay, DayEntryInput{
				IsPeriod:      true,
				Flow:          models.FlowMedium,
				Mood:          0,
				SexActivity:   models.SexActivityNone,
				CervicalMucus: models.CervicalMucusNone,
			}, now, tt.location); err != nil {
				t.Fatalf("UpsertDayEntryWithAutoFillAt: %v", err)
			}

			var rawDate string
			if err := database.Raw("SELECT date FROM daily_logs WHERE user_id = ? ORDER BY date ASC LIMIT 1", user.ID).Row().Scan(&rawDate); err != nil {
				t.Fatalf("raw SELECT date: %v", err)
			}
			if !strings.HasPrefix(rawDate, "2026-02-10") {
				t.Fatalf("expected on-disk date to begin with local calendar day 2026-02-10, got %q", rawDate)
			}
			if strings.Contains(rawDate, "+") || strings.Contains(rawDate, "-05:") || strings.Contains(rawDate, "+09:") {
				if !strings.Contains(rawDate, "+00:00") && !strings.Contains(rawDate, "Z") {
					t.Fatalf("expected on-disk offset to be UTC (+00:00 or Z), got %q", rawDate)
				}
			}

			entry, err := service.FetchLogByDate(user.ID, localCalendarDay, tt.location)
			if err != nil {
				t.Fatalf("FetchLogByDate after canonicalized write: %v", err)
			}
			if !entry.IsPeriod {
				t.Fatalf("expected upserted period entry to be found via local-day round-trip, got is_period=false (DayRange bounds drifted past UTC-midnight row)")
			}
			if entry.Flow != models.FlowMedium {
				t.Fatalf("expected flow %q, got %q", models.FlowMedium, entry.Flow)
			}
			if entry.Date.Format("2006-01-02") != "2026-02-10" {
				t.Fatalf("expected loaded entry to carry calendar day 2026-02-10, got %s", entry.Date.Format("2006-01-02"))
			}
			if entry.Date.Hour() != 0 || entry.Date.Minute() != 0 {
				t.Fatalf("expected loaded entry at midnight, got %s", entry.Date.Format(time.RFC3339Nano))
			}
		})
	}
}
