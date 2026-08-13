package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/net/html"
	"gorm.io/gorm"
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

// TestDashboardPregnancyTestRendersAnUntestedDayAsAbsentData pins the first
// half of the pregnancy-test control's contract: an untested day is absent
// data, not a selected choice. The field offers exactly the two results, both
// unfilled, and says in its own hook that nothing is recorded; the "none" value
// the column still stores rides on a hidden carrier, so an untouched control
// writes back what it read.
func TestDashboardPregnancyTestRendersAnUntestedDayAsAbsentData(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "dashboard-pregnancy-absent@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	document := mustParseHTMLDocument(t, mustRenderDashboard(t, app, authCookie, "en"))
	field := pregnancyTestField(t, document)

	if got := htmlAttr(field, "data-pregnancy-test-state"); got != "absent" {
		t.Fatalf("expected an untested day to render as absent data, got state %q", got)
	}
	options := pregnancyTestOptions(t, field)
	for _, value := range []string{"negative", "positive"} {
		if options[value] == nil {
			t.Fatalf("expected the %q result to stay on offer", value)
		}
	}
	if options["none"] != nil {
		t.Fatal("expected no selectable \"none\" segment: an untested day is absent data, not a third result")
	}
	for value, option := range options {
		if htmlHasAttr(pregnancyTestOptionInput(t, option), "checked") {
			t.Fatalf("expected no filled segment on an untested day, %q is checked", value)
		}
	}

	if !pregnancyTestHasHook(field, "data-pregnancy-test-empty") {
		t.Fatal("expected an untested day to name its own empty state")
	}
	if pregnancyTestHasHook(field, "data-pregnancy-test-remove") {
		t.Fatal("expected no removal action when there is no result to remove")
	}

	carrier := pregnancyTestUnsetCarrier(t, field)
	if !htmlHasAttr(carrier, "checked") || !htmlHasAttr(carrier, "hidden") {
		t.Fatal("expected the unset carrier to be checked and hidden from view")
	}
}

// TestDashboardPregnancyTestOffersRemovalForEitherSavedResult is the other
// half: once "did not test" stops being a selectable segment, the unset state
// must stay reachable, or the most emotionally loaded field in the product
// becomes a one-way door. Both results are anchored — a removal offered only
// after a negative would leave a positive unremovable. The removal is a button
// standing after the group, never a radio inside it: as a third radio it
// announced as "3 of 3", which reads as a third possible result.
func TestDashboardPregnancyTestOffersRemovalForEitherSavedResult(t *testing.T) {
	for _, saved := range []string{models.PregnancyTestNegative, models.PregnancyTestPositive} {
		t.Run(saved, func(t *testing.T) {
			app, database := newOnboardingTestApp(t)
			user := createOnboardingTestUser(t, database, "dashboard-pregnancy-"+saved+"@example.com", "StrongPass1", true)

			today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
			if err := database.Create(&models.DailyLog{
				UserID:        user.ID,
				Date:          today,
				Flow:          models.FlowNone,
				PregnancyTest: saved,
			}).Error; err != nil {
				t.Fatalf("seed daily log: %v", err)
			}

			authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
			document := mustParseHTMLDocument(t, mustRenderDashboard(t, app, authCookie, "en"))
			field := pregnancyTestField(t, document)

			if got := htmlAttr(field, "data-pregnancy-test-state"); got != "recorded" {
				t.Fatalf("expected a saved result to render as recorded, got state %q", got)
			}
			options := pregnancyTestOptions(t, field)
			for value, option := range options {
				checked := htmlHasAttr(pregnancyTestOptionInput(t, option), "checked")
				if checked != (value == saved) {
					t.Fatalf("expected only %q to be filled, %q checked=%v", saved, value, checked)
				}
			}

			remove := htmlFindElement(field, func(node *html.Node) bool {
				return node.Type == html.ElementNode && htmlHasAttr(node, "data-pregnancy-test-remove")
			})
			if remove == nil {
				t.Fatalf("expected a saved %q result to be removable", saved)
			}
			if remove.Data != "button" || htmlAttr(remove, "type") != "button" {
				t.Fatalf("expected the removal to be a type=button control, got <%s type=%q>", remove.Data, htmlAttr(remove, "type"))
			}
			if htmlFindElement(remove, func(node *html.Node) bool {
				return node.Type == html.ElementNode && node.Data == "input"
			}) != nil {
				t.Fatal("expected no input inside the removal action: the radiogroup keeps exactly the two results")
			}
			if htmlNodeContains(pregnancyTestResultRow(t, field), remove) {
				t.Fatal("expected the removal to stand after the result row, not inside the radiogroup")
			}
			if got := strings.TrimSpace(htmlNodeText(remove)); got == "" {
				t.Fatal("expected the removal button to name itself: its accessible name is its own text")
			}

			// The unset value keeps riding a hidden carrier, unselected while a
			// result stands: the button moves it, so removal must have
			// something to move.
			carrier := pregnancyTestUnsetCarrier(t, field)
			if htmlHasAttr(carrier, "checked") {
				t.Fatal("expected the unset carrier to start unselected while a result is recorded")
			}
			if !htmlHasAttr(carrier, "hidden") {
				t.Fatal("expected the unset carrier to stay hidden from view")
			}
			if pregnancyTestHasHook(field, "data-pregnancy-test-empty") {
				t.Fatal("expected no empty state while a result is recorded")
			}
		})
	}
}

