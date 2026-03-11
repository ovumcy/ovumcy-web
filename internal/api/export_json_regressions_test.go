package api

import (
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestExportJSONNormalizesFlowAndMapsSymptoms(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "export-json@example.com", "StrongPass1", true)

	symptoms := []models.SymptomType{
		{UserID: user.ID, Name: "Mood swings", Icon: "A", Color: "#111111"},
		{UserID: user.ID, Name: "My Custom", Icon: "B", Color: "#222222"},
	}
	if err := database.Create(&symptoms).Error; err != nil {
		t.Fatalf("create symptoms: %v", err)
	}

	logEntry := models.DailyLog{
		UserID:        user.ID,
		Date:          time.Date(2026, time.February, 19, 0, 0, 0, 0, time.UTC),
		IsPeriod:      false,
		Flow:          "unexpected-flow",
		Mood:          4,
		SexActivity:   models.SexActivityProtected,
		BBT:           36.55,
		CervicalMucus: models.CervicalMucusEggWhite,
		SymptomIDs:    []uint{symptoms[0].ID, symptoms[1].ID},
		Notes:         "json-note",
	}
	if err := database.Create(&logEntry).Error; err != nil {
		t.Fatalf("create daily log: %v", err)
	}

	response := exportResponseForTest(t, app, user.Email, "StrongPass1", "/api/export/json")
	assertBodyContainsAll(t, response.Header.Get("Content-Type"),
		bodyStringMatch{fragment: "application/json", message: "expected application/json content type"},
	)
	assertBodyContainsAll(t, response.Header.Get("Content-Disposition"),
		bodyStringMatch{fragment: "attachment; filename=ovumcy-export-", message: "expected attachment filename header"},
	)

	payload := decodeExportJSONPayload(t, response.Body)
	assertExportJSONPayload(t, payload)
}

func decodeExportJSONPayload(t *testing.T, body io.Reader) struct {
	ExportedAt string            `json:"exported_at"`
	Entries    []exportJSONEntry `json:"entries"`
} {
	t.Helper()

	payload := struct {
		ExportedAt string            `json:"exported_at"`
		Entries    []exportJSONEntry `json:"entries"`
	}{}
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		t.Fatalf("decode json payload: %v", err)
	}
	return payload
}

func assertExportJSONPayload(t *testing.T, payload struct {
	ExportedAt string            `json:"exported_at"`
	Entries    []exportJSONEntry `json:"entries"`
}) {
	t.Helper()

	if payload.ExportedAt == "" {
		t.Fatalf("expected exported_at in payload")
	}
	if _, err := time.Parse(time.RFC3339, payload.ExportedAt); err != nil {
		t.Fatalf("expected RFC3339 exported_at, got %q", payload.ExportedAt)
	}
	if len(payload.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(payload.Entries))
	}

	entry := payload.Entries[0]
	if entry.Flow != models.FlowNone {
		t.Fatalf("expected unknown flow normalized to %q, got %q", models.FlowNone, entry.Flow)
	}
	if entry.MoodRating != 4 {
		t.Fatalf("expected mood rating 4, got %d", entry.MoodRating)
	}
	if entry.SexActivity != models.SexActivityProtected {
		t.Fatalf("expected protected sex activity, got %q", entry.SexActivity)
	}
	if entry.BBT != 36.55 {
		t.Fatalf("expected BBT 36.55, got %.2f", entry.BBT)
	}
	if entry.CervicalMucus != models.CervicalMucusEggWhite {
		t.Fatalf("expected eggwhite cervical mucus, got %q", entry.CervicalMucus)
	}
	if !entry.Symptoms.Mood {
		t.Fatalf("expected mood flag to be true")
	}
	if len(entry.OtherSymptoms) != 1 || entry.OtherSymptoms[0] != "My Custom" {
		t.Fatalf("expected custom symptom in other list, got %#v", entry.OtherSymptoms)
	}
	if entry.Notes != "json-note" {
		t.Fatalf("expected notes to be preserved, got %q", entry.Notes)
	}
}
