package api

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/db"
	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestFetchSymptomsBackfillsMissingBuiltinSymptoms(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "ovumcy-symptom-backfill.db")
	database, err := db.OpenSQLite(databasePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	user := models.User{
		Email:               "symptoms@example.com",
		PasswordHash:        "hash",
		Role:                models.RoleOwner,
		OnboardingCompleted: true,
		CycleLength:         28,
		PeriodLength:        5,
		CreatedAt:           time.Now().UTC(),
	}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

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

	handler := &Handler{db: database}
	handler.ensureDependencies()
	symptoms, err := handler.symptomService.FetchSymptoms(user.ID)
	if err != nil {
		t.Fatalf("fetch symptoms: %v", err)
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
