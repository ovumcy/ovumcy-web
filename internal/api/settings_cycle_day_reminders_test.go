package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

const cycleDayRemindersPath = "/api/v1/users/current/cycle-day-reminders"

// sendCycleDayReminderForm sends an HTMX form write as the settings card does.
// withCSRF=false drops the token; the auth cookie is always sent.
func sendCycleDayReminderForm(t *testing.T, owner settingsSecurityTestContext, method string, path string, form string, withCSRF bool) (int, string) {
	t.Helper()
	// The settings page's account binding, as every card form carries it, so a
	// refusal below is the one under test and not the cross-account guard.
	binding := dayFormAccountWireName + "=" + url.QueryEscape(renderedDayFormAccount(t, owner.app, owner.authCookie, "/settings"))
	if form == "" {
		form = binding
	} else {
		form += "&" + binding
	}
	request := httptest.NewRequest(method, path, strings.NewReader(form))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Accept", "text/html")
	request.Header.Set("Cookie", owner.authCookie)
	if withCSRF {
		if owner.csrfCookie != nil {
			request.AddCookie(owner.csrfCookie)
		}
		request.Header.Set("X-CSRF-Token", owner.csrfToken)
	}
	response, err := owner.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = response.Body.Close() }()
	return response.StatusCode, mustReadBodyString(t, response.Body)
}

func listCycleDayRulesForTest(t *testing.T, owner settingsSecurityTestContext, userID uint) []models.CycleDayReminder {
	t.Helper()
	var rules []models.CycleDayReminder
	if err := owner.database.Where("user_id = ?", userID).Order("id ASC").Find(&rules).Error; err != nil {
		t.Fatalf("list rules: %v", err)
	}
	return rules
}

var cycleDayCSRFValuePattern = regexp.MustCompile(`(csrf[^"]*"\s+(?:value|content)=")[^"]*(")`)

func normalizeCycleDayCardBody(body string) string {
	return cycleDayCSRFValuePattern.ReplaceAllString(body, "${1}CSRF${2}")
}

func TestCycleDayReminderCreateRendersTheEgressCardWithTheRule(t *testing.T) {
	owner := newSettingsSecurityTestContext(t, "cdr-api-create@example.com")
	messages := mustEnglishMessages(t)

	status, body := sendCycleDayReminderForm(t, owner, http.MethodPost, cycleDayRemindersPath, "cycle_day=15&remind_at_hour=8&label=Vitamin+D", true)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, body)
	}
	if !strings.Contains(body, `id="settings-egress"`) {
		t.Fatalf("the answer must be the egress card: %s", body)
	}
	rules := listCycleDayRulesForTest(t, owner, owner.user.ID)
	if len(rules) != 1 || rules[0].CycleDay != 15 || rules[0].RemindAtHour != 8 || rules[0].Label != "Vitamin D" || rules[0].SendLabel {
		t.Fatalf("expected the stored rule, got %+v", rules)
	}
	if !strings.Contains(body, fmt.Sprintf(`data-egress-cycle-day-rule="%d"`, rules[0].ID)) {
		t.Fatalf("the card must list the new rule: %s", body)
	}
	if !strings.Contains(body, `value="Vitamin D"`) {
		t.Fatalf("the card must show the rule's label: %s", body)
	}
	// The stored rule carries its own edit and remove forms, and below the cap
	// the add form is still offered.
	for _, hook := range []string{"data-egress-cycle-day-update", "data-egress-cycle-day-remove", "data-egress-cycle-day-add"} {
		if !strings.Contains(body, hook) {
			t.Fatalf("the card must render %s: %s", hook, body)
		}
	}
	if strings.Contains(body, "data-egress-cycle-day-limit") {
		t.Fatalf("one rule is below the cap; the card must not announce the limit: %s", body)
	}
	if added := messages["settings.success.cycle_day_reminder_added"]; added == "" || !strings.Contains(body, added) {
		t.Fatalf("the card must carry the added status %q", added)
	}
}

func TestCycleDayReminderCardWithdrawsTheAddFormAtTheCap(t *testing.T) {
	owner := newSettingsSecurityTestContext(t, "cdr-api-cap@example.com")
	for day := 1; day < models.CycleDayReminderMaxPerOwner; day++ {
		rule := models.CycleDayReminder{UserID: owner.user.ID, CycleDay: day, RemindAtHour: 9}
		if err := owner.database.Create(&rule).Error; err != nil {
			t.Fatalf("seed rule %d: %v", day, err)
		}
	}

	status, body := sendCycleDayReminderForm(t, owner, http.MethodPost, cycleDayRemindersPath, fmt.Sprintf("cycle_day=%d&remind_at_hour=9", models.CycleDayReminderMaxPerOwner), true)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, body)
	}
	if rules := listCycleDayRulesForTest(t, owner, owner.user.ID); len(rules) != models.CycleDayReminderMaxPerOwner {
		t.Fatalf("expected %d stored rules, got %d", models.CycleDayReminderMaxPerOwner, len(rules))
	}
	if strings.Contains(body, "data-egress-cycle-day-add") {
		t.Fatalf("at the cap the card must not offer the add form: %s", body)
	}
	if !strings.Contains(body, "data-egress-cycle-day-limit") {
		t.Fatalf("at the cap the card must say the limit is reached: %s", body)
	}
}