// TestDashboardJournalKeepsRareFieldsBehindOneDisclosure pins the journal's
// two tiers. Everything a day is usually logged with stays in the open; the
// rare fields sit inside one closed disclosure, and a day that holds none of
// them renders it closed.
func TestDashboardJournalKeepsRareFieldsBehindOneDisclosure(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "dashboard-more-closed@example.com", "StrongPass1", true)
	enableDashboardMeasurementTracking(t, database, user.ID)

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	document := mustParseHTMLDocument(t, mustRenderDashboard(t, app, authCookie, "en"))
	form := dashboardSaveForm(t, document)
	more := dashboardMoreDisclosure(t, form)

	if htmlHasAttr(more, "open") {
		t.Fatal("expected the disclosure to stay closed on a day that holds none of its fields")
	}
	if summary := htmlFindElement(more, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "summary"
	}); summary == nil || strings.TrimSpace(htmlNodeText(summary)) == "" {
		t.Fatal("expected the disclosure to name itself in its summary")
	}

	for _, tier := range []struct {
		hook   func(*html.Node) bool
		inside bool
		name   string
	}{
		{hook: htmlNodeHasAttr("data-period-toggle"), inside: false, name: "the period toggle"},
		{hook: htmlNodeHasAttr("data-period-fields"), inside: false, name: "the flow fieldset"},
		{hook: htmlNodeAttrEquals("data-dashboard-section", "mood"), inside: false, name: "mood"},
		{hook: htmlNodeAttrEquals("data-dashboard-section", "symptoms"), inside: false, name: "symptoms"},
		{hook: htmlNodeAttrEquals("name", "sex_activity"), inside: true, name: "intimacy"},
		{hook: htmlNodeAttrEquals("name", "cervical_mucus"), inside: true, name: "cervical mucus"},
		{hook: htmlNodeHasAttr("data-pregnancy-test"), inside: true, name: "the pregnancy test"},
		{hook: htmlNodeAttrEquals("id", "dashboard-bbt"), inside: true, name: "BBT"},
		{hook: htmlNodeAttrEquals("name", "cycle_factor_keys"), inside: true, name: "cycle factors"},
		{hook: htmlNodeHasAttr("data-note-disclosure"), inside: true, name: "the note disclosure"},
	} {
		element := htmlFindElement(form, tier.hook)
		if element == nil {
			t.Fatalf("expected %s to be rendered in the journal", tier.name)
		}
		if got := htmlNodeContains(more, element); got != tier.inside {
			t.Fatalf("expected %s inside the disclosure=%v, got %v", tier.name, tier.inside, got)
		}
	}
}

