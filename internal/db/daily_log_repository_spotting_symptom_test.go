package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The cycle-boundary rule reads a day whose only bleeding signal is the
// Spotting symptom as spotting, so the flag it reads must be set on every read
// that feeds it: only for the OWNER's built-in Spotting row, never for another
// symptom of the owner's or another owner's built-in Spotting.
func TestDailyLogReadsMarkTheOwnersBuiltinSpottingSymptom(t *testing.T) {
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "daily-spotting-mark.db"))
	owner := createDailyLogTestUser(t, database, "spotting-mark-owner@example.com")
	other := createDailyLogTestUser(t, database, "spotting-mark-other@example.com")
	repo := NewDailyLogRepository(database)
	ctx := context.Background()

	symptoms := []models.SymptomType{
		{UserID: owner, Name: "Spotting", Icon: "S", Color: "#111111", IsBuiltin: true},
		{UserID: owner, Name: "Light bleeding", Icon: "C", Color: "#222222"},
		{UserID: other, Name: "Spotting", Icon: "S", Color: "#333333", IsBuiltin: true},
		{UserID: owner, Name: "Cramps", Icon: "K", Color: "#444444", IsBuiltin: true},
	}
	requireNoErr(t, database.Create(&symptoms).Error, "seed symptoms")
	builtin, custom, foreign, cramps := symptoms[0].ID, symptoms[1].ID, symptoms[2].ID, symptoms[3].ID

	day := func(dayOfMonth int) time.Time {
		return time.Date(2026, time.June, dayOfMonth, 0, 0, 0, 0, time.UTC)
	}
	logs := []models.DailyLog{
		{UserID: owner, Date: day(1), IsPeriod: true, SymptomIDs: []uint{cramps, builtin}},
		{UserID: owner, Date: day(2), IsPeriod: true, SymptomIDs: []uint{custom}},
		{UserID: owner, Date: day(3), IsPeriod: true, SymptomIDs: []uint{foreign}},
		{UserID: owner, Date: day(4), IsPeriod: true},
	}
	requireNoErr(t, database.Create(&logs).Error, "seed daily logs")

	want := map[string]bool{"2026-06-01": true, "2026-06-02": false, "2026-06-03": false, "2026-06-04": false}
	assertMarks := func(read string, got []models.DailyLog) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s returned %d days, want %d", read, len(got), len(want))
		}
		for _, entry := range got {
			key := entry.Date.UTC().Format("2006-01-02")
			if entry.HasSpottingSymptom != want[key] {
				t.Fatalf("%s: %s HasSpottingSymptom = %v, want %v", read, key, entry.HasSpottingSymptom, want[key])
			}
		}
	}

	all, err := repo.ListByUser(ctx, owner)
	requireNoErr(t, err, "list by user")
	assertMarks("ListByUser", all)

	from, to := day(1), day(5)
	ranged, err := repo.ListByUserRange(ctx, owner, &from, &to)
	requireNoErr(t, err, "list by user range")
	assertMarks("ListByUserRange", ranged)
}

// A failed Spotting lookup is a failed read, not a set of days silently read as
// bleeding: both reads that feed the boundary rule surface the error.
func TestDailyLogReadsSurfaceASpottingLookupFailure(t *testing.T) {
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "daily-spotting-failure.db"))
	owner := createDailyLogTestUser(t, database, "spotting-failure-owner@example.com")
	repo := NewDailyLogRepository(database)
	ctx := context.Background()

	entry := models.DailyLog{UserID: owner, Date: time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC), IsPeriod: true, SymptomIDs: []uint{7}}
	requireNoErr(t, database.Create(&entry).Error, "seed daily log")
	requireNoErr(t, database.Migrator().DropTable(&models.SymptomType{}), "drop symptom_types")

	if _, err := repo.ListByUser(ctx, owner); err == nil {
		t.Fatal("ListByUser: expected the spotting lookup failure, got nil")
	}
	if _, err := repo.ListByUserRange(ctx, owner, nil, nil); err == nil {
		t.Fatal("ListByUserRange: expected the spotting lookup failure, got nil")
	}
}
