package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
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
		UserID:          user.ID,
		Date:            today,
		IsPeriod:        false,
		Flow:            models.FlowNone,
		CycleFactorKeys: []string{models.CycleFactorStress, models.CycleFactorTravel},
		SymptomIDs:      []uint{symptoms[0].ID, symptoms[1].ID},
		Notes:           "Remember to hydrate",
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
	assertDashboardSavedNoteDisclosure(t, document)
	assertDashboardSavedLabels(t, documentText, "saved custom symptom", "Custom cramps", "Custom headache")
	assertDashboardSavedLabels(t, documentText, "saved cycle factor", "Stress", "Travel")
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

func TestDashboardShowsCurrentUsageGoalSummaryForOwner(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "dashboard-usage-goal@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("usage_goal", models.UsageGoalTrying).Error; err != nil {
		t.Fatalf("update usage goal: %v", err)
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

	rendered := mustReadBodyString(t, response.Body)
	assertBodyContainsAll(t, rendered,
		bodyStringMatch{fragment: `data-usage-goal-summary`, message: "expected dashboard usage-goal summary panel"},
		bodyStringMatch{fragment: "Current mode: Trying to conceive", message: "expected current usage-goal label on dashboard"},
		bodyStringMatch{fragment: "Fertile indicators are framed around timing opportunities", message: "expected usage-goal summary copy on dashboard"},
		bodyStringMatch{fragment: "Calculations stay the same.", message: "expected dashboard usage-goal disclaimer"},
	)
}

func assertDashboardSavedNoteDisclosure(t *testing.T, document *html.Node) {
	t.Helper()

	if !strings.Contains(htmlDocumentText(document), "Remember to hydrate") {
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
}

func assertDashboardSavedLabels(t *testing.T, documentText string, labelType string, expected ...string) {
	t.Helper()

	for _, fragment := range expected {
		if !strings.Contains(documentText, fragment) {
			t.Fatalf("expected %s label %q to be rendered in dashboard picker", labelType, fragment)
		}
	}
}