// TestDashboardJournalMoreDisclosureOpensForDataItHolds is the other half of
// the contract: a value already recorded is never hidden behind a closed
// disclosure. Each field behind it opens it on its own, and a field the
// tracking settings hide is absent from the form altogether — a value left in
// its column cannot open the disclosure over a control that does not exist.
func TestDashboardJournalMoreDisclosureOpensForDataItHolds(t *testing.T) {
	for name, seed := range map[string]func(*models.DailyLog){
		"sex_activity":   func(entry *models.DailyLog) { entry.SexActivity = models.SexActivityProtected },
		"cervical_mucus": func(entry *models.DailyLog) { entry.CervicalMucus = models.CervicalMucusEggWhite },
		"pregnancy_test": func(entry *models.DailyLog) { entry.PregnancyTest = models.PregnancyTestNegative },
		"bbt":            func(entry *models.DailyLog) { entry.BBT = new(36.6) },
		"cycle_factors":  func(entry *models.DailyLog) { entry.CycleFactorKeys = []string{models.CycleFactorStress} },
		"notes":          func(entry *models.DailyLog) { entry.Notes = "logged in the evening" },
	} {
		t.Run(name, func(t *testing.T) {
			app, database := newOnboardingTestApp(t)
			user := createOnboardingTestUser(t, database, "dashboard-more-"+name+"@example.com", "StrongPass1", true)
			enableDashboardMeasurementTracking(t, database, user.ID)
			seedDashboardTodayLog(t, database, user.ID, seed)

			authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
			document := mustParseHTMLDocument(t, mustRenderDashboard(t, app, authCookie, "en"))
			more := dashboardMoreDisclosure(t, dashboardSaveForm(t, document))

			if !htmlHasAttr(more, "open") {
				t.Fatalf("expected a day holding %s to render the disclosure open", name)
			}
		})
	}

	t.Run("hidden by settings", func(t *testing.T) {
		app, database := newOnboardingTestApp(t)
		user := createOnboardingTestUser(t, database, "dashboard-more-hidden@example.com", "StrongPass1", true)
		seedDashboardTodayLog(t, database, user.ID, func(entry *models.DailyLog) {
			entry.CervicalMucus = models.CervicalMucusEggWhite
		})

		authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
		document := mustParseHTMLDocument(t, mustRenderDashboard(t, app, authCookie, "en"))
		form := dashboardSaveForm(t, document)

		if htmlFindElement(form, htmlNodeAttrEquals("name", "cervical_mucus")) != nil {
			t.Fatal("expected an untracked field to stay absent from the form, not to be folded into the disclosure")
		}
		if htmlHasAttr(dashboardMoreDisclosure(t, form), "open") {
			t.Fatal("expected a value behind an untracked field to leave the disclosure closed")
		}
	})
}

// TestDashboardFramesTimingForTheGoalThatIsAboutIt pins the goal-aware timing
// frame promised at onboarding. An account trying to conceive reads the page
// for timing, so the status line also carries the ovulation estimate and the
// morning temperature sits in the journal's visible tier; the disclosure then
// stops counting a recorded temperature, which is no longer behind it. Every
// other goal keeps the line and the two tiers it had.
//
// Both accounts own one completed cycle: the estimate exists only once there is
// an observed cycle to project from, and what the same goal reads before that
// is TestDashboardHeaderWithholdsFertilityUntilTheFirstCompletedCycle's subject.
func TestDashboardFramesTimingForTheGoalThatIsAboutIt(t *testing.T) {
	for name, testCase := range map[string]struct {
		goal   string
		framed bool
	}{
		"trying to conceive": {goal: models.UsageGoalTrying, framed: true},
		"general health":     {goal: models.UsageGoalHealth, framed: false},
	} {
		t.Run(name, func(t *testing.T) {
			app, database := newOnboardingTestApp(t)
			user := createOnboardingTestUser(t, database, "dashboard-timing-"+testCase.goal+"@example.com", "StrongPass1", true)
			enableDashboardMeasurementTracking(t, database, user.ID)
			seedDashboardStableCycleForGoal(t, database, user.ID, testCase.goal)
			seedDashboardCompletedCycle(t, database, user.ID)
			seedDashboardTodayLog(t, database, user.ID, func(entry *models.DailyLog) {
				entry.BBT = new(36.6)
			})

			authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
			document := mustParseHTMLDocument(t, mustRenderDashboard(t, app, authCookie, "en"))

			statusLine := dashboardElementByDataAttr(document, "data-dashboard-status-line")
			if statusLine == nil {
				t.Fatal("expected the dashboard status line")
			}
			if got := htmlFindElement(statusLine, htmlNodeHasAttr("data-dashboard-ovulation")) != nil; got != testCase.framed {
				t.Fatalf("expected the ovulation estimate in the status line=%v, got %v", testCase.framed, got)
			}
			if dashboardElementByDataAttr(document, "data-dashboard-prediction-disclaimer") == nil {
				t.Fatal("expected the prediction disclaimer beside a prediction surface")
			}

			form := dashboardSaveForm(t, document)
			more := dashboardMoreDisclosure(t, form)
			temperature := htmlFindElement(form, htmlNodeAttrEquals("id", "dashboard-bbt"))
			if temperature == nil {
				t.Fatal("expected the temperature field in the journal")
			}
			if got := htmlNodeContains(more, temperature); got == testCase.framed {
				t.Fatalf("expected the temperature field inside the disclosure=%v, got %v", !testCase.framed, got)
			}
			if got := htmlHasAttr(more, "open"); got == testCase.framed {
				t.Fatalf("expected today's temperature to open the disclosure=%v, got %v", !testCase.framed, got)
			}
		})
	}
}

