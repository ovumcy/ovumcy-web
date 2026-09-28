package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// These tests pin the server behaviour that docs/openapi.yaml states for GET
// and HEAD /api/v1/days/{date}: GET answers 200 with an id-0 placeholder for a
// day without a stored record, and HEAD reports whether the day holds data,
// which is not the same question as whether a record is stored. HEAD's "no
// body" is left to the HTTP stack: a client never sees a HEAD body.

func sendDayRequest(t *testing.T, app *fiber.App, method, path, authCookie, timezone, body string) (int, []byte) {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Cookie", authCookie)
	if timezone != "" {
		request.Header.Set(timezoneHeaderName, timezone)
	}
	if body != "" {
		request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	}
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("%s %s: read body: %v", method, path, err)
	}
	return response.StatusCode, raw
}

func getDayForTest(t *testing.T, app *fiber.App, path, authCookie, timezone string) dayResponse {
	t.Helper()

	status, raw := sendDayRequest(t, app, http.MethodGet, path, authCookie, timezone, "")
	if status != http.StatusOK {
		t.Fatalf("GET %s (tz %q): status %d, want 200", path, timezone, status)
	}
	var day dayResponse
	if err := json.Unmarshal(raw, &day); err != nil {
		t.Fatalf("GET %s: decode: %v (%s)", path, err, raw)
	}
	return day
}

func TestGetDayWithoutRecordAnswersTheDocumentedPlaceholder(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	// The other owner is created first so the caller's id is not 1.
	other := createOnboardingTestUser(t, database, "day-placeholder-get-other@example.com", "StrongPass1", true)
	user := createOnboardingTestUser(t, database, "day-placeholder-get@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	// Another owner's data on the same date must not reach this owner's answer.
	otherDay := models.DailyLog{
		UserID: other.ID, Date: time.Date(2026, time.March, 4, 0, 0, 0, 0, time.UTC),
		IsPeriod: true, Flow: models.FlowHeavy, Mood: 3, Notes: "other owner",
		SexActivity: models.SexActivityNone, CervicalMucus: models.CervicalMucusNone,
		PregnancyTest: models.PregnancyTestNone, CycleFactorKeys: []string{}, SymptomIDs: []uint{},
	}
	if err := database.Create(&otherDay).Error; err != nil {
		t.Fatalf("seed other owner's day: %v", err)
	}

	for _, timezone := range []string{"", "Pacific/Kiritimati", "Pacific/Pago_Pago"} {
		status, raw := sendDayRequest(t, app, http.MethodGet, "/api/v1/days/2026-03-04", authCookie, timezone, "")
		if status != http.StatusOK {
			t.Fatalf("GET day without a record (tz %q): status %d, want 200", timezone, status)
		}

		var got map[string]json.RawMessage
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("decode placeholder (tz %q): %v (%s)", timezone, err, raw)
		}
		want := map[string]string{
			"id":                `0`,
			"user_id":           strconv.FormatUint(uint64(user.ID), 10),
			"date":              `"2026-03-04"`,
			"is_period":         `false`,
			"cycle_start":       `false`,
			"is_uncertain":      `false`,
			"flow":              `"none"`,
			"sex_activity":      `"none"`,
			"cervical_mucus":    `"none"`,
			"pregnancy_test":    `"none"`,
			"mood":              `0`,
			"notes":             `""`,
			"cycle_factor_keys": `[]`,
			"symptom_ids":       `[]`,
			"created_at":        `"0001-01-01T00:00:00Z"`,
			"updated_at":        `"0001-01-01T00:00:00Z"`,
		}
		for key, value := range want {
			if string(got[key]) != value {
				t.Errorf("placeholder (tz %q) %s = %s, want %s", timezone, key, got[key], value)
			}
		}
		if _, present := got["bbt"]; present {
			t.Errorf("placeholder (tz %q) carries bbt = %s, want the key absent", timezone, got["bbt"])
		}
		if len(got) != len(want) {
			t.Errorf("placeholder (tz %q) has %d keys, want %d: %s", timezone, len(got), len(want), raw)
		}
	}

	var stored int64
	if err := database.Model(&models.DailyLog{}).Where("user_id = ?", user.ID).Count(&stored).Error; err != nil {
		t.Fatalf("count day rows: %v", err)
	}
	if stored != 0 {
		t.Fatalf("GET stored %d day rows, want the placeholder never stored", stored)
	}
}

