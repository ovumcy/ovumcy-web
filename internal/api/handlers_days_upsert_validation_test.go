package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func TestUpsertDayNormalizesFlowWhenNotPeriod(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "upsert-day-normalize@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	payload := map[string]any{
		"is_period":   false,
		"flow":        models.FlowHeavy,
		"symptom_ids": []uint{},
		"notes":       "note",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/2026-02-19", bytes.NewReader(body))
	request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("upsert request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	day, err := services.ParseDayDate("2026-02-19", time.UTC)
	if err != nil {
		t.Fatalf("parse day for assertion: %v", err)
	}
	entry, err := fetchLogByDateForTest(database, user.ID, day, time.UTC)
	if err != nil {
		t.Fatalf("load stored log: %v", err)
	}
	if entry.Flow != models.FlowNone {
		t.Fatalf("expected non-period flow normalized to %q, got %q", models.FlowNone, entry.Flow)
	}
}

func TestUpsertDayAllowsPeriodWithoutExplicitFlow(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "upsert-day-invalid-flow@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	payload := map[string]any{
		"is_period":   true,
		"flow":        models.FlowNone,
		"symptom_ids": []uint{},
		"notes":       "note",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/2026-02-19", bytes.NewReader(body))
	request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("upsert request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	day, err := services.ParseDayDate("2026-02-19", time.UTC)
	if err != nil {
		t.Fatalf("parse day for assertion: %v", err)
	}
	entry, err := fetchLogByDateForTest(database, user.ID, day, time.UTC)
	if err != nil {
		t.Fatalf("load stored log: %v", err)
	}
	if !entry.IsPeriod {
		t.Fatal("expected period day to persist when flow is none")
	}
	if entry.Flow != models.FlowNone {
		t.Fatalf("expected stored flow %q, got %q", models.FlowNone, entry.Flow)
	}
}

func TestUpsertDayPreservesSymptomsWhenNotPeriod(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "upsert-day-clear-symptoms@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	symptom := models.SymptomType{
		UserID:    user.ID,
		Name:      "Cramps",
		Icon:      "🩸",
		Color:     "#FF4444",
		IsBuiltin: true,
	}
	if err := database.Create(&symptom).Error; err != nil {
		t.Fatalf("create symptom: %v", err)
	}

	payload := map[string]any{
		"is_period":   false,
		"flow":        models.FlowLight,
		"symptom_ids": []uint{symptom.ID},
		"notes":       "note",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/2026-02-20", bytes.NewReader(body))
	request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("upsert request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	day, err := services.ParseDayDate("2026-02-20", time.UTC)
	if err != nil {
		t.Fatalf("parse day for assertion: %v", err)
	}
	entry, err := fetchLogByDateForTest(database, user.ID, day, time.UTC)
	if err != nil {
		t.Fatalf("load stored log: %v", err)
	}
	if len(entry.SymptomIDs) != 1 || entry.SymptomIDs[0] != symptom.ID {
		t.Fatalf("expected symptoms to stay persisted for non-period day, got %v", entry.SymptomIDs)
	}
}

func TestUpsertDayPersistsPregnancyTest(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "upsert-day-pregnancy-test@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	payload := map[string]any{
		"is_period":      false,
		"flow":           models.FlowNone,
		"pregnancy_test": models.PregnancyTestPositive,
		"symptom_ids":    []uint{},
		"notes":          "note",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/2026-02-21", bytes.NewReader(body))
	request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("upsert request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	day, err := services.ParseDayDate("2026-02-21", time.UTC)
	if err != nil {
		t.Fatalf("parse day for assertion: %v", err)
	}
	entry, err := fetchLogByDateForTest(database, user.ID, day, time.UTC)
	if err != nil {
		t.Fatalf("load stored log: %v", err)
	}
	if entry.PregnancyTest != models.PregnancyTestPositive {
		t.Fatalf("expected stored pregnancy test %q, got %q", models.PregnancyTestPositive, entry.PregnancyTest)
	}
}

// TestUpsertDayPersistsLongPeriodWarningAcknowledgement pins the
// `feedbackErr == nil` operand of handlers_days_write.go:113
// (`if feedbackErr == nil && feedback.ShowLongPeriodWarning && !feedback.LongPeriodCycleStart.IsZero()`),
// which guards persisting the long-period-warning acknowledgement. Seeding an
// 8-day period streak and upserting the ninth consecutive period day makes
// ResolveDayFeedback return ShowLongPeriodWarning=true with a non-zero cycle start,
// so the handler must persist LongPeriodWarningCycleStart (column
// long_period_warning_cycle_start) to suppress the warning on later saves. The
// CONDITIONALS_NEGATION mutant (`feedbackErr != nil`) skips the ack block on the
// happy path, leaving the column NULL and re-showing the warning on every save.
func TestUpsertDayPersistsLongPeriodWarningAcknowledgement(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "upsert-day-longperiod-ack@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	// Disable auto-fill so the upsert persists exactly the posted day, keeping the
	// consecutive-period streak deterministic (seeded 8 + posted 1 = 9 > 8).
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("auto_period_fill", false).Error; err != nil {
		t.Fatalf("disable auto period fill: %v", err)
	}

	// Seed the first eight days of a long-period streak (2026-03-01..2026-03-08) as
	// UTC-midnight period days; the ninth is posted below.
	cycleStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	for offset := range 8 {
		day := cycleStart.AddDate(0, 0, offset)
		entry := models.DailyLog{
			UserID:   user.ID,
			Date:     day,
			IsPeriod: true,
			Flow:     models.FlowMedium,
		}
		if err := database.Create(&entry).Error; err != nil {
			t.Fatalf("seed period day %s: %v", day.Format("2006-01-02"), err)
		}
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

	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/2026-03-09", bytes.NewReader(body))
	request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("upsert request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	var persisted models.User
	if err := database.Select("long_period_warning_cycle_start").First(&persisted, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if persisted.LongPeriodWarningCycleStart == nil {
		t.Fatal("expected long-period-warning acknowledgement to be persisted after a nine-day period streak upsert (feedbackErr==nil guard must run the ack)")
	}
	if got := services.CalendarDayKey(*persisted.LongPeriodWarningCycleStart); got != "2026-03-01" {
		t.Fatalf("expected acknowledged cycle start 2026-03-01, got %s", got)
	}
}

func TestUpsertDayNormalizesUnknownPregnancyTest(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "upsert-day-pregnancy-normalize@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	payload := map[string]any{
		"is_period":      false,
		"flow":           models.FlowNone,
		"pregnancy_test": "bogus-value",
		"symptom_ids":    []uint{},
		"notes":          "note",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/2026-02-22", bytes.NewReader(body))
	request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("upsert request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	day, err := services.ParseDayDate("2026-02-22", time.UTC)
	if err != nil {
		t.Fatalf("parse day for assertion: %v", err)
	}
	entry, err := fetchLogByDateForTest(database, user.ID, day, time.UTC)
	if err != nil {
		t.Fatalf("load stored log: %v", err)
	}
	if entry.PregnancyTest != models.PregnancyTestNone {
		t.Fatalf("expected unknown pregnancy test normalized to %q, got %q", models.PregnancyTestNone, entry.PregnancyTest)
	}
}

// invalidDayBBTErrorKey is the key mapDayUpsertError publishes for
// services.ErrInvalidDayBBT; the refusals below assert the owner is told which
// field lost the save, not merely that one was lost.
const invalidDayBBTErrorKey = "invalid bbt value"

// TestUpsertDayJudgesTheNotMeasuredSentinelInTheAccountsUnit walks both
// transports of the day upsert for an account tracking in Fahrenheit: a
// non-positive entry is the owner's "not measured" answer and saves an empty
// temperature, a positive one is a reading the physiological range judges, so
// 20 °F and 32 °F are refused rather than filed as a day with no temperature on
// it. Why the sentinel is read before the conversion rather than after:
// services.ConvertDayBBTToStorage.
//
// Both transports are swept because they reach that conversion by different
// routes — the JSON bind hands over a parsed float, the form its own string
// parse — and a fix applied to one of them leaves the other saving nothing.
func TestUpsertDayJudgesTheNotMeasuredSentinelInTheAccountsUnit(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "upsert-day-bbt-sentinel-unit@example.com", "StrongPass1", true)
	updateStatsOverviewUser(t, database, user, fahrenheitBBTTrackingPreferences(true))
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	const storedTolerance = 1e-9

	testCases := []struct {
		name        string
		typed       string
		wantStatus  int
		wantReading bool
		wantStored  float64
	}{
		{name: "impossible positive reading is refused", typed: "20", wantStatus: http.StatusBadRequest},
		{name: "freezing point is refused", typed: "32", wantStatus: http.StatusBadRequest},
		{name: "zero is not measured", typed: "0", wantStatus: http.StatusOK},
		{name: "negative is not measured", typed: "-1", wantStatus: http.StatusOK},
		{name: "ordinary reading is stored in celsius", typed: "97.7", wantStatus: http.StatusOK, wantReading: true, wantStored: 36.5},
	}

	firstDay := time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
	for transportIndex, transport := range []string{"json", "form"} {
		for caseIndex, testCase := range testCases {
			day := firstDay.AddDate(0, 0, transportIndex*len(testCases)+caseIndex)
			dayRaw := day.Format("2006-01-02")

			t.Run(transport+"/"+testCase.name, func(t *testing.T) {
				var request *http.Request
				if transport == "json" {
					body := fmt.Sprintf(`{"is_period":false,"flow":%q,"symptom_ids":[],"notes":"","bbt":%s}`, models.FlowNone, testCase.typed)
					request = httptest.NewRequest(http.MethodPut, "/api/v1/days/"+dayRaw, strings.NewReader(body))
					request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
				} else {
					form := url.Values{"flow": {models.FlowNone}, "bbt": {testCase.typed}}
					request = httptest.NewRequest(http.MethodPut, "/api/v1/days/"+dayRaw, strings.NewReader(form.Encode()))
					request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				}
				request.Header.Set("Cookie", authCookie)

				response := mustAppResponse(t, app, request)
				assertStatusCode(t, response, testCase.wantStatus)

				entry, err := fetchLogByDateForTest(database, user.ID, day, time.UTC)
				if err != nil {
					t.Fatalf("load stored log for %s: %v", dayRaw, err)
				}

				if testCase.wantStatus != http.StatusOK {
					if got := readAPIError(t, response.Body); got != invalidDayBBTErrorKey {
						t.Fatalf("%s °F must be refused under %q, got %q", testCase.typed, invalidDayBBTErrorKey, got)
					}
					if entry.ID != 0 {
						t.Fatalf("%s °F was refused, so %s must hold no entry at all; found one with bbt=%v", testCase.typed, dayRaw, entry.BBT)
					}
					return
				}
				if entry.ID == 0 {
					t.Fatalf("%s °F was accepted, so %s must hold the saved day", testCase.typed, dayRaw)
				}
				if !testCase.wantReading {
					if entry.BBT != nil {
						t.Fatalf("%s °F means not measured, got %.4f °C stored", testCase.typed, *entry.BBT)
					}
					return
				}
				if entry.BBT == nil {
					t.Fatalf("%s °F is a reading, got no stored temperature", testCase.typed)
				}
				if math.Abs(*entry.BBT-testCase.wantStored) > storedTolerance {
					t.Fatalf("%s °F: expected %.4f °C stored, got %.4f", testCase.typed, testCase.wantStored, *entry.BBT)
				}
			})
		}
	}
}

// TestUpsertDayRefusesAnImpossibleFahrenheitReadingOverHTMX runs one of those
// refusals through the arm the day form itself uses. apiError answers an HTMX
// request with the shared status fragment rather than the JSON envelope, so the
// key travels as data-flash-key — a 400 that reached the panel without it would
// trade a named refusal for a silent one.
func TestUpsertDayRefusesAnImpossibleFahrenheitReadingOverHTMX(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "upsert-day-bbt-sentinel-htmx@example.com", "StrongPass1", true)
	updateStatsOverviewUser(t, database, user, fahrenheitBBTTrackingPreferences(true))
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	day := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	form := url.Values{"flow": {models.FlowNone}, "bbt": {"20"}}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/"+day.Format("2006-01-02"), strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusBadRequest)

	root := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	if htmlFlashByKey(root, "dashboard.error.invalid_bbt") == nil {
		t.Fatalf("expected the HTMX refusal to carry the bbt flash key")
	}

	entry, err := fetchLogByDateForTest(database, user.ID, day, time.UTC)
	if err != nil {
		t.Fatalf("load stored log: %v", err)
	}
	if entry.ID != 0 {
		t.Fatalf("a refused day must not be stored, found an entry with bbt=%v", entry.BBT)
	}
}

// TestUpsertDayRefusesAnImpossibleTemperatureSentAsJSON pins the transport
// condition the hidden-field handling below rests on: hiddenDayFields grants it
// to a FORM body only. A form is the browser day editor, which posts the fields
// the account shows and nothing else, so a value arriving in a hidden one is
// not this write's subject; a JSON body is a client replacing the record with
// exactly what it states, so every field it sends is its subject and is judged.
// The same account, the same 20 °F and the same day therefore answer 400 here
// and 200 in the table below, and the entry must not be created either way
// round. Widening the condition (`!hasJSONBody(c)` → true) reddens this test.
func TestUpsertDayRefusesAnImpossibleTemperatureSentAsJSON(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "upsert-day-bbt-hidden-json@example.com", "StrongPass1", true)
	updateStatsOverviewUser(t, database, user, fahrenheitBBTTrackingPreferences(false))
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	day := time.Date(2026, time.June, 10, 0, 0, 0, 0, time.UTC)
	body := fmt.Sprintf(`{"is_period":false,"flow":%q,"symptom_ids":[],"notes":"","bbt":20}`, models.FlowNone)
	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/"+day.Format("2006-01-02"), strings.NewReader(body))
	request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusBadRequest)
	if got := readAPIError(t, response.Body); got != invalidDayBBTErrorKey {
		t.Fatalf("a JSON body is granted no preservation, so 20 °F must be refused under %q, got %q", invalidDayBBTErrorKey, got)
	}

	entry, err := fetchLogByDateForTest(database, user.ID, day, time.UTC)
	if err != nil {
		t.Fatalf("load stored log: %v", err)
	}
	if entry.ID != 0 {
		t.Fatalf("a refused day must not be stored, found an entry with bbt=%v", entry.BBT)
	}
}

// TestUpsertDayDoesNotReadAHiddenTemperatureField drives an account that hides
// temperature — and tracks in Fahrenheit, where everything in (0, 32] °F is an
// impossible reading — through the form the browser posts. A field the account
// hides is not read at all (parseDayPayload consults hiddenDayFields), so
// nothing that can arrive in it reaches a parser or a validator: not a value
// the range refuses, not one strconv refuses, not one no float64 can hold. The
// day the owner did save has to land, and the temperature already stored has to
// come back untouched (mergePreservedDayEntryInput).
//
// The last row is the other half of the same rule: on a day that does not exist
// yet there is nothing to merge, so the hidden field starts empty rather than
// storing a reading the account cannot see or correct.
func TestUpsertDayDoesNotReadAHiddenTemperatureField(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "upsert-day-bbt-hidden-impossible@example.com", "StrongPass1", true)
	updateStatsOverviewUser(t, database, user, fahrenheitBBTTrackingPreferences(false))
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	const storedCelsius = 36.65

	testCases := []struct {
		name       string
		typed      string
		seedStored bool
	}{
		{name: "an impossible reading leaves the stored one alone", typed: "20", seedStored: true},
		{name: "an unparseable value cannot refuse the day", typed: "abc", seedStored: true},
		{name: "a magnitude no float64 holds cannot refuse the day", typed: "1e400", seedStored: true},
		{name: "a hidden field starts empty on a day that does not exist yet", typed: "20"},
	}

	firstDay := time.Date(2026, time.June, 2, 0, 0, 0, 0, time.UTC)
	for caseIndex, testCase := range testCases {
		day := firstDay.AddDate(0, 0, caseIndex)
		dayRaw := day.Format("2006-01-02")

		t.Run(testCase.name, func(t *testing.T) {
			if testCase.seedStored {
				stored := storedCelsius
				seeded := models.DailyLog{UserID: user.ID, Date: day, Mood: 2, BBT: &stored}
				if err := database.Create(&seeded).Error; err != nil {
					t.Fatalf("seed the stored day: %v", err)
				}
			}

			form := url.Values{"flow": {models.FlowNone}, "mood": {"4"}, "bbt": {testCase.typed}}
			request := httptest.NewRequest(http.MethodPut, "/api/v1/days/"+dayRaw, strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("Cookie", authCookie)

			response := mustAppResponse(t, app, request)
			assertStatusCode(t, response, http.StatusOK)

			entry, err := fetchLogByDateForTest(database, user.ID, day, time.UTC)
			if err != nil {
				t.Fatalf("load stored log for %s: %v", dayRaw, err)
			}
			if entry.ID == 0 {
				t.Fatalf("bbt=%q is not this write's subject, so %s must hold the saved day", testCase.typed, dayRaw)
			}
			if entry.Mood != 4 {
				t.Fatalf("bbt=%q: expected the sent mood 4 to land, got %d", testCase.typed, entry.Mood)
			}
			if !testCase.seedStored {
				if entry.BBT != nil {
					t.Fatalf("bbt=%q: a hidden field must start empty on a new day, got %.4f °C stored", testCase.typed, *entry.BBT)
				}
				return
			}
			if entry.BBT == nil || *entry.BBT != storedCelsius {
				t.Fatalf("bbt=%q: expected the stored temperature preserved as %.2f °C, got %v", testCase.typed, storedCelsius, entry.BBT)
			}
		})
	}
}

// fahrenheitBBTTrackingPreferences is the pair of preferences the temperature
// cases in this file drive the day form through: the account reads and writes
// in Fahrenheit, and either tracks BBT or hides it.
func fahrenheitBBTTrackingPreferences(trackBBT bool) map[string]any {
	return map[string]any{
		"temperature_unit": services.TemperatureUnitFahrenheit,
		"track_bbt":        trackBBT,
	}
}
