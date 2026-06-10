package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

func assertExportServiceLoadDataForRangeFiltersInclusiveBoundaries(t *testing.T, setup func(*testing.T) (*DayService, *gorm.DB), email string) {
	t.Helper()

	dayService, database := setup(t)
	user := createDayServiceTestUser(t, database, email)

	repositories := db.NewRepositories(database)
	symptomService := NewSymptomService(repositories.Symptoms)
	exportService := NewExportService(dayService, symptomService)

	logs := []models.DailyLog{
		{UserID: user.ID, Date: time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC), Flow: models.FlowNone},
		{UserID: user.ID, Date: time.Date(2026, time.February, 11, 0, 0, 0, 0, time.UTC), Flow: models.FlowLight},
		{UserID: user.ID, Date: time.Date(2026, time.February, 12, 0, 0, 0, 0, time.UTC), Flow: models.FlowMedium},
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatalf("create logs: %v", err)
	}

	from := time.Date(2026, time.February, 11, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, time.February, 11, 0, 0, 0, 0, time.UTC)

	t.Run("exact day range includes target day", func(t *testing.T) {
		filtered, _, err := exportService.LoadDataForRange(context.Background(), user.ID, &from, &to, time.UTC)
		if err != nil {
			t.Fatalf("LoadDataForRange returned error: %v", err)
		}
		if len(filtered) != 1 {
			t.Fatalf("expected exactly one entry, got %d", len(filtered))
		}
		if filtered[0].Date.Format("2006-01-02") != "2026-02-11" {
			t.Fatalf("expected date 2026-02-11, got %s", filtered[0].Date.Format("2006-01-02"))
		}
	})

	t.Run("from only includes from and after", func(t *testing.T) {
		filtered, _, err := exportService.LoadDataForRange(context.Background(), user.ID, &from, nil, time.UTC)
		if err != nil {
			t.Fatalf("LoadDataForRange returned error: %v", err)
		}
		if len(filtered) != 2 {
			t.Fatalf("expected two entries, got %d", len(filtered))
		}
		if filtered[0].Date.Format("2006-01-02") != "2026-02-11" || filtered[1].Date.Format("2006-01-02") != "2026-02-12" {
			t.Fatalf("unexpected dates: %s, %s", filtered[0].Date.Format("2006-01-02"), filtered[1].Date.Format("2006-01-02"))
		}
	})

	t.Run("to only includes up to and including day", func(t *testing.T) {
		filtered, _, err := exportService.LoadDataForRange(context.Background(), user.ID, nil, &to, time.UTC)
		if err != nil {
			t.Fatalf("LoadDataForRange returned error: %v", err)
		}
		if len(filtered) != 2 {
			t.Fatalf("expected two entries, got %d", len(filtered))
		}
		if filtered[0].Date.Format("2006-01-02") != "2026-02-10" || filtered[1].Date.Format("2006-01-02") != "2026-02-11" {
			t.Fatalf("unexpected dates: %s, %s", filtered[0].Date.Format("2006-01-02"), filtered[1].Date.Format("2006-01-02"))
		}
	})
}

func TestExportServiceLoadDataForRangeFiltersInclusiveBoundaries(t *testing.T) {
	assertExportServiceLoadDataForRangeFiltersInclusiveBoundaries(t, newDayServiceIntegration, "export-range-data-service@example.com")
}

func TestExportServiceLoadDataForRangeFiltersInclusiveBoundariesPostgres(t *testing.T) {
	assertExportServiceLoadDataForRangeFiltersInclusiveBoundaries(t, newDayServicePostgresIntegration, "export-range-data-service-postgres@example.com")
}

func TestExportServiceBuildSummaryForRangeFiltersInclusiveBoundaries(t *testing.T) {
	dayService, database := newDayServiceIntegration(t)
	user := createDayServiceTestUser(t, database, "export-range-summary-service@example.com")

	repositories := db.NewRepositories(database)
	symptomService := NewSymptomService(repositories.Symptoms)
	exportService := NewExportService(dayService, symptomService)

	logs := []models.DailyLog{
		{UserID: user.ID, Date: time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC), Flow: models.FlowNone},
		{UserID: user.ID, Date: time.Date(2026, time.February, 11, 0, 0, 0, 0, time.UTC), Flow: models.FlowLight},
		{UserID: user.ID, Date: time.Date(2026, time.February, 12, 0, 0, 0, 0, time.UTC), Flow: models.FlowMedium},
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatalf("create logs: %v", err)
	}

	from := time.Date(2026, time.February, 11, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, time.February, 11, 0, 0, 0, 0, time.UTC)

	summary, err := exportService.BuildSummary(context.Background(), user.ID, &from, &to, time.UTC)
	if err != nil {
		t.Fatalf("BuildSummary returned error: %v", err)
	}
	if summary.TotalEntries != 1 {
		t.Fatalf("expected total=1, got %d", summary.TotalEntries)
	}
	if summary.DateFrom != "2026-02-11" || summary.DateTo != "2026-02-11" {
		t.Fatalf("expected range 2026-02-11..2026-02-11, got %s..%s", summary.DateFrom, summary.DateTo)
	}
}