// The label leaves the server only for an opted-in rule, and the warning
// beside that opt-in names where it would go: the configured webhook's host.
func TestCycleDayReminderLabelWarningNamesTheWebhookHost(t *testing.T) {
	owner := newSettingsSecurityTestContext(t, "cdr-api-host@example.com")

	status, body := sendCycleDayReminderForm(t, owner, http.MethodPost, "/api/v1/users/current/webhook", "webhook_url="+url.QueryEscape("https://ntfy.example.test/cycle-day")+"&webhook_enabled=true&webhook_notify_period=true", true)
	if status != http.StatusOK {
		t.Fatalf("configure the webhook: expected 200, got %d: %s", status, body)
	}
	status, body = sendCycleDayReminderForm(t, owner, http.MethodPost, cycleDayRemindersPath, "cycle_day=14&remind_at_hour=9&label=Check-in", true)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", status, body)
	}
	hostPattern := regexp.MustCompile(`<span data-egress-cycle-day-label-host>\s*ntfy\.example\.test\s*</span>`)
	if got := len(hostPattern.FindAllString(body, -1)); got < 2 {
		t.Fatalf("both the rule's warning and the add form's warning must name the webhook host, found %d: %s", got, body)
	}
}

func TestCycleDayReminderAnotherOwnersRuleAnswersLikeANeverExistingOne(t *testing.T) {
	owner := newSettingsSecurityTestContext(t, "cdr-api-owner@example.com")
	other := createOnboardingTestUser(t, owner.database, "cdr-api-other@example.com", "StrongPass1", true)
	foreign := models.CycleDayReminder{UserID: other.ID, CycleDay: 12, RemindAtHour: 7, Label: "theirs"}
	if err := owner.database.Create(&foreign).Error; err != nil {
		t.Fatalf("seed foreign rule: %v", err)
	}
	missingID := foreign.ID + 1000
	notFound := mustEnglishMessages(t)["settings.error.cycle_day_reminder_not_found"]
	if notFound == "" {
		t.Fatal("the not-found message is missing from the catalogue")
	}

	cases := []struct {
		method string
		form   string
	}{
		{http.MethodPatch, "cycle_day=20&remind_at_hour=20&label=mine"},
		{http.MethodDelete, ""},
	}
	for _, testCase := range cases {
		foreignStatus, foreignBody := sendCycleDayReminderForm(t, owner, testCase.method, fmt.Sprintf("%s/%d", cycleDayRemindersPath, foreign.ID), testCase.form, true)
		missingStatus, missingBody := sendCycleDayReminderForm(t, owner, testCase.method, fmt.Sprintf("%s/%d", cycleDayRemindersPath, missingID), testCase.form, true)

		if !strings.Contains(foreignBody, `id="settings-egress"`) || !strings.Contains(foreignBody, notFound) {
			t.Fatalf("%s of another owner's rule must answer the card with the not-found refusal: %s", testCase.method, foreignBody)
		}
		if foreignStatus != missingStatus {
			t.Fatalf("%s: another owner's id answered %d, a never-existing id %d", testCase.method, foreignStatus, missingStatus)
		}
		if normalizeCycleDayCardBody(foreignBody) != normalizeCycleDayCardBody(missingBody) {
			t.Fatalf("%s: another owner's id and a never-existing id must answer identically\n--- foreign ---\n%s\n--- missing ---\n%s", testCase.method, foreignBody, missingBody)
		}
	}

	stored := listCycleDayRulesForTest(t, owner, other.ID)
	if len(stored) != 1 || stored[0].CycleDay != 12 || stored[0].RemindAtHour != 7 || stored[0].Label != "theirs" {
		t.Fatalf("the other owner's rule must be untouched, got %+v", stored)
	}
}

