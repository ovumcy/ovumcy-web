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

// TestDashboardJournalHeaderShowsTheEditableEntryDateAndItsQuickSwitch pins the
// after-midnight contract: the journal header names the date the form writes to
// — as localized copy carrying the ISO date in its own hook — and offers the
// Today / Yesterday jumps unconditionally, so an evening entry logged after
// midnight can be moved to the day it belongs to. Yesterday already holds an
// entry here, the state that used to hide the only yesterday affordance.
func TestDashboardJournalHeaderShowsTheEditableEntryDateAndItsQuickSwitch(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "dashboard-entry-date@example.com", "StrongPass1", true)

	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	yesterday := today.AddDate(0, 0, -1)
	filledYesterday := models.DailyLog{
		UserID:   user.ID,
		Date:     yesterday,
		IsPeriod: true,
		Flow:     models.FlowMedium,
	}
	if err := database.Create(&filledYesterday).Error; err != nil {
		t.Fatalf("seed yesterday daily log: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	document := mustParseHTMLDocument(t, mustRenderDashboard(t, app, authCookie, "en"))

	entryDate := dashboardEntryDateElement(t, document)
	todayISO := today.Format("2006-01-02")
	if got := htmlAttr(entryDate, "data-dashboard-entry-date"); got != todayISO {
		t.Fatalf("expected the journal header to name %q as the editable date, got %q", todayISO, got)
	}
	saveForm := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-dashboard-save-form")
	})
	if saveForm == nil {
		t.Fatal("expected the dashboard save form")
	}
	if got := htmlAttr(saveForm, "hx-put"); got != "/api/v1/days/"+todayISO {
		t.Fatalf("expected the header date to be the date the form writes to, got hx-put=%q", got)
	}

	group := dashboardEntryDateSwitch(t, document)
	if got := htmlAttr(group, "role"); got != "group" {
		t.Fatalf("expected the entry-date switch to be a labelled group, got role=%q", got)
	}
	if htmlAttr(group, "aria-label") == "" {
		t.Fatal("expected the entry-date switch group to carry an accessible name")
	}
	if htmlFindElement(saveForm, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-dashboard-entry-date-switch")
	}) != nil {
		t.Fatal("expected the entry-date switch to sit outside the save form, so switching writes nothing")
	}

	choices := dashboardEntryDateChoices(t, group)
	todayChoice := choices["today"]
	if todayChoice == nil {
		t.Fatal("expected a Today entry-date choice")
	}
	if got := htmlAttr(todayChoice, "href"); got != "/dashboard" {
		t.Fatalf("expected the Today choice to reuse the dashboard route, got href=%q", got)
	}
	if got := htmlAttr(todayChoice, "aria-current"); got != "page" {
		t.Fatalf("expected the Today choice to be marked as the current date, got aria-current=%q", got)
	}
	if got := htmlAttr(todayChoice, "data-entry-date"); got != todayISO {
		t.Fatalf("expected the Today choice to carry %q, got %q", todayISO, got)
	}

	yesterdayChoice := choices["yesterday"]
	if yesterdayChoice == nil {
		t.Fatal("expected a Yesterday entry-date choice even when yesterday already holds an entry")
	}
	yesterdayISO := yesterday.Format("2006-01-02")
	wantHref := "/calendar?month=" + yesterday.Format("2006-01") + "&day=" + yesterdayISO + "&edit=1"
	if got := htmlAttr(yesterdayChoice, "href"); got != wantHref {
		t.Fatalf("expected the Yesterday choice to reuse the calendar day-editor route %q, got %q", wantHref, got)
	}
	if got := htmlAttr(yesterdayChoice, "data-entry-date"); got != yesterdayISO {
		t.Fatalf("expected the Yesterday choice to carry %q, got %q", yesterdayISO, got)
	}
	if got := htmlAttr(yesterdayChoice, "data-entry-date-empty"); got != "false" {
		t.Fatalf("expected the Yesterday choice to report the seeded entry, got data-entry-date-empty=%q", got)
	}
}

// TestDashboardJournalEntryDateFollowsRequestTimezoneAtUTCBoundary is the
// timezone half of the same contract: with the server on UTC and the request
// resolving to a zone whose calendar day differs, the date shown in the journal
// header — and the day the Yesterday jump targets — follow the request-local
// zone, never the server's UTC date.
func TestDashboardJournalEntryDateFollowsRequestTimezoneAtUTCBoundary(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "dashboard-entry-date-tz@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	nowUTC := time.Now().UTC()
	timezoneName, location := timezoneWithDifferentCalendarDay(t, nowUTC)
	localToday := services.DateAtLocation(nowUTC.In(location), location)
	localTodayISO := localToday.Format("2006-01-02")
	if serverTodayISO := services.DateAtLocation(nowUTC, time.UTC).Format("2006-01-02"); serverTodayISO == localTodayISO {
		t.Fatalf("expected %s to sit on another calendar day than UTC, both are %q", timezoneName, localTodayISO)
	}

	response := dashboardWithTimezoneResponse(t, app, authCookie, timezoneName)
	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))

	entryDate := dashboardEntryDateElement(t, document)
	if got := htmlAttr(entryDate, "data-dashboard-entry-date"); got != localTodayISO {
		t.Fatalf("expected the journal header to name the request-local date %q, got %q", localTodayISO, got)
	}
	// The localized copy and the hook must name the same day, or the visible
	// date and the saved date part company again.
	if got, want := strings.TrimSpace(htmlNodeText(entryDate)), services.LocalizedDashboardDate("ru", localToday); got != want {
		t.Fatalf("expected the header copy to follow the request-local date %q, got %q", want, got)
	}

	choices := dashboardEntryDateChoices(t, dashboardEntryDateSwitch(t, document))
	if got := htmlAttr(choices["today"], "data-entry-date"); got != localTodayISO {
		t.Fatalf("expected the Today choice to follow the request-local date %q, got %q", localTodayISO, got)
	}
	localYesterdayISO := localToday.AddDate(0, 0, -1).Format("2006-01-02")
	if got := htmlAttr(choices["yesterday"], "data-entry-date"); got != localYesterdayISO {
		t.Fatalf("expected the Yesterday choice to follow the request-local date %q, got %q", localYesterdayISO, got)
	}
}

func dashboardEntryDateElement(t *testing.T, document *html.Node) *html.Node {
	t.Helper()

	element := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-dashboard-entry-date")
	})
	if element == nil {
		t.Fatal("expected the journal header to expose the editable date through data-dashboard-entry-date")
	}
	return element
}

func dashboardEntryDateSwitch(t *testing.T, document *html.Node) *html.Node {
	t.Helper()

	group := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-dashboard-entry-date-switch")
	})
	if group == nil {
		t.Fatal("expected a Today / Yesterday entry-date switch next to the journal date")
	}
	return group
}

func dashboardEntryDateChoices(t *testing.T, group *html.Node) map[string]*html.Node {
	t.Helper()

	choices := make(map[string]*html.Node)
	for _, node := range htmlFindElements(group, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-entry-date-choice")
	}) {
		if node.Data != "a" || htmlAttr(node, "href") == "" {
			t.Fatalf("expected every entry-date choice to be a plain link, got <%s>", node.Data)
		}
		choices[htmlAttr(node, "data-entry-date-choice")] = node
	}
	if len(choices) != 2 || choices["today"] == nil || choices["yesterday"] == nil {
		t.Fatalf("expected exactly the today and yesterday choices, got %d", len(choices))
	}
	return choices
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
