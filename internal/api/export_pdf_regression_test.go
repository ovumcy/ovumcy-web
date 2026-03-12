package api

import (
	"bytes"
	"net/http"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestExportPDFReturnsAttachmentWithPDFPayload(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "export-pdf@example.com", "StrongPass1", true)

	logs := []models.DailyLog{
		{UserID: user.ID, Date: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), IsPeriod: true, Flow: models.FlowLight},
		{UserID: user.ID, Date: time.Date(2026, time.January, 14, 0, 0, 0, 0, time.UTC), Mood: 5, SexActivity: models.SexActivityProtected, BBT: 36.55, CervicalMucus: models.CervicalMucusEggWhite, Notes: "русская заметка"},
		{UserID: user.ID, Date: time.Date(2026, time.January, 29, 0, 0, 0, 0, time.UTC), IsPeriod: true, Flow: models.FlowMedium},
		{UserID: user.ID, Date: time.Date(2026, time.February, 26, 0, 0, 0, 0, time.UTC), IsPeriod: true, Flow: models.FlowHeavy},
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatalf("create daily logs: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	request := newExportRequestForTest(t, "/api/export/pdf", authCookie)
	request.Header.Set("Accept-Language", "ru")

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	assertBodyContainsAll(t, response.Header.Get("Content-Type"),
		bodyStringMatch{fragment: "application/pdf", message: "expected application/pdf content type"},
	)
	assertBodyContainsAll(t, response.Header.Get("Content-Disposition"),
		bodyStringMatch{fragment: "attachment; filename=ovumcy-export-", message: "expected attachment filename header"},
	)

	body := []byte(mustReadBodyString(t, response.Body))
	if len(body) < 4 {
		t.Fatalf("expected pdf payload to contain at least 4 bytes, got %d", len(body))
	}
	if !bytes.HasPrefix(body, []byte("%PDF")) {
		t.Fatalf("expected PDF magic bytes, got %q", string(body[:4]))
	}
	if len(body) < 512 {
		t.Fatalf("expected non-trivial pdf body, got %d bytes", len(body))
	}
	for _, marker := range [][]byte{[]byte("Identity-H"), []byte("ToUnicode"), []byte("CIDFontType2")} {
		if !bytes.Contains(body, marker) {
			t.Fatalf("expected PDF payload to include Unicode font marker %q", marker)
		}
	}
}