// TestDashboardTimingFrameKeepsSuppressedPredictionsSuppressed is the safety
// half: the goal reframes what a prediction is called, never whether one is
// made. An account that turned predictions off sees no ovulation estimate
// whatever its goal, while the field placement — which no prediction feeds —
// stays as the goal asked.
func TestDashboardTimingFrameKeepsSuppressedPredictionsSuppressed(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "dashboard-timing-suppressed@example.com", "StrongPass1", true)
	enableDashboardMeasurementTracking(t, database, user.ID)
	seedDashboardStableCycleForGoal(t, database, user.ID, models.UsageGoalTrying)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("unpredictable_cycle", true).Error; err != nil {
		t.Fatalf("enable unpredictable cycle: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	document := mustParseHTMLDocument(t, mustRenderDashboard(t, app, authCookie, "en"))

	statusLine := dashboardElementByDataAttr(document, "data-dashboard-status-line")
	if statusLine == nil {
		t.Fatal("expected the dashboard status line")
	}
	if htmlFindElement(statusLine, htmlNodeHasAttr("data-dashboard-ovulation")) != nil {
		t.Fatal("did not expect an ovulation estimate while predictions are suppressed")
	}

	form := dashboardSaveForm(t, document)
	temperature := htmlFindElement(form, htmlNodeAttrEquals("id", "dashboard-bbt"))
	if temperature == nil {
		t.Fatal("expected the temperature field in the journal")
	}
	if htmlNodeContains(dashboardMoreDisclosure(t, form), temperature) {
		t.Fatal("expected the temperature field to stay in the visible tier for this goal")
	}
}

// TestDashboardTimingFrameDropsTheOvulationEstimateForAnOverdueCycle is the same
// safety half against the other suppression signal. Once a cycle runs past its
// reference length by more than a week the dashboard withholds the projected
// window; the ovulation estimate is derived from that same projection, so an
// account trying to conceive must not be the one cohort still reading a slot
// where the window used to be. The field placement is a property of the goal
// alone — a late cycle is when a morning reading matters most — so the
// temperature stays in the visible tier.
func TestDashboardTimingFrameDropsTheOvulationEstimateForAnOverdueCycle(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "dashboard-timing-overdue@example.com", "StrongPass1", true)
	enableDashboardMeasurementTracking(t, database, user.ID)
	seedDashboardStableCycleForGoal(t, database, user.ID, models.UsageGoalTrying)
	// Cycle day 37 against the 28-day reference: past 28 + 7.
	today := services.DateAtLocation(time.Now().UTC(), time.UTC)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).
		Update("last_period_start", today.AddDate(0, 0, -36)).Error; err != nil {
		t.Fatalf("seed overdue cycle context: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	document := mustParseHTMLDocument(t, mustRenderDashboard(t, app, authCookie, "en"))

	statusLine := dashboardElementByDataAttr(document, "data-dashboard-status-line")
	if statusLine == nil {
		t.Fatal("expected the dashboard status line")
	}
	if htmlFindElement(statusLine, htmlNodeHasAttr("data-dashboard-ovulation")) != nil {
		t.Fatal("did not expect an ovulation estimate for a cycle past the late threshold")
	}
	if htmlFindElement(statusLine, htmlNodeHasAttr("data-dashboard-next-period")) != nil {
		t.Fatal("did not expect a next-period estimate for a cycle past the late threshold")
	}
	if htmlFindElement(statusLine, htmlNodeHasAttr("data-dashboard-next-period-paused")) == nil {
		t.Fatal("expected the withheld-estimate line in place of the two estimates")
	}

	form := dashboardSaveForm(t, document)
	temperature := htmlFindElement(form, htmlNodeAttrEquals("id", "dashboard-bbt"))
	if temperature == nil {
		t.Fatal("expected the temperature field in the journal")
	}
	if htmlNodeContains(dashboardMoreDisclosure(t, form), temperature) {
		t.Fatal("expected the temperature field to stay in the visible tier for this goal")
	}
}

// seedDashboardStableCycleForGoal gives the account a cycle context predictions
// can be made from, under the usage goal the case is about.
func seedDashboardStableCycleForGoal(t *testing.T, database *gorm.DB, userID uint, goal string) {
	t.Helper()

	today := services.DateAtLocation(time.Now().UTC(), time.UTC)
	if err := database.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"usage_goal":        goal,
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": today.AddDate(0, 0, -2),
	}).Error; err != nil {
		t.Fatalf("seed cycle context for %s: %v", goal, err)
	}
}

