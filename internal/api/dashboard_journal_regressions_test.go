package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
	"golang.org/x/net/html"
)

func TestDashboardSymptomsNotesPanelUsesSavedSymptomsAndNotesState(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "dashboard-journal@example.com", "StrongPass1", true)

	symptoms := []models.SymptomType{
		{UserID: user.ID, Name: "Custom cramps", Icon: "A", Color: "#FF7755"},
		{UserID: user.ID, Name: "Custom headache", Icon: "B", Color: "#55AAFF"},
	}
	if err := database.Create(&symptoms).Error; err != nil {
		t.Fatalf("create symptoms: %v", err)
	}

	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	logEntry := models.DailyLog{
		UserID:     user.ID,
		Date:       today,
		IsPeriod:   false,
		Flow:       models.FlowNone,
		SymptomIDs: []uint{symptoms[0].ID, symptoms[1].ID},
		Notes:      "Remember to hydrate",
	}
	if err := database.Create(&logEntry).Error; err != nil {
		t.Fatalf("create daily log: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("dashboard request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read dashboard body: %v", err)
	}
	document := mustParseHTMLDocument(t, string(body))
	documentText := htmlDocumentText(document)
	if !strings.Contains(documentText, "Remember to hydrate") {
		t.Fatalf("expected saved note to stay visible in dashboard form")
	}
	disclosure := htmlElementByTagAndClass(document, "details", "note-disclosure")
	if disclosure == nil {
		t.Fatalf("expected saved notes to render inside a disclosure block")
	}
	if !htmlHasAttr(disclosure, "open") {
		t.Fatalf("expected saved dashboard note disclosure to stay open")
	}
	summary := htmlFindElement(disclosure, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "summary"
	})
	if !strings.Contains(htmlDocumentText(summary), "Hide note") {
		t.Fatalf("expected saved dashboard note disclosure to use Hide note copy")
	}
	noteField := htmlElementByID(document, "today-notes")
	if noteField == nil {
		t.Fatalf("expected dashboard notes textarea")
	}
	if got := htmlDocumentText(noteField); got != "Remember to hydrate" {
		t.Fatalf("expected saved note textarea value, got %q", got)
	}
	if !strings.Contains(documentText, "Custom cramps") {
		t.Fatalf("expected saved custom symptom label to be rendered in dashboard picker")
	}
	if !strings.Contains(documentText, "Custom headache") {
		t.Fatalf("expected second saved custom symptom label to be rendered in dashboard picker")
	}
}

func TestDashboardEmptyNotesUseAddNoteDisclosure(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "dashboard-empty-note@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("dashboard request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	disclosure := htmlElementByTagAndClass(document, "details", "note-disclosure")
	if disclosure == nil {
		t.Fatalf("expected dashboard note field to render as a disclosure")
	}
	summary := htmlFindElement(disclosure, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "summary"
	})
	if !strings.Contains(htmlDocumentText(summary), "Add note") {
		t.Fatalf("expected empty dashboard note disclosure to use Add note copy")
	}
	if htmlHasAttr(disclosure, "open") {
		t.Fatalf("expected empty dashboard note disclosure to stay closed")
	}
}
