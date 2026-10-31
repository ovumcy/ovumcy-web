package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

func importDayKey(offsetDays int) string {
	return time.Now().UTC().AddDate(0, 0, offsetDays).Format("2006-01-02")
}

// A restore is a writer of days like a save, so it is held to the same
// observation bound: a new day recording a period or a pregnancy-test result
// past today plus manualCycleStartFutureDays refuses the whole file, and the
// refusal leaves nothing behind — no day, no custom symptom.
func TestImportServiceRefusesAFileRecordingAFutureObservation(t *testing.T) {
	cases := map[string]ExportJSONEntry{
		"period":         {Date: importDayKey(manualCycleStartFutureDays + 3), Period: true, Flow: models.FlowMedium, CycleFactors: []string{}},
		"pregnancy test": {Date: importDayKey(manualCycleStartFutureDays + 3), PregnancyTest: models.PregnancyTestPositive, CycleFactors: []string{}},
	}
	for name, future := range cases {
		t.Run(name, func(t *testing.T) {
			_, database := newDayServiceIntegration(t)
			symptomService := NewSymptomService(db.NewRepositories(database).Symptoms)
			user := createDayServiceTestUser(t, database, "import-future@example.com")

			raw, _ := json.Marshal(importPayload{Entries: []ExportJSONEntry{
				{Date: importDayKey(-3), Period: true, Flow: models.FlowLight, OtherSymptoms: []string{"Restored ache"}, CycleFactors: []string{}},
				future,
			}})
			_, err := newImportServiceIntegration(t, database, symptomService).ImportJSON(context.Background(), user.ID, raw, time.UTC)
			if !errors.Is(err, ErrImportFutureObservation) {
				t.Fatalf("expected ErrImportFutureObservation, got %v", err)
			}

			var days int64
			if err := database.Model(&models.DailyLog{}).Where("user_id = ?", user.ID).Count(&days).Error; err != nil {
				t.Fatalf("count days: %v", err)
			}
			if days != 0 {
				t.Fatalf("a refused restore stored %d days", days)
			}
			catalog, err := symptomService.FetchSymptoms(context.Background(), user.ID)
			if err != nil {
				t.Fatalf("FetchSymptoms: %v", err)
			}
			for _, symptom := range catalog {
				if !symptom.IsBuiltin {
					t.Fatalf("a refused restore left custom symptom %q in the catalogue", symptom.Name)
				}
			}
		})
	}
}

// The bound is the save's, not a stricter one: a period on the last day it
// allows restores, and a day the owner already holds past it is skipped, as a
// save re-stating it goes through — a backup restored over its own rows still
// restores.
func TestImportServiceKeepsTheSaveBoundForObservations(t *testing.T) {
	_, database := newDayServiceIntegration(t)
	symptomService := NewSymptomService(db.NewRepositories(database).Symptoms)
	user := createDayServiceTestUser(t, database, "import-bound@example.com")

	storedDay, err := time.Parse("2006-01-02", importDayKey(manualCycleStartFutureDays+3))
	if err != nil {
		t.Fatalf("parse stored day: %v", err)
	}
	if err := database.Create(&models.DailyLog{UserID: user.ID, Date: storedDay, IsPeriod: true, Flow: models.FlowLight, CycleFactorKeys: []string{}}).Error; err != nil {
		t.Fatalf("seed stored future day: %v", err)
	}

	raw, _ := json.Marshal(importPayload{Entries: []ExportJSONEntry{
		{Date: importDayKey(manualCycleStartFutureDays), Period: true, Flow: models.FlowLight, CycleFactors: []string{}},
		{Date: importDayKey(manualCycleStartFutureDays + 3), Period: true, Flow: models.FlowLight, CycleFactors: []string{}},
	}})
	result, err := newImportServiceIntegration(t, database, symptomService).ImportJSON(context.Background(), user.ID, raw, time.UTC)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Added != 1 || result.Skipped != 1 || result.Rejected != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
}
