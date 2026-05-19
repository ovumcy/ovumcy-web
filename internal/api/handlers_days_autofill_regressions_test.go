package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

func TestUpsertDayAutoFillCanBeDisabled(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "upsert-day-autofill-disabled@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"period_length":    4,
		"auto_period_fill": false,
	}).Error; err != nil {
		t.Fatalf("update user cycle settings: %v", err)
	}

	payload := map[string]any{
		"is_period":   true,
		"flow":        models.FlowMedium,
		"symptom_ids": []uint{},
		"notes":       "",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/2026-02-10", bytes.NewReader(body))
	request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("upsert request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	firstDay, err := services.ParseDayDate("2026-02-10", time.UTC)
	if err != nil {
		t.Fatalf("parse first day: %v", err)
	}
	firstEntry, err := fetchLogByDateForTest(database, user.ID, firstDay, time.UTC)
	if err != nil {
		t.Fatalf("fetch first day log: %v", err)
	}
	if !firstEntry.IsPeriod {
		t.Fatalf("expected first selected day to be period")
	}

	nextDay, err := services.ParseDayDate("2026-02-11", time.UTC)
	if err != nil {
		t.Fatalf("parse next day: %v", err)
	}
	nextEntry, err := fetchLogByDateForTest(database, user.ID, nextDay, time.UTC)
	if err != nil {
		t.Fatalf("fetch next day log: %v", err)
	}
	if nextEntry.IsPeriod {
		t.Fatalf("expected next day to stay manual when auto-fill is disabled")
	}
}

func TestUpsertDayAutoFillDoesNotCreateFutureDays(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "upsert-day-autofill-future-guard@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"period_length":    4,
		"auto_period_fill": true,
	}).Error; err != nil {
		t.Fatalf("seed autofill settings: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	todayRaw := today.Format("2006-01-02")

	form := url.Values{
		"is_period": {"true"},
		"flow":      {models.FlowLight},
	}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/"+todayRaw, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	tomorrow := today.AddDate(0, 0, 1)
	entry, err := fetchLogByDateForTest(database, user.ID, tomorrow, time.UTC)
	if err != nil {
		t.Fatalf("load tomorrow entry after autofill attempt: %v", err)
	}
	if entry.ID != 0 {
		t.Fatalf("did not expect autofill to create future entry for %s", tomorrow.Format("2006-01-02"))
	}
}

func TestUpsertDayAutoFillsFollowingPeriodDays(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "upsert-day-autofill@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"period_length":    4,
		"auto_period_fill": true,
	}).Error; err != nil {
		t.Fatalf("update user cycle settings: %v", err)
	}

	payload := map[string]any{
		"is_period":   true,
		"flow":        models.FlowMedium,
		"symptom_ids": []uint{},
		"notes":       "",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/2026-02-10", bytes.NewReader(body))
	request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("upsert request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	autoFilledDays := []string{"2026-02-10", "2026-02-11", "2026-02-12", "2026-02-13"}
	for _, dateRaw := range autoFilledDays {
		day, err := services.ParseDayDate(dateRaw, time.UTC)
		if err != nil {
			t.Fatalf("parse day %s: %v", dateRaw, err)
		}
		entry, err := fetchLogByDateForTest(database, user.ID, day, time.UTC)
		if err != nil {
			t.Fatalf("fetch log for %s: %v", dateRaw, err)
		}
		if !entry.IsPeriod {
			t.Fatalf("expected %s to be auto-marked as period day", dateRaw)
		}
	}

	dayAfterAutoFill, err := services.ParseDayDate("2026-02-14", time.UTC)
	if err != nil {
		t.Fatalf("parse day after auto-fill: %v", err)
	}
	dayAfterEntry, err := fetchLogByDateForTest(database, user.ID, dayAfterAutoFill, time.UTC)
	if err != nil {
		t.Fatalf("fetch log for day after auto-fill: %v", err)
	}
	if dayAfterEntry.IsPeriod {
		t.Fatalf("expected day after auto-fill window to remain non-period")
	}
}

func TestUpsertDayAutoFillSkipsWhenRecentPeriodDayExists(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "upsert-day-autofill-recent-period@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"period_length":    4,
		"auto_period_fill": true,
	}).Error; err != nil {
		t.Fatalf("update user cycle settings: %v", err)
	}

	existingPeriodDay := time.Date(2026, time.February, 8, 0, 0, 0, 0, time.UTC)
	logEntry := models.DailyLog{
		UserID:     user.ID,
		Date:       existingPeriodDay,
		IsPeriod:   true,
		Flow:       models.FlowMedium,
		SymptomIDs: []uint{},
	}
	if err := database.Create(&logEntry).Error; err != nil {
		t.Fatalf("create existing period day: %v", err)
	}

	payload := map[string]any{
		"is_period":   true,
		"flow":        models.FlowMedium,
		"symptom_ids": []uint{},
		"notes":       "",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/2026-02-10", bytes.NewReader(body))
	request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("upsert request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	nextDay, err := services.ParseDayDate("2026-02-11", time.UTC)
	if err != nil {
		t.Fatalf("parse next day: %v", err)
	}
	nextEntry, err := fetchLogByDateForTest(database, user.ID, nextDay, time.UTC)
	if err != nil {
		t.Fatalf("fetch next day log: %v", err)
	}
	if nextEntry.IsPeriod {
		t.Fatalf("expected recent-period guard to prevent a new auto-fill sequence")
	}
}

func TestUpsertDayAutoFillClearsBareNeighborsWhenAnchorToggledOff(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "upsert-day-autofill-clear@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"period_length":    4,
		"auto_period_fill": true,
	}).Error; err != nil {
		t.Fatalf("update user cycle settings: %v", err)
	}

	putDayPayloadExpectOK(t, app, authCookie, "2026-02-10", map[string]any{
		"is_period":   true,
		"flow":        models.FlowMedium,
		"symptom_ids": []uint{},
		"notes":       "",
	}, "toggle on")

	for _, dateRaw := range []string{"2026-02-11", "2026-02-12", "2026-02-13"} {
		assertDayPeriodState(t, database, user.ID, dateRaw, true, "auto-marked as period day before toggle off")
	}

	putDayPayloadExpectOK(t, app, authCookie, "2026-02-10", map[string]any{
		"is_period":   false,
		"flow":        models.FlowNone,
		"symptom_ids": []uint{},
		"notes":       "",
	}, "toggle off")

	for _, dateRaw := range []string{"2026-02-10", "2026-02-11", "2026-02-12", "2026-02-13"} {
		assertDayPeriodState(t, database, user.ID, dateRaw, false, "cleared after anchor toggle off")
	}
}