func TestPutWithNoValuesStoresARecordThatHeadReportsAsHoldingNoData(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "day-placeholder-empty-put@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	const path = "/api/v1/days/2026-03-05"

	status, raw := sendDayRequest(t, app, http.MethodPut, path, authCookie, "", `{}`)
	if status != http.StatusOK {
		t.Fatalf("PUT with no values: status %d, want 200 (%s)", status, raw)
	}

	if day := getDayForTest(t, app, path, authCookie, ""); day.ID == 0 {
		t.Fatalf("stored record answered with id 0, want a non-zero id")
	}

	if status, _ := sendDayRequest(t, app, http.MethodHead, path, authCookie, "", ""); status != http.StatusNotFound {
		t.Fatalf("HEAD on a stored record without data: status %d, want 404", status)
	}
}

func TestHeadDayReportsWhetherTheDayHoldsData(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	other := createOnboardingTestUser(t, database, "day-placeholder-head-other@example.com", "StrongPass1", true)
	user := createOnboardingTestUser(t, database, "day-placeholder-head@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	symptom := models.SymptomType{UserID: user.ID, Name: "Contract symptom", Icon: "*", Color: "#AABBCC"}
	if err := database.Create(&symptom).Error; err != nil {
		t.Fatalf("seed symptom: %v", err)
	}

	// Each seeded row carries exactly one value; the no-record day sits between
	// two days with data and shares its date with another owner's data.
	cases := []struct {
		name string
		seed func(*models.DailyLog)
		want int
	}{
		{name: "period", seed: func(e *models.DailyLog) { e.IsPeriod = true }, want: http.StatusOK},
		{name: "no record", want: http.StatusNotFound},
		{name: "flow spotting", seed: func(e *models.DailyLog) { e.Flow = models.FlowSpotting }, want: http.StatusOK},
		{name: "flow light", seed: func(e *models.DailyLog) { e.Flow = models.FlowLight }, want: http.StatusOK},
		{name: "flow medium", seed: func(e *models.DailyLog) { e.Flow = models.FlowMedium }, want: http.StatusOK},
		{name: "flow heavy", seed: func(e *models.DailyLog) { e.Flow = models.FlowHeavy }, want: http.StatusOK},
		{name: "mood 0", seed: func(e *models.DailyLog) { e.Mood = 0 }, want: http.StatusNotFound},
		{name: "mood 1", seed: func(e *models.DailyLog) { e.Mood = 1 }, want: http.StatusOK},
		{name: "mood 5", seed: func(e *models.DailyLog) { e.Mood = 5 }, want: http.StatusOK},
		{name: "mood 6", seed: func(e *models.DailyLog) { e.Mood = 6 }, want: http.StatusNotFound},
		{name: "intimacy protected", seed: func(e *models.DailyLog) { e.SexActivity = models.SexActivityProtected }, want: http.StatusOK},
		{name: "intimacy unprotected", seed: func(e *models.DailyLog) { e.SexActivity = models.SexActivityUnprotected }, want: http.StatusOK},
		{name: "bbt", seed: func(e *models.DailyLog) { e.BBT = new(36.6) }, want: http.StatusOK},
		{name: "mucus dry", seed: func(e *models.DailyLog) { e.CervicalMucus = models.CervicalMucusDry }, want: http.StatusOK},
		{name: "mucus moist", seed: func(e *models.DailyLog) { e.CervicalMucus = models.CervicalMucusMoist }, want: http.StatusOK},
		{name: "mucus creamy", seed: func(e *models.DailyLog) { e.CervicalMucus = models.CervicalMucusCreamy }, want: http.StatusOK},
		{name: "mucus eggwhite", seed: func(e *models.DailyLog) { e.CervicalMucus = models.CervicalMucusEggWhite }, want: http.StatusOK},
		{name: "pregnancy test negative", seed: func(e *models.DailyLog) { e.PregnancyTest = models.PregnancyTestNegative }, want: http.StatusOK},
		{name: "pregnancy test positive", seed: func(e *models.DailyLog) { e.PregnancyTest = models.PregnancyTestPositive }, want: http.StatusOK},
		{name: "cycle factor", seed: func(e *models.DailyLog) { e.CycleFactorKeys = []string{"stress"} }, want: http.StatusOK},
		{name: "symptom", seed: func(e *models.DailyLog) { e.SymptomIDs = []uint{symptom.ID} }, want: http.StatusOK},
		{name: "notes", seed: func(e *models.DailyLog) { e.Notes = "note" }, want: http.StatusOK},
		{name: "blank notes", seed: func(e *models.DailyLog) { e.Notes = "   " }, want: http.StatusNotFound},
		{name: "whitespace notes", seed: func(e *models.DailyLog) { e.Notes = "\n\t" }, want: http.StatusNotFound},
		{name: "cycle start only", seed: func(e *models.DailyLog) { e.CycleStart = true }, want: http.StatusNotFound},
		{name: "uncertain only", seed: func(e *models.DailyLog) { e.IsUncertain = true }, want: http.StatusNotFound},
		{name: "bbt out of range", seed: func(e *models.DailyLog) { e.BBT = new(20.0) }, want: http.StatusNotFound},
		{name: "unknown cycle factor", seed: func(e *models.DailyLog) { e.CycleFactorKeys = []string{"not-a-factor"} }, want: http.StatusNotFound},
	}

	first := time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
	dateOf := func(index int) time.Time { return first.AddDate(0, 0, index) }
	emptyDay := func(userID uint, date time.Time) models.DailyLog {
		return models.DailyLog{
			UserID:          userID,
			Date:            date,
			Flow:            models.FlowNone,
			SexActivity:     models.SexActivityNone,
			CervicalMucus:   models.CervicalMucusNone,
			PregnancyTest:   models.PregnancyTestNone,
			CycleFactorKeys: []string{},
			SymptomIDs:      []uint{},
		}
	}

	for index, tc := range cases {
		if tc.seed == nil {
			foreign := emptyDay(other.ID, dateOf(index))
			foreign.IsPeriod, foreign.Notes = true, "other owner"
			if err := database.Create(&foreign).Error; err != nil {
				t.Fatalf("%s: seed other owner's day: %v", tc.name, err)
			}
			continue
		}
		entry := emptyDay(user.ID, dateOf(index))
		tc.seed(&entry)
		if err := database.Create(&entry).Error; err != nil {
			t.Fatalf("%s: seed day: %v", tc.name, err)
		}
	}

	for index, tc := range cases {
		date := dateOf(index).Format(time.DateOnly)
		path := "/api/v1/days/" + date
		if status, _ := sendDayRequest(t, app, http.MethodHead, path, authCookie, "", ""); status != tc.want {
			t.Errorf("HEAD %s (%s): status %d, want %d", path, tc.name, status, tc.want)
		}

		day := getDayForTest(t, app, path, authCookie, "")
		if day.Date != date {
			t.Errorf("GET %s (%s): date %q, want %q", path, tc.name, day.Date, date)
		}
		if stored := tc.seed != nil; stored != (day.ID != 0) {
			t.Errorf("GET %s (%s): id %d, want a non-zero id exactly when a record is stored", path, tc.name, day.ID)
		}
		if day.UserID != user.ID {
			t.Errorf("GET %s (%s): user_id %d, want the caller %d", path, tc.name, day.UserID, user.ID)
		}
	}
}