// seedDashboardCompletedCycle records the two cycle starts that make one
// completed cycle — the threshold the status header's fertility half waits for —
// keeping the running cycle on the same recent anchor
// seedDashboardStableCycleForGoal set.
func seedDashboardCompletedCycle(t *testing.T, database *gorm.DB, userID uint) {
	t.Helper()

	today := services.DateAtLocation(time.Now().UTC(), time.UTC)
	for _, offsetDays := range []int{-30, -2} {
		if err := database.Create(&models.DailyLog{
			UserID:     userID,
			Date:       today.AddDate(0, 0, offsetDays),
			IsPeriod:   true,
			CycleStart: true,
		}).Error; err != nil {
			t.Fatalf("seed cycle start %d: %v", offsetDays, err)
		}
	}
}

// enableDashboardMeasurementTracking switches on the two tracking-gated fields
// the journal's disclosure holds (BBT and cervical mucus); the rest are on by
// default.
func enableDashboardMeasurementTracking(t *testing.T, database *gorm.DB, userID uint) {
	t.Helper()

	if err := database.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"track_bbt":            true,
		"track_cervical_mucus": true,
		"temperature_unit":     services.TemperatureUnitCelsius,
	}).Error; err != nil {
		t.Fatalf("enable measurement tracking: %v", err)
	}
}

// seedDashboardTodayLog writes today's entry with one field filled in.
func seedDashboardTodayLog(t *testing.T, database *gorm.DB, userID uint, fill func(*models.DailyLog)) {
	t.Helper()

	entry := models.DailyLog{
		UserID: userID,
		Date:   services.DateAtLocation(time.Now().In(time.UTC), time.UTC),
		Flow:   models.FlowNone,
	}
	fill(&entry)
	if err := database.Create(&entry).Error; err != nil {
		t.Fatalf("seed daily log: %v", err)
	}
}

// dashboardSaveForm returns the owner's day form on the dashboard.
func dashboardSaveForm(t *testing.T, document *html.Node) *html.Node {
	t.Helper()

	form := htmlFindElement(document, htmlNodeHasAttr("data-dashboard-save-form"))
	if form == nil {
		t.Fatal("expected the dashboard save form")
	}
	return form
}

// dashboardMoreDisclosure returns the journal's single "More" disclosure.
func dashboardMoreDisclosure(t *testing.T, form *html.Node) *html.Node {
	t.Helper()

	disclosures := htmlFindElements(form, htmlNodeHasAttr("data-dashboard-more"))
	if len(disclosures) != 1 {
		t.Fatalf("expected exactly one More disclosure in the journal, got %d", len(disclosures))
	}
	if disclosures[0].Data != "details" {
		t.Fatalf("expected the disclosure to be a <details>, got <%s>", disclosures[0].Data)
	}
	return disclosures[0]
}

func htmlNodeHasAttr(name string) func(*html.Node) bool {
	return func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, name)
	}
}

func htmlNodeAttrEquals(name string, value string) func(*html.Node) bool {
	return func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlAttr(node, name) == value
	}
}

