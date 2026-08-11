package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
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
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("dashboard request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

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
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("dashboard request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	disclosure := htmlElementByTagAndClass(document, "details", "note-disclosure")
	if disclosure == nil {
		t.Fatalf("expected dashboard note field to render as a disclosure")
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
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("dashboard request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	summary := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-usage-goal-summary")
	})
	if summary == nil {
		t.Fatal("expected dashboard usage-goal summary panel")
	}
	if got := htmlAttr(summary, "data-usage-goal-label-key"); got != "settings.goal.trying" {
		t.Fatalf("expected usage-goal label key %q, got %q", "settings.goal.trying", got)
	}
	if got := htmlAttr(summary, "data-usage-goal-summary-key"); got != "usage_goal.summary.trying" {
		t.Fatalf("expected usage-goal summary key %q, got %q", "usage_goal.summary.trying", got)
	}
}

// TestDashboardOffersAQuickUsageGoalSwitchForOwner pins the quick-change
// control next to the mode line: the modes the owner is NOT in are offered as
// one-click chips, each patching the existing cycle-settings endpoint, and the
// current mode is not offered again (the line above already names it).
func TestDashboardOffersAQuickUsageGoalSwitchForOwner(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "dashboard-usage-goal-switch@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("usage_goal", models.UsageGoalTrying).Error; err != nil {
		t.Fatalf("update usage goal: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	group := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-usage-goal-quick-switch")
	})
	if group == nil {
		t.Fatal("expected a dashboard usage-goal quick-switch group")
	}
	if got := htmlAttr(group, "role"); got != "group" {
		t.Fatalf("expected the quick-switch container to be a labelled group, got role=%q", got)
	}

	choices := htmlFindElements(group, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-usage-goal-choice")
	})
	offered := make([]string, 0, len(choices))
	for _, choice := range choices {
		offered = append(offered, htmlAttr(choice, "data-usage-goal-choice"))
	}
	sort.Strings(offered)
	want := []string{models.UsageGoalAvoid, models.UsageGoalHealth}
	if !reflect.DeepEqual(offered, want) {
		t.Fatalf("expected the two alternative modes %v to be offered, got %v", want, offered)
	}

	forms := htmlFindElements(group, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "form"
	})
	if len(forms) != len(choices) {
		t.Fatalf("expected one form per offered mode, got %d forms for %d modes", len(forms), len(choices))
	}
	for _, form := range forms {
		if got := htmlAttr(form, "hx-patch"); got != "/api/v1/users/current/cycle" {
			t.Fatalf("expected the quick switch to patch the existing cycle endpoint, got %q", got)
		}
		// A goal-only patch: nothing else about the cycle may ride along, or a
		// stale dashboard would silently rewrite settings changed elsewhere.
		fields := htmlFindElements(form, func(node *html.Node) bool {
			return node.Type == html.ElementNode && node.Data == "input"
		})
		names := make([]string, 0, len(fields))
		for _, field := range fields {
			names = append(names, htmlAttr(field, "name"))
		}
		sort.Strings(names)
		if !reflect.DeepEqual(names, []string{"csrf_token", "usage_goal"}) {
			t.Fatalf("expected the quick switch to submit only the goal, got fields %v", names)
		}
	}
}

func assertDashboardSavedNoteDisclosure(t *testing.T, document *html.Node) {
	t.Helper()

	// "Remember to hydrate" is user-entered note content verifying data round-trip, not UI copy.
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