func TestCycleDayReminderInvalidDayAnswersTheCardWithTheRefusal(t *testing.T) {
	owner := newSettingsSecurityTestContext(t, "cdr-api-invalid@example.com")
	invalidDay := mustEnglishMessages(t)["settings.error.cycle_day_reminder_invalid_day"]
	if invalidDay == "" {
		t.Fatal("the invalid-day message is missing from the catalogue")
	}

	for _, day := range []string{"0", "91"} {
		_, body := sendCycleDayReminderForm(t, owner, http.MethodPost, cycleDayRemindersPath, "cycle_day="+day+"&remind_at_hour=9", true)
		if !strings.Contains(body, `id="settings-egress"`) || !strings.Contains(body, invalidDay) {
			t.Fatalf("day %s must answer the card with the invalid-day message: %s", day, body)
		}
	}
	if rules := listCycleDayRulesForTest(t, owner, owner.user.ID); len(rules) != 0 {
		t.Fatalf("an invalid day must store nothing, got %+v", rules)
	}
}

func TestCycleDayReminderRoutesRequireCSRFAndAnOwner(t *testing.T) {
	owner := newSettingsSecurityTestContext(t, "cdr-api-csrf@example.com")
	mine := models.CycleDayReminder{UserID: owner.user.ID, CycleDay: 3, RemindAtHour: 9}
	if err := owner.database.Create(&mine).Error; err != nil {
		t.Fatalf("seed rule: %v", err)
	}
	rulePath := fmt.Sprintf("%s/%d", cycleDayRemindersPath, mine.ID)

	for _, call := range []struct {
		method string
		path   string
		form   string
	}{
		{http.MethodPost, cycleDayRemindersPath, "cycle_day=15&remind_at_hour=9"},
		{http.MethodPatch, rulePath, "cycle_day=16&remind_at_hour=9"},
		{http.MethodDelete, rulePath, ""},
	} {
		status, _ := sendCycleDayReminderForm(t, owner, call.method, call.path, call.form, false)
		if status != http.StatusForbidden {
			t.Fatalf("%s %s without a csrf token: expected 403, got %d", call.method, call.path, status)
		}

		request := httptest.NewRequest(call.method, call.path, strings.NewReader(call.form))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Accept", "application/json")
		response, err := owner.app.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("anonymous %s %s: %v", call.method, call.path, err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized && response.StatusCode != http.StatusForbidden {
			t.Fatalf("anonymous %s %s: expected a refusal, got %d", call.method, call.path, response.StatusCode)
		}
	}

	rules := listCycleDayRulesForTest(t, owner, owner.user.ID)
	if len(rules) != 1 || rules[0].CycleDay != 3 {
		t.Fatalf("refused requests must write nothing, got %+v", rules)
	}
}

// newCycleDayReminderHandlerApp mounts the three handlers without the routed
// middleware, as mutation_error_branch_coverage_test.go does: the handler-level
// currentUser guard and every parse/map branch are reached directly.
// withUser=false mounts no current user.
func newCycleDayReminderHandlerApp(t *testing.T, withUser bool) (*fiber.App, *gorm.DB, models.User) {
	t.Helper()
	handler, database := newDataAccessTestHandler(t)
	user := createOnboardingTestUser(t, database, "cdr-handler@example.com", "StrongPass1", true)
	app := fiber.New()
	if withUser {
		app.Use(func(c fiber.Ctx) error {
			current := user
			c.Locals(contextUserKey, &current)
			return c.Next()
		})
	}
	app.Post(cycleDayRemindersPath, handler.CreateCycleDayReminder)
	app.Patch(cycleDayRemindersPath+"/:id", handler.UpdateCycleDayReminder)
	app.Delete(cycleDayRemindersPath+"/:id", handler.DeleteCycleDayReminder)
	return app, database, user
}

func TestCycleDayReminderHandlersRejectAMissingUserAtHandlerLevel(t *testing.T) {
	app, database, _ := newCycleDayReminderHandlerApp(t, false)
	for _, call := range []struct{ method, path string }{
		{http.MethodPost, cycleDayRemindersPath},
		{http.MethodPatch, cycleDayRemindersPath + "/1"},
		{http.MethodDelete, cycleDayRemindersPath + "/1"},
	} {
		response := mutationBranchRequest(t, app, call.method, call.path, "cycle_day=5&remind_at_hour=9", "application/x-www-form-urlencoded")
		_ = response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s %s without user: expected 401, got %d", call.method, call.path, response.StatusCode)
		}
	}
	var count int64
	if err := database.Model(&models.CycleDayReminder{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("a refused request must write nothing, count=%d err=%v", count, err)
	}
}

func TestCycleDayReminderHandlersMapEachRefusal(t *testing.T) {
	app, database, user := newCycleDayReminderHandlerApp(t, true)
	const form = "application/x-www-form-urlencoded"
	const jsonType = "application/json"

	send := func(method, path, body, contentType string) int {
		t.Helper()
		response := mutationBranchRequest(t, app, method, path, body, contentType)
		_ = response.Body.Close()
		return response.StatusCode
	}

	if status := send(http.MethodPost, cycleDayRemindersPath, `{"cycle_day":5,"remind_at_hour":9,"label":"iron","send_label":true}`, jsonType); status != http.StatusOK {
		t.Fatalf("a JSON create: expected 200, got %d", status)
	}
	stored := []models.CycleDayReminder{}
	if err := database.Where("user_id = ?", user.ID).Find(&stored).Error; err != nil || len(stored) != 1 ||
		stored[0].CycleDay != 5 || stored[0].RemindAtHour != 9 || stored[0].Label != "iron" || !stored[0].SendLabel {
		t.Fatalf("the JSON body must be stored as sent, got %+v err=%v", stored, err)
	}
	ruleID := stored[0].ID
	rulePath := fmt.Sprintf("%s/%d", cycleDayRemindersPath, ruleID)

	cases := []struct {
		name        string
		method      string
		path        string
		body        string
		contentType string
		want        int
	}{
		{"create non-numeric day", http.MethodPost, cycleDayRemindersPath, "cycle_day=five&remind_at_hour=9", form, http.StatusBadRequest},
		{"create non-numeric hour", http.MethodPost, cycleDayRemindersPath, "cycle_day=5&remind_at_hour=nine", form, http.StatusBadRequest},
		{"create hour out of range", http.MethodPost, cycleDayRemindersPath, "cycle_day=5&remind_at_hour=24", form, http.StatusBadRequest},
		{"create label too long", http.MethodPost, cycleDayRemindersPath, "cycle_day=6&remind_at_hour=9&label=" + strings.Repeat("a", models.CycleDayReminderLabelMaxRunes+1), form, http.StatusBadRequest},
		{"create duplicate day and hour", http.MethodPost, cycleDayRemindersPath, "cycle_day=5&remind_at_hour=9", form, http.StatusConflict},
		{"update non-numeric id", http.MethodPatch, cycleDayRemindersPath + "/garbage", "cycle_day=5&remind_at_hour=9", form, http.StatusNotFound},
		{"update invalid day", http.MethodPatch, rulePath, "cycle_day=0&remind_at_hour=9", form, http.StatusBadRequest},
		{"delete non-numeric id", http.MethodDelete, cycleDayRemindersPath + "/garbage", "", "", http.StatusNotFound},
		{"delete never-existing id", http.MethodDelete, fmt.Sprintf("%s/%d", cycleDayRemindersPath, ruleID+1000), "", "", http.StatusNotFound},
	}
	for _, testCase := range cases {
		if status := send(testCase.method, testCase.path, testCase.body, testCase.contentType); status != testCase.want {
			t.Fatalf("%s: expected %d, got %d", testCase.name, testCase.want, status)
		}
	}

	// A body that is not a rule is the caller's fault: 400, never a 500.
	if status := send(http.MethodPost, cycleDayRemindersPath, "{", jsonType); status != http.StatusBadRequest {
		t.Fatalf("a malformed JSON body must be refused as invalid input, got %d", status)
	}

	if status := send(http.MethodPatch, rulePath, "cycle_day=6&remind_at_hour=10&label=zinc&send_label=on", form); status != http.StatusOK {
		t.Fatalf("an update of the owner's rule: expected 200, got %d", status)
	}
	for day := 20; len(listCycleDayRulesForHandlerTest(t, database, user.ID)) < models.CycleDayReminderMaxPerOwner; day++ {
		if err := database.Create(&models.CycleDayReminder{UserID: user.ID, CycleDay: day, RemindAtHour: 9}).Error; err != nil {
			t.Fatalf("seed rule: %v", err)
		}
	}
	if status := send(http.MethodPost, cycleDayRemindersPath, "cycle_day=80&remind_at_hour=9", form); status != http.StatusConflict {
		t.Fatalf("a create past the cap: expected 409, got %d", status)
	}
	if status := send(http.MethodDelete, rulePath, "", ""); status != http.StatusOK {
		t.Fatalf("a delete of the owner's rule: expected 200, got %d", status)
	}

	remaining := listCycleDayRulesForHandlerTest(t, database, user.ID)
	if len(remaining) != models.CycleDayReminderMaxPerOwner-1 {
		t.Fatalf("expected the cap minus the deleted rule, got %d", len(remaining))
	}
	for _, rule := range remaining {
		if rule.ID == ruleID {
			t.Fatalf("the deleted rule is still stored: %+v", rule)
		}
	}
}

func listCycleDayRulesForHandlerTest(t *testing.T, database *gorm.DB, userID uint) []models.CycleDayReminder {
	t.Helper()
	var rules []models.CycleDayReminder
	if err := database.Where("user_id = ?", userID).Order("id ASC").Find(&rules).Error; err != nil {
		t.Fatalf("list rules: %v", err)
	}
	return rules
}