// putDayPayloadExpectOK marshals payload, fires PUT /api/v1/days/{date}, and
// fails the test on any error or non-200. Extracted to keep parent test
// cyclomatic complexity below the gocyclo gate.
func putDayPayloadExpectOK(t *testing.T, app *fiber.App, authCookie, dateISO string, payload map[string]any, label string) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal %s payload: %v", label, err)
	}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/"+dateISO, bytes.NewReader(body))
	request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	request.Header.Set("Cookie", authCookie)
	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("%s request failed: %v", label, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 on %s, got %d", label, response.StatusCode)
	}
}

// assertDayPeriodState parses dateRaw, fetches the user's daily log, and
// asserts IsPeriod matches expectPeriod. Provides a uniform failure message
// keyed on the date so each per-day branch in the test stays a single line.
func assertDayPeriodState(t *testing.T, database *gorm.DB, userID uint, dateRaw string, expectPeriod bool, reason string) {
	t.Helper()
	day, err := services.ParseDayDate(dateRaw, time.UTC)
	if err != nil {
		t.Fatalf("parse day %s: %v", dateRaw, err)
	}
	entry, err := fetchLogByDateForTest(database, userID, day, time.UTC)
	if err != nil {
		t.Fatalf("fetch log for %s: %v", dateRaw, err)
	}
	if entry.IsPeriod != expectPeriod {
		t.Fatalf("expected %s %s (IsPeriod=%v), got IsPeriod=%v", dateRaw, reason, expectPeriod, entry.IsPeriod)
	}
}

func TestUpsertDayAutoFillPreservesManualNeighborsWhenAnchorToggledOff(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "upsert-day-autofill-preserve@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"period_length":    5,
		"auto_period_fill": true,
	}).Error; err != nil {
		t.Fatalf("update user cycle settings: %v", err)
	}

	onPayload := map[string]any{
		"is_period":   true,
		"flow":        models.FlowMedium,
		"symptom_ids": []uint{},
		"notes":       "",
	}
	onBody, _ := json.Marshal(onPayload)
	onRequest := httptest.NewRequest(http.MethodPut, "/api/v1/days/2026-02-10", bytes.NewReader(onBody))
	onRequest.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	onRequest.Header.Set("Cookie", authCookie)
	onResponse, _ := app.Test(onRequest, -1)
	defer onResponse.Body.Close()

	manualDay, _ := services.ParseDayDate("2026-02-12", time.UTC)
	manualEntry, err := fetchLogByDateForTest(database, user.ID, manualDay, time.UTC)
	if err != nil {
		t.Fatalf("fetch manual day: %v", err)
	}
	manualEntry.Notes = "cramps reminder"
	if err := database.Save(&manualEntry).Error; err != nil {
		t.Fatalf("save manual annotation: %v", err)
	}

	offPayload := map[string]any{
		"is_period":   false,
		"flow":        models.FlowNone,
		"symptom_ids": []uint{},
		"notes":       "",
	}
	offBody, _ := json.Marshal(offPayload)
	offRequest := httptest.NewRequest(http.MethodPut, "/api/v1/days/2026-02-10", bytes.NewReader(offBody))
	offRequest.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	offRequest.Header.Set("Cookie", authCookie)
	offResponse, err := app.Test(offRequest, -1)
	if err != nil {
		t.Fatalf("off request failed: %v", err)
	}
	defer offResponse.Body.Close()
	if offResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 on toggle off, got %d", offResponse.StatusCode)
	}

	day11, _ := services.ParseDayDate("2026-02-11", time.UTC)
	day11Entry, err := fetchLogByDateForTest(database, user.ID, day11, time.UTC)
	if err != nil {
		t.Fatalf("fetch day 11: %v", err)
	}
	if day11Entry.IsPeriod {
		t.Fatalf("expected day 11 to be cleared, got IsPeriod=true")
	}

	day12, _ := services.ParseDayDate("2026-02-12", time.UTC)
	day12Entry, err := fetchLogByDateForTest(database, user.ID, day12, time.UTC)
	if err != nil {
		t.Fatalf("fetch day 12: %v", err)
	}
	if !day12Entry.IsPeriod {
		t.Fatalf("expected day 12 with manual notes to be preserved as period")
	}
	if day12Entry.Notes != "cramps reminder" {
		t.Fatalf("expected manual notes to be preserved on day 12, got %q", day12Entry.Notes)
	}

	day13, _ := services.ParseDayDate("2026-02-13", time.UTC)
	day13Entry, err := fetchLogByDateForTest(database, user.ID, day13, time.UTC)
	if err != nil {
		t.Fatalf("fetch day 13: %v", err)
	}
	if !day13Entry.IsPeriod {
		t.Fatalf("expected clearing to stop at the first manual day; day 13 should remain period")
	}
}
