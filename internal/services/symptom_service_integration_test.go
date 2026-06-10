package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

func TestSymptomServiceFetchSymptomsBackfillsMissingBuiltinSymptoms(t *testing.T) {
	_, database := newDayServiceIntegration(t)
	user := createDayServiceTestUser(t, database, "symptoms-service@example.com")

	oldBuiltin := models.DefaultBuiltinSymptoms()[:7]
	records := make([]models.SymptomType, 0, len(oldBuiltin))
	for _, symptom := range oldBuiltin {
		records = append(records, models.SymptomType{
			UserID:    user.ID,
			Name:      symptom.Name,
			Icon:      symptom.Icon,
			Color:     symptom.Color,
			IsBuiltin: true,
		})
	}
	if err := database.Create(&records).Error; err != nil {
		t.Fatalf("seed old builtin symptoms: %v", err)
	}

	repositories := db.NewRepositories(database)
	service := NewSymptomService(repositories.Symptoms)

	symptoms, err := service.FetchSymptoms(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("FetchSymptoms returned error: %v", err)
	}

	expected := models.DefaultBuiltinSymptoms()
	if len(symptoms) != len(expected) {
		t.Fatalf("expected %d symptoms after backfill, got %d", len(expected), len(symptoms))
	}
	for index, symptom := range expected {
		if symptoms[index].Name != symptom.Name {
			t.Fatalf("expected symptom %q at index %d, got %q", symptom.Name, index, symptoms[index].Name)
		}
		if !symptoms[index].IsBuiltin {
			t.Fatalf("expected symptom %q to be builtin", symptom.Name)
		}
	}
}

func TestSymptomServiceArchiveKeepsHistoryAndHidesFromPicker(t *testing.T) {
	_, database := newDayServiceIntegration(t)
	user := createDayServiceTestUser(t, database, "symptoms-archive@example.com")
	customSymptom := createSymptomServiceTestCustomSymptom(t, database, user.ID, "Joint stiffness", "J", "#334455")
	logEntry := createSymptomServiceTestLogEntry(t, database, user.ID, time.Date(2026, time.March, 7, 0, 0, 0, 0, time.UTC), customSymptom.ID)

	repositories := db.NewRepositories(database)
	service := NewSymptomService(repositories.Symptoms)

	if err := service.ArchiveSymptomForUser(context.Background(), user.ID, customSymptom.ID, time.Date(2026, time.March, 8, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("archive symptom: %v", err)
	}

	allSymptoms, err := service.FetchSymptoms(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("FetchSymptoms returned error: %v", err)
	}
	assertSymptomServiceArchivedSymptomPresent(t, allSymptoms, customSymptom.ID)

	pickerSymptoms, err := service.FetchPickerSymptoms(context.Background(), user.ID, nil)
	if err != nil {
		t.Fatalf("FetchPickerSymptoms returned error: %v", err)
	}
	assertSymptomServicePickerOmitsSymptom(t, pickerSymptoms, customSymptom.ID)

	selectedPickerSymptoms, err := service.FetchPickerSymptoms(context.Background(), user.ID, []uint{customSymptom.ID})
	if err != nil {
		t.Fatalf("FetchPickerSymptoms(selected) returned error: %v", err)
	}
	assertSymptomServicePickerIncludesSymptom(t, selectedPickerSymptoms, customSymptom.ID)
	assertSymptomServiceLogHistoryPreserved(t, database, logEntry.ID, customSymptom.ID)
}

func createSymptomServiceTestCustomSymptom(t *testing.T, database *gorm.DB, userID uint, name string, icon string, color string) models.SymptomType {
	t.Helper()

	customSymptom := models.SymptomType{
		UserID: userID,
		Name:   name,
		Icon:   icon,
		Color:  color,
	}
	if err := database.Create(&customSymptom).Error; err != nil {
		t.Fatalf("create custom symptom: %v", err)
	}
	return customSymptom
}

func createSymptomServiceTestLogEntry(t *testing.T, database *gorm.DB, userID uint, date time.Time, symptomID uint) models.DailyLog {
	t.Helper()

	logEntry := models.DailyLog{
		UserID:     userID,
		Date:       date,
		SymptomIDs: []uint{symptomID},
	}
	if err := database.Create(&logEntry).Error; err != nil {
		t.Fatalf("create daily log: %v", err)
	}
	return logEntry
}

func assertSymptomServiceArchivedSymptomPresent(t *testing.T, symptoms []models.SymptomType, symptomID uint) {
	t.Helper()

	for _, symptom := range symptoms {
		if symptom.ID != symptomID {
			continue
		}
		if symptom.IsActive() {
			t.Fatalf("expected archived symptom to become inactive")
		}
		return
	}

	t.Fatalf("expected archived custom symptom to remain queryable")
}

func assertSymptomServicePickerOmitsSymptom(t *testing.T, symptoms []models.SymptomType, symptomID uint) {
	t.Helper()

	for _, symptom := range symptoms {
		if symptom.ID == symptomID {
			t.Fatalf("did not expect archived custom symptom in empty picker")
		}
	}
}

func assertSymptomServicePickerIncludesSymptom(t *testing.T, symptoms []models.SymptomType, symptomID uint) {
	t.Helper()

	for _, symptom := range symptoms {
		if symptom.ID == symptomID {
			return
		}
	}

	t.Fatalf("expected selected archived symptom to remain available for existing logs")
}

func assertSymptomServiceLogHistoryPreserved(t *testing.T, database *gorm.DB, logID uint, symptomID uint) {
	t.Helper()

	updatedLog := models.DailyLog{}
	if err := database.First(&updatedLog, logID).Error; err != nil {
		t.Fatalf("load daily log after archive: %v", err)
	}
	if len(updatedLog.SymptomIDs) != 1 || updatedLog.SymptomIDs[0] != symptomID {
		t.Fatalf("expected archived symptom to remain in history, got %#v", updatedLog.SymptomIDs)
	}
}
