package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sort"
	"strconv"
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

var wireDateOnlyPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// TestDayAndSymptomJSONUseSnakeCaseWireKeys is the public-contract lock for the
// /api/v1/days and /api/v1/symptoms JSON surfaces. The handlers serialize
// transport DTOs (api.dayResponse / api.symptomResponse), not the raw GORM
// models, so the wire format must expose the snake_case keys documented in
// docs/openapi.yaml (DailyLog / Symptom schemas) and must never leak the
// PascalCase Go field names of models.DailyLog / models.SymptomType. `date`
// must be a calendar date (format: date), not an RFC3339 timestamp.
func TestDayAndSymptomJSONUseSnakeCaseWireKeys(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "wire-keys-day-symptom@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	// Seed a fully-populated day directly so every documented field is present
	// on the wire regardless of owner hidden-field preferences.
	day := time.Date(2026, time.May, 17, 0, 0, 0, 0, time.UTC)
	seed := models.DailyLog{
		UserID:          user.ID,
		Date:            day,
		IsPeriod:        true,
		CycleStart:      true,
		IsUncertain:     true,
		Flow:            models.FlowMedium,
		Mood:            3,
		SexActivity:     models.SexActivityProtected,
		BBT:             36.6,
		CervicalMucus:   models.CervicalMucusCreamy,
		PregnancyTest:   models.PregnancyTestNegative,
		CycleFactorKeys: []string{"stress"},
		SymptomIDs:      []uint{},
		Notes:           "wire-key note",
	}
	if err := database.Create(&seed).Error; err != nil {
		t.Fatalf("seed daily log: %v", err)
	}

	dayKeys := decodeJSONObjectKeys(t, app, authCookie, "/api/v1/days/2026-05-17")
	assertWireKeysPresent(t, dayKeys, "day", []string{
		"id", "user_id", "date", "is_period", "cycle_start", "is_uncertain",
		"flow", "mood", "sex_activity", "bbt", "cervical_mucus", "pregnancy_test",
		"cycle_factor_keys", "symptom_ids", "notes", "created_at", "updated_at",
	})
	assertWireKeysAbsent(t, dayKeys, "day", []string{
		"IsPeriod", "CycleStart", "IsUncertain", "SexActivity", "CervicalMucus",
		"PregnancyTest", "CycleFactorKeys", "SymptomIDs",
	})
	assertWireDateOnly(t, dayKeys, "day", "/api/v1/days/2026-05-17", "2026-05-17")

	// FetchSymptoms backfills builtin symptoms, so the list is always non-empty
	// and includes is_builtin=true rows whose archived_at is null.
	symptomKeys := decodeJSONArrayFirstObjectKeys(t, app, authCookie, "/api/v1/symptoms")
	assertWireKeysPresent(t, symptomKeys, "symptom", []string{
		"id", "user_id", "name", "icon", "color", "is_builtin", "archived_at",
	})
	assertWireKeysAbsent(t, symptomKeys, "symptom", []string{
		"UserID", "IsBuiltin", "ArchivedAt",
	})

	// Pin the symptom update JSON branch (PATCH content-negotiation) to the same
	// snake_case DTO contract. A no-CSRF app is appropriate here: the test
	// isolates content negotiation, while the valid-token CSRF update path is
	// covered by the settings symptom HTMX regressions and the missing-token 403
	// by state_mutation_csrf_missing_token_test.go (testing.md content-negotiation
	// rule).
	custom := models.SymptomType{
		UserID:    user.ID,
		Name:      "Wire custom symptom",
		Icon:      "🌟",
		Color:     "#AABBCC",
		IsBuiltin: false,
	}
	if err := database.Create(&custom).Error; err != nil {
		t.Fatalf("seed custom symptom: %v", err)
	}
	updatedKeys := patchSymptomJSON(t, app, authCookie, custom.ID, url.Values{
		"name":  {"Wire custom renamed"},
		"icon":  {"⭐"},
		"color": {"#112233"},
	})
	assertWireKeysPresent(t, updatedKeys, "symptom-update", []string{
		"id", "user_id", "name", "icon", "color", "is_builtin", "archived_at",
	})
	assertWireKeysAbsent(t, updatedKeys, "symptom-update", []string{
		"UserID", "IsBuiltin", "ArchivedAt",
	})
}

