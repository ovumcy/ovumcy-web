package services

import (
	"testing"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestSanitizeLogForViewerPartnerHidesPrivateFields(t *testing.T) {
	partner := &models.User{Role: models.RolePartner}
	entry := models.DailyLog{
		Mood:          4,
		SexActivity:   models.SexActivityProtected,
		BBT:           36.55,
		CervicalMucus: models.CervicalMucusEggWhite,
		Notes:         "private",
		SymptomIDs:    []uint{1, 2},
	}

	sanitized := SanitizeLogForViewer(partner, entry)
	if sanitized.Mood != 0 {
		t.Fatalf("expected mood to be hidden, got %d", sanitized.Mood)
	}
	if sanitized.SexActivity != models.SexActivityNone {
		t.Fatalf("expected sex activity to be hidden, got %q", sanitized.SexActivity)
	}
	if sanitized.BBT != 0 {
		t.Fatalf("expected BBT to be hidden, got %.2f", sanitized.BBT)
	}
	if sanitized.CervicalMucus != models.CervicalMucusNone {
		t.Fatalf("expected cervical mucus to be hidden, got %q", sanitized.CervicalMucus)
	}
	if sanitized.Notes != "" {
		t.Fatalf("expected notes to be hidden, got %q", sanitized.Notes)
	}
	if len(sanitized.SymptomIDs) != 0 {
		t.Fatalf("expected symptom IDs to be hidden, got %#v", sanitized.SymptomIDs)
	}
}

func TestSanitizeLogForViewerOwnerKeepsFields(t *testing.T) {
	owner := &models.User{Role: models.RoleOwner}
	entry := models.DailyLog{
		Mood:          4,
		SexActivity:   models.SexActivityProtected,
		BBT:           36.55,
		CervicalMucus: models.CervicalMucusEggWhite,
		Notes:         "private",
		SymptomIDs:    []uint{1, 2},
	}

	sanitized := SanitizeLogForViewer(owner, entry)
	if sanitized.Mood != entry.Mood {
		t.Fatalf("expected owner mood preserved, got %d", sanitized.Mood)
	}
	if sanitized.SexActivity != entry.SexActivity {
		t.Fatalf("expected owner sex activity preserved, got %q", sanitized.SexActivity)
	}
	if sanitized.BBT != entry.BBT {
		t.Fatalf("expected owner BBT preserved, got %.2f", sanitized.BBT)
	}
	if sanitized.CervicalMucus != entry.CervicalMucus {
		t.Fatalf("expected owner cervical mucus preserved, got %q", sanitized.CervicalMucus)
	}
	if sanitized.Notes != entry.Notes {
		t.Fatalf("expected owner notes preserved, got %q", sanitized.Notes)
	}
	if len(sanitized.SymptomIDs) != 2 {
		t.Fatalf("expected owner symptom IDs preserved, got %#v", sanitized.SymptomIDs)
	}
}

func TestSanitizeLogsForViewerPartnerHidesPrivateFieldsInAllEntries(t *testing.T) {
	partner := &models.User{Role: models.RolePartner}
	logs := []models.DailyLog{
		{Mood: 1, SexActivity: models.SexActivityProtected, BBT: 36.1, CervicalMucus: models.CervicalMucusMoist, Notes: "a", SymptomIDs: []uint{1}},
		{Mood: 5, SexActivity: models.SexActivityUnprotected, BBT: 36.8, CervicalMucus: models.CervicalMucusEggWhite, Notes: "b", SymptomIDs: []uint{2, 3}},
	}

	SanitizeLogsForViewer(partner, logs)

	for index := range logs {
		if logs[index].Mood != 0 {
			t.Fatalf("expected mood to be hidden for entry %d, got %d", index, logs[index].Mood)
		}
		if logs[index].SexActivity != models.SexActivityNone {
			t.Fatalf("expected sex activity to be hidden for entry %d, got %q", index, logs[index].SexActivity)
		}
		if logs[index].BBT != 0 {
			t.Fatalf("expected BBT to be hidden for entry %d, got %.2f", index, logs[index].BBT)
		}
		if logs[index].CervicalMucus != models.CervicalMucusNone {
			t.Fatalf("expected cervical mucus to be hidden for entry %d, got %q", index, logs[index].CervicalMucus)
		}
		if logs[index].Notes != "" {
			t.Fatalf("expected notes to be hidden for entry %d, got %q", index, logs[index].Notes)
		}
		if len(logs[index].SymptomIDs) != 0 {
			t.Fatalf("expected symptom IDs to be hidden for entry %d, got %#v", index, logs[index].SymptomIDs)
		}
	}
}

func TestShouldExposeSymptomsForViewer(t *testing.T) {
	if !ShouldExposeSymptomsForViewer(&models.User{Role: models.RoleOwner}) {
		t.Fatal("expected owner to see symptoms")
	}
	if ShouldExposeSymptomsForViewer(&models.User{Role: models.RolePartner}) {
		t.Fatal("expected partner not to see symptoms")
	}
}
