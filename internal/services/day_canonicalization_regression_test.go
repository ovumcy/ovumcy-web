package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
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
	runDayServiceUpsertCanonicalizesDateToUTCMidnight(t, newDayServiceIntegration, "canonicalize")
}

// TestDayServiceUpsertCanonicalizesDateToUTCMidnightPostgres is the Postgres
// dialect-parity counterpart. The DATE column type, GORM bindings, and
// driver-level timezone handling differ from SQLite; this lane exercises the
// same canonicalization invariant on the supported advanced deployment path.
func TestDayServiceUpsertCanonicalizesDateToUTCMidnightPostgres(t *testing.T) {
	runDayServiceUpsertCanonicalizesDateToUTCMidnight(t, newDayServicePostgresIntegration, "canonicalize-postgres")
}

func runDayServiceUpsertCanonicalizesDateToUTCMidnight(t *testing.T, setup func(*testing.T) (*DayService, *gorm.DB), emailPrefix string) {
	t.Helper()

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
		{name: "America/Toronto UTC-5", location: toronto, email: emailPrefix + "-toronto@example.com"},
		{name: "Asia/Tokyo UTC+9", location: tokyo, email: emailPrefix + "-tokyo@example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, database := setup(t)
			user := createDayServiceTestUser(t, database, tt.email)

			localCalendarDay := time.Date(2026, time.February, 10, 0, 0, 0, 0, tt.location)
			now := localCalendarDay.Add(8 * time.Hour)

			if _, err := service.UpsertDayEntryWithAutoFillAt(context.Background(), user.ID, localCalendarDay, DayEntryInput{
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
			assertUTCMidnightRawDate(t, rawDate, "2026-02-10")

			entry, err := service.FetchLogByDate(context.Background(), user.ID, localCalendarDay, tt.location)
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

// TestDayServiceConsecutiveUpsertsUpdateSameRowAcrossTimezones is the
// regression lock for issue #64. The first PUT /api/v1/days/{date} writes a
// row at UTC-midnight; the second PUT for the same calendar day must locate
// that row and update it rather than attempt to INSERT a duplicate. In
// UTC-minus zones the inner UpsertDayEntry was applying DayRange a second
// time to a value that was already UTC-midnight, which projected the lookup
// window one day backward and missed the existing row — the subsequent
// Create then collided with the uidx_user_date unique index.
func TestDayServiceConsecutiveUpsertsUpdateSameRowAcrossTimezones(t *testing.T) {
	runDayServiceConsecutiveUpsertsUpdateSameRowAcrossTimezones(t, newDayServiceIntegration, "consecutive-upsert")
}

// TestDayServiceConsecutiveUpsertsUpdateSameRowAcrossTimezonesPostgres is the
// Postgres dialect-parity counterpart. The uidx_user_date unique constraint
// behaves identically on both engines, but Postgres exercises a different
// DATE binding path and is the supported advanced deployment lane, so the
// regression must hold there too.
func TestDayServiceConsecutiveUpsertsUpdateSameRowAcrossTimezonesPostgres(t *testing.T) {
	runDayServiceConsecutiveUpsertsUpdateSameRowAcrossTimezones(t, newDayServicePostgresIntegration, "consecutive-upsert-postgres")
}

func runDayServiceConsecutiveUpsertsUpdateSameRowAcrossTimezones(t *testing.T, setup func(*testing.T) (*DayService, *gorm.DB), emailPrefix string) {
	t.Helper()

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
		{name: "America/Toronto UTC-5", location: toronto, email: emailPrefix + "-toronto@example.com"},
		{name: "Asia/Tokyo UTC+9", location: tokyo, email: emailPrefix + "-tokyo@example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, database := setup(t)
			user := createDayServiceTestUser(t, database, tt.email)

			localCalendarDay := time.Date(2026, time.February, 10, 0, 0, 0, 0, tt.location)
			now := localCalendarDay.Add(8 * time.Hour)

			if _, err := service.UpsertDayEntryWithAutoFillAt(context.Background(), user.ID, localCalendarDay, DayEntryInput{
				IsPeriod:      true,
				Flow:          models.FlowSpotting,
				Mood:          0,
				SexActivity:   models.SexActivityNone,
				CervicalMucus: models.CervicalMucusNone,
			}, now, tt.location); err != nil {
				t.Fatalf("first UpsertDayEntryWithAutoFillAt: %v", err)
			}

			if _, err := service.UpsertDayEntryWithAutoFillAt(context.Background(), user.ID, localCalendarDay, DayEntryInput{
				IsPeriod:      true,
				Flow:          models.FlowSpotting,
				Mood:          0,
				SexActivity:   models.SexActivityNone,
				CervicalMucus: models.CervicalMucusNone,
				SymptomIDs:    []uint{1, 2},
			}, now, tt.location); err != nil {
				t.Fatalf("second UpsertDayEntryWithAutoFillAt for same local day: %v", err)
			}

			var rowCount int64
			if err := database.Model(&models.DailyLog{}).Where("user_id = ?", user.ID).Count(&rowCount).Error; err != nil {
				t.Fatalf("count daily_logs rows: %v", err)
			}
			if rowCount != 1 {
				t.Fatalf("expected exactly one row after two upserts for the same local day, got %d", rowCount)
			}

			entry, err := service.FetchLogByDate(context.Background(), user.ID, localCalendarDay, tt.location)
			if err != nil {
				t.Fatalf("FetchLogByDate after second upsert: %v", err)
			}
			if len(entry.SymptomIDs) != 2 {
				t.Fatalf("expected second upsert to persist 2 symptoms, got %d (UpsertDayEntry lookup window may be drifting past the canonical row)", len(entry.SymptomIDs))
			}
		})
	}
}

func assertUTCMidnightRawDate(t *testing.T, rawDate, expectedPrefix string) {
	t.Helper()
	if !strings.HasPrefix(rawDate, expectedPrefix) {
		t.Fatalf("expected on-disk date to begin with local calendar day %s, got %q", expectedPrefix, rawDate)
	}
	hasNonUTCOffset := strings.Contains(rawDate, "+") || strings.Contains(rawDate, "-05:") || strings.Contains(rawDate, "+09:")
	isUTC := strings.Contains(rawDate, "+00:00") || strings.Contains(rawDate, "Z")
	if hasNonUTCOffset && !isUTC {
		t.Fatalf("expected on-disk offset to be UTC (+00:00 or Z), got %q", rawDate)
	}
}