// decodeJSONObjectKeys issues an Accept: application/json GET and returns the
// top-level object decoded as raw-message keys so tests can assert exact wire
// key names without binding to a Go struct.
func decodeJSONObjectKeys(t *testing.T, app *fiber.App, authCookie, path string) map[string]json.RawMessage {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: expected status 200, got %d", path, response.StatusCode)
	}
	var keys map[string]json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&keys); err != nil {
		t.Fatalf("decode %s object: %v", path, err)
	}
	return keys
}

// decodeJSONArrayFirstObjectKeys issues an Accept: application/json GET against
// a list endpoint and returns the first element's raw-message keys.
func decodeJSONArrayFirstObjectKeys(t *testing.T, app *fiber.App, authCookie, path string) map[string]json.RawMessage {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: expected status 200, got %d", path, response.StatusCode)
	}
	var list []map[string]json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&list); err != nil {
		t.Fatalf("decode %s array: %v", path, err)
	}
	if len(list) == 0 {
		t.Fatalf("GET %s: expected at least one element, got empty array", path)
	}
	return list[0]
}

// patchSymptomJSON updates a symptom via PATCH with Accept: application/json and
// returns the response object's raw-message keys, so the update wire contract is
// asserted the same way as the read endpoints.
func patchSymptomJSON(t *testing.T, app *fiber.App, authCookie string, id uint, form url.Values) map[string]json.RawMessage {
	t.Helper()
	path := "/api/v1/symptoms/" + strconv.FormatUint(uint64(id), 10)
	request := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("PATCH %s: expected status 200, got %d", path, response.StatusCode)
	}
	var keys map[string]json.RawMessage
	if err := json.NewDecoder(response.Body).Decode(&keys); err != nil {
		t.Fatalf("decode %s object: %v", path, err)
	}
	return keys
}

func assertWireKeysPresent(t *testing.T, keys map[string]json.RawMessage, label string, expected []string) {
	t.Helper()
	for _, key := range expected {
		if _, ok := keys[key]; !ok {
			t.Fatalf("%s JSON: expected snake_case key %q to be present, got keys %v", label, key, sortedKeys(keys))
		}
	}
}

func assertWireKeysAbsent(t *testing.T, keys map[string]json.RawMessage, label string, forbidden []string) {
	t.Helper()
	for _, key := range forbidden {
		if _, ok := keys[key]; ok {
			t.Fatalf("%s JSON: PascalCase Go field %q leaked onto the wire; serialize the DTO, not the model", label, key)
		}
	}
}

func assertWireDateOnly(t *testing.T, keys map[string]json.RawMessage, label, path, expected string) {
	t.Helper()
	raw, ok := keys["date"]
	if !ok {
		t.Fatalf("%s JSON %s: missing date key", label, path)
	}
	var dateValue string
	if err := json.Unmarshal(raw, &dateValue); err != nil {
		t.Fatalf("%s JSON %s: decode date value: %v", label, path, err)
	}
	if !wireDateOnlyPattern.MatchString(dateValue) {
		t.Fatalf("%s JSON %s: expected date-only value matching YYYY-MM-DD, got %q (a timestamp would break openapi format: date)", label, path, dateValue)
	}
	if dateValue != expected {
		t.Fatalf("%s JSON %s: expected date %q, got %q", label, path, expected, dateValue)
	}
}

func sortedKeys(keys map[string]json.RawMessage) []string {
	out := make([]string, 0, len(keys))
	for key := range keys {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