// pregnancyTestField returns the single rendered pregnancy-test control.
func pregnancyTestField(t *testing.T, root *html.Node) *html.Node {
	t.Helper()

	fields := htmlFindElements(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-pregnancy-test")
	})
	if len(fields) != 1 {
		t.Fatalf("expected exactly one pregnancy-test control, got %d", len(fields))
	}
	return fields[0]
}

// pregnancyTestHasHook reports whether the control renders a given state hook.
func pregnancyTestHasHook(field *html.Node, hook string) bool {
	return htmlFindElement(field, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, hook)
	}) != nil
}

// pregnancyTestOptions maps each offered result to its option element.
func pregnancyTestOptions(t *testing.T, field *html.Node) map[string]*html.Node {
	t.Helper()

	options := make(map[string]*html.Node)
	for _, node := range htmlFindElements(field, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-pregnancy-test-option")
	}) {
		options[htmlAttr(node, "data-pregnancy-test-option")] = node
	}
	if len(options) != 2 {
		offered := make([]string, 0, len(options))
		for value := range options {
			offered = append(offered, value)
		}
		sort.Strings(offered)
		t.Fatalf("expected exactly the two result options negative and positive, got %v", offered)
	}
	return options
}

// pregnancyTestResultRow returns the row that holds the two result radios.
func pregnancyTestResultRow(t *testing.T, field *html.Node) *html.Node {
	t.Helper()

	row := htmlFindElement(field, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasClass(node, "pregnancy-test-row")
	})
	if row == nil {
		t.Fatal("expected the two results to share one row")
	}
	return row
}

// pregnancyTestUnsetCarrier returns the hidden radio that carries "none".
func pregnancyTestUnsetCarrier(t *testing.T, field *html.Node) *html.Node {
	t.Helper()

	carrier := htmlFindElement(field, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-pregnancy-test-unset")
	})
	if carrier == nil {
		t.Fatal("expected the unset value to stay in the form, or an untouched control would post nothing")
	}
	if got := htmlAttr(carrier, "value"); got != models.PregnancyTestNone {
		t.Fatalf("expected the carrier to post %q, got %q", models.PregnancyTestNone, got)
	}
	return carrier
}

// htmlNodeContains reports whether candidate is a descendant of parent.
func htmlNodeContains(parent *html.Node, candidate *html.Node) bool {
	for node := candidate; node != nil; node = node.Parent {
		if node == parent {
			return true
		}
	}
	return false
}

func pregnancyTestOptionInput(t *testing.T, option *html.Node) *html.Node {
	t.Helper()

	input := htmlFindElement(option, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "input" && htmlAttr(node, "name") == "pregnancy_test"
	})
	if input == nil {
		t.Fatal("expected the option to carry a pregnancy_test radio")
	}
	return input
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

