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

// TestExportServiceScopesRangeExportToRequestingOwner pins the owner-isolation
// invariant on the export read path against a database that holds two independent
// owners (household self-hosting). Owner B is created first, so B holds the lowest
// user id: an export that read a constant owner instead of the requesting one
// returns B's days and B's symptom catalog here, and every assertion below fails.
// The boundary itself is docs/SECURITY_INVARIANTS.md — no surface may expose
// another account's data.
func TestExportServiceScopesRangeExportToRequestingOwner(t *testing.T) {
	dayService, database := newDayServiceIntegration(t)
	repositories := db.NewRepositories(database)
	symptomService := NewSymptomService(repositories.Symptoms)
	exportService := NewExportService(dayService, symptomService)

	ownerB := createDayServiceTestUser(t, database, "export-idor-b@example.com")
	bSymptom, err := symptomService.CreateSymptomForUser(context.Background(), ownerB.ID, "Owner B Only", "", "")
	if err != nil {
		t.Fatalf("seed owner B symptom: %v", err)
	}
	ownerA := createDayServiceTestUser(t, database, "export-idor-a@example.com")
	aSymptom, err := symptomService.CreateSymptomForUser(context.Background(), ownerA.ID, "Owner A Only", "", "")
	if err != nil {
		t.Fatalf("seed owner A symptom: %v", err)
	}

	logs := []models.DailyLog{
		{
			UserID:     ownerB.ID,
			Date:       time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC),
			Flow:       models.FlowHeavy,
			SymptomIDs: []uint{bSymptom.ID},
			Notes:      "owner B note",
		},
		{
			UserID:     ownerA.ID,
			Date:       time.Date(2026, time.February, 12, 0, 0, 0, 0, time.UTC),
			Flow:       models.FlowLight,
			SymptomIDs: []uint{aSymptom.ID},
			Notes:      "owner A note",
		},
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatalf("create logs: %v", err)
	}

	from := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, time.February, 28, 0, 0, 0, 0, time.UTC)

	exported, symptomNames, err := exportService.LoadDataForRange(context.Background(), ownerA.ID, &from, &to, time.UTC)
	if err != nil {
		t.Fatalf("LoadDataForRange for owner A: %v", err)
	}
	// Positive anchor first: A's own day must be in the export, so an export that
	// returned nothing at all cannot pass the isolation assertions vacuously.
	if len(exported) != 1 {
		t.Fatalf("expected owner A's single day, got %d rows", len(exported))
	}
	if exported[0].UserID != ownerA.ID {
		t.Fatalf("cross-owner leak: exported day belongs to owner %d, not requesting owner A (%d)", exported[0].UserID, ownerA.ID)
	}
	if exported[0].Notes != "owner A note" {
		t.Fatalf("expected owner A's own day, got notes %q", exported[0].Notes)
	}

	// And no trace of owner B: neither B's rows nor B's symptom names.
	for _, logEntry := range exported {
		if logEntry.UserID == ownerB.ID || logEntry.Notes == "owner B note" {
			t.Fatalf("cross-owner leak: owner B's day %s reached owner A's export", CalendarDayKey(logEntry.Date))
		}
	}
	if _, leaked := symptomNames[bSymptom.ID]; leaked {
		t.Fatalf("cross-owner leak: owner B's symptom id %d reached owner A's export", bSymptom.ID)
	}
	for symptomID, name := range symptomNames {
		if name == "Owner B Only" {
			t.Fatalf("cross-owner leak: owner B's symptom name reached owner A's export as id %d", symptomID)
		}
	}
	if symptomNames[aSymptom.ID] != "Owner A Only" {
		t.Fatalf("expected owner A's own symptom name in the export, got %q", symptomNames[aSymptom.ID])
	}

	// The summary is a separate read of the day repository, so it carries the owner
	// on its own: B's earlier day would widen the range even where the row list is
	// scoped correctly.
	summary, err := exportService.BuildSummary(context.Background(), ownerA.ID, &from, &to, time.UTC)
	if err != nil {
		t.Fatalf("BuildSummary for owner A: %v", err)
	}
	if summary.TotalEntries != 1 {
		t.Fatalf("expected owner A's single entry in the summary, got %d", summary.TotalEntries)
	}
	if summary.DateFrom != "2026-02-12" || summary.DateTo != "2026-02-12" {
		t.Fatalf("expected summary range 2026-02-12..2026-02-12, got %s..%s", summary.DateFrom, summary.DateTo)
	}

	// Owner B still exports B's own day: the scoping is a filter on the requesting
	// owner, not an export path that dropped the second account's data entirely.
	exportedB, symptomNamesB, err := exportService.LoadDataForRange(context.Background(), ownerB.ID, &from, &to, time.UTC)
	if err != nil {
		t.Fatalf("LoadDataForRange for owner B: %v", err)
	}
	if len(exportedB) != 1 || exportedB[0].UserID != ownerB.ID {
		t.Fatalf("expected owner B's own single day, got %#v", exportedB)
	}
	if symptomNamesB[bSymptom.ID] != "Owner B Only" {
		t.Fatalf("expected owner B's own symptom name in B's export, got %q", symptomNamesB[bSymptom.ID])
	}
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