// On the dashboard journal the cycle-start question is asked once, beside the
// period toggle that raised it. The hint that used to send the owner to the
// separate manual control gives way to it — two asks for one event is the
// defect, so the hint's own hook is asserted absent while the question is up.
func TestDashboardAsksTheCycleStartQuestionInsteadOfPointingAtTheManualControl(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "dashboard-cycle-start-question@example.com", "StrongPass1", true)

	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	entries := []models.DailyLog{
		{UserID: user.ID, Date: today.AddDate(0, 0, -28), IsPeriod: true, Flow: models.FlowMedium, CycleStart: true},
		{UserID: user.ID, Date: today, IsPeriod: true, Flow: models.FlowMedium},
	}
	if err := database.Create(&entries).Error; err != nil {
		t.Fatalf("create daily logs: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"=UTC"))
	request.Header.Set(timezoneHeaderName, "UTC")

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	rendered := mustReadBodyString(t, response.Body)

	if got := strings.Count(rendered, "data-cycle-start-question"); got != 1 {
		t.Fatalf("expected exactly one inline cycle-start question on the dashboard journal, got %d", got)
	}
	if strings.Contains(rendered, "data-cycle-start-suggestion") {
		t.Fatalf("expected the manual-control hint to stand down while the inline question is asked")
	}
	// Positive anchor: the manual control itself stays available for corrections.
	if !strings.Contains(rendered, "data-dashboard-cycle-start-button") {
		t.Fatalf("expected the separate manual cycle-start control to stay on the page")
	}
}

// TestDashboardMoodPickerNamesEveryStepOfTheScale pins the fix for an audit
// finding raised by two lenses at once: the mood picker was a row of faces with
// nothing naming what any of them meant, so the scale was guessed and two
// people picking the third face could be recording different things into a
// health record. The faces stay — they present logged data — and every step now
// carries a name.
//
// The contract asserted here is structural, so translated copy can change
// freely: the picker offers exactly the steps the service scale defines; each
// one exposes the name key the service resolves for it; each one's accessible
// name, its tooltip and its visible name are the same resolved string, which is
// what makes the visible label and the announced label the same label; and no
// two steps resolve to the same name.
func TestDashboardMoodPickerNamesEveryStepOfTheScale(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "dashboard-mood-names@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	document := mustParseHTMLDocument(t, mustRenderDashboard(t, app, authCookie, "en"))
	picker := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-mood-picker")
	})
	if picker == nil {
		t.Fatal("expected the journal to expose the mood picker through data-mood-picker")
	}

	scale := services.DayMoodScale()
	options := htmlFindElements(picker, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-mood-option")
	})
	if len(options) != len(scale.Steps) {
		t.Fatalf("expected one option per scale step (%d), got %d", len(scale.Steps), len(options))
	}

	namesByStep := make(map[int]string, len(scale.Steps))
	for index, step := range scale.Steps {
		option := options[index]
		if got := htmlAttr(option, "data-mood-option"); got != strconv.Itoa(step) {
			t.Fatalf("expected option %d to be step %d, got %q (the picker renders the scale in order)", index, step, got)
		}

		input := htmlFindElement(option, func(node *html.Node) bool {
			return node.Type == html.ElementNode && node.Data == "input" && htmlAttr(node, "name") == "mood"
		})
		if input == nil {
			t.Fatalf("step %d: expected a mood radio inside the option", step)
		}
		if got := htmlAttr(input, "value"); got != strconv.Itoa(step) {
			t.Fatalf("step %d: radio posts %q", step, got)
		}

		accessibleName := htmlAttr(input, "aria-label")
		if strings.TrimSpace(accessibleName) == "" {
			t.Fatalf("step %d: the control's only content is a decorative face, so it must name itself", step)
		}

		name := htmlFindElement(option, func(node *html.Node) bool {
			return node.Type == html.ElementNode && htmlHasAttr(node, "data-mood-name")
		})
		if name == nil {
			t.Fatalf("step %d: expected a visible name beside the face", step)
		}
		if got, want := htmlAttr(name, "data-mood-name-key"), services.MoodTranslationKey(step); got != want {
			t.Fatalf("step %d: name key %q, want %q", step, got, want)
		}
		visibleName := strings.TrimSpace(htmlNodeText(name))
		if visibleName != accessibleName {
			t.Fatalf("step %d: visible name %q and accessible name %q disagree", step, visibleName, accessibleName)
		}
		if visibleName == services.MoodTranslationKey(step) {
			t.Fatalf("step %d: the name rendered as its own catalogue key, so nothing resolved it", step)
		}

		chip := htmlFindElement(option, func(node *html.Node) bool {
			return node.Type == html.ElementNode && htmlHasClass(node, "chip")
		})
		if chip == nil {
			t.Fatalf("step %d: expected the face to sit on a chip", step)
		}
		if got := htmlAttr(chip, "title"); got != accessibleName {
			t.Fatalf("step %d: tooltip %q disagrees with the accessible name %q", step, got, accessibleName)
		}

		for earlier, taken := range namesByStep {
			if taken == visibleName {
				t.Fatalf("steps %d and %d render the same name %q", earlier, step, visibleName)
			}
		}
		namesByStep[step] = visibleName
	}

	// Nothing is logged for today, so no step is selected and the caption names
	// the ends of the scale instead — the row's direction has to be readable
	// before the first tap, which is the half of the finding a selected-step
	// caption alone would leave standing.
	ends := htmlFindElement(picker, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-mood-scale-ends")
	})
	if ends == nil {
		t.Fatal("expected the picker to name the ends of the scale while nothing is chosen")
	}
	endsText := htmlNodeText(ends)
	for _, step := range []int{scale.Lowest, scale.Highest} {
		if !strings.Contains(endsText, namesByStep[step]) {
			t.Fatalf("expected the scale-ends caption to name step %d, got %q", step, endsText)
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
