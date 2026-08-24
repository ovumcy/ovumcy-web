package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func TestParseDayPayloadFromJSON(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodPost, "/day", strings.NewReader(`{"is_period":true,"flow":"heavy","cycle_factor_keys":["travel","stress"],"symptom_ids":[1,3],"notes":"abc"}`))
	request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)

	payload := parseDayPayloadForTest(t, request)
	if !payload.IsPeriod || payload.Flow != "heavy" || len(payload.CycleFactorKeys) != 2 || len(payload.SymptomIDs) != 2 || payload.Notes != "abc" {
		t.Fatalf("unexpected payload parsed from json: %+v", payload)
	}
}

func TestParseDayPayloadFromForm(t *testing.T) {
	t.Parallel()

	form := url.Values{}
	form.Set("is_period", "on")
	form.Set("flow", " Medium ")
	form.Add("cycle_factor_keys", " travel ")
	form.Add("cycle_factor_keys", "stress")
	form.Add("symptom_ids", "2")
	form.Add("symptom_ids", "4")
	form.Set("notes", " note ")

	request := httptest.NewRequest(http.MethodPost, "/day", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	payload := parseDayPayloadForTest(t, request)
	if !payload.IsPeriod {
		t.Fatal("expected is_period=true from form")
	}
	if payload.Flow != "medium" {
		t.Fatalf("expected normalized flow=medium, got %q", payload.Flow)
	}
	if payload.Notes != "note" {
		t.Fatalf("expected trimmed notes, got %q", payload.Notes)
	}
	if len(payload.CycleFactorKeys) != 2 || payload.CycleFactorKeys[0] != " travel " || payload.CycleFactorKeys[1] != "stress" {
		t.Fatalf("unexpected cycle factor keys: %#v", payload.CycleFactorKeys)
	}
	if len(payload.SymptomIDs) != 2 || payload.SymptomIDs[0] != 2 || payload.SymptomIDs[1] != 4 {
		t.Fatalf("unexpected symptom IDs: %#v", payload.SymptomIDs)
	}
}

// TestParseDayPayloadTreatsAMalformedScalarTheSameInBothBranches is the parity
// table for the two transports of one endpoint. A malformed scalar in the JSON
// body is a bind error and answers 400; the form branch used to substitute a
// value instead — mood=abc became mood=0, which is "no mood recorded", so on an
// update of an existing day a malformed request ERASED a recorded mood and
// answered 200. That failure is indistinguishable, at every layer below, from
// the owner clearing the field, which is why it cannot be repaired anywhere but
// here.
//
// Both halves are asserted per row: the JSON refusal is the anchor, so a row
// cannot go green by both branches quietly accepting the value.
func TestParseDayPayloadTreatsAMalformedScalarTheSameInBothBranches(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		field string
		value string
	}{
		{field: "mood", value: "abc"},
		{field: "bbt", value: "abc"},
	} {
		t.Run(testCase.field, func(t *testing.T) {
			t.Parallel()

			jsonStatus := parseDayPayloadStatusForJSON(t, `{"`+testCase.field+`":"`+testCase.value+`"}`)
			if jsonStatus != http.StatusBadRequest {
				t.Fatalf("the JSON branch answered %d for %s=%q; the parity claim below needs it to refuse", jsonStatus, testCase.field, testCase.value)
			}

			formStatus := parseDayPayloadStatusForForm(t, url.Values{testCase.field: {testCase.value}})
			if formStatus != http.StatusBadRequest {
				t.Fatalf("the form branch answered %d for %s=%q while the JSON branch refuses it; a value substituted here is indistinguishable downstream from the owner clearing the field",
					formStatus, testCase.field, testCase.value)
			}
		})
	}
}

// The other direction: strictness must not swallow the two shapes a real form
// posts. An unchecked radio submits no field at all, which is the owner
// recording no mood — not a malformed request.
func TestParseDayPayloadKeepsAnAbsentOrEmptyMoodAsNoMood(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"absent", "empty"} {
		form := url.Values{"is_period": {"on"}}
		if name == "empty" {
			form.Set("mood", "")
		}

		request := httptest.NewRequest(http.MethodPost, "/day", strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		payload := parseDayPayloadForTest(t, request)
		if payload.Mood != 0 {
			t.Fatalf("expected an %s mood to parse as no mood, got %d", name, payload.Mood)
		}
	}

	form := url.Values{"mood": {"3"}}
	request := httptest.NewRequest(http.MethodPost, "/day", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if payload := parseDayPayloadForTest(t, request); payload.Mood != 3 {
		t.Fatalf("expected a well-formed mood to survive, got %d", payload.Mood)
	}
}

// TestParseDayPayloadIgnoresOutOfRangeSymptomIDs states the one field where the
// form branch deliberately stays lenient: an unparseable multi-value is
// dropped rather than refused. The parity table above therefore does not cover
// symptom_ids, and this test is what says the drop is intent rather than the
// same oversight mood carried.
func TestParseDayPayloadIgnoresOutOfRangeSymptomIDs(t *testing.T) {
	t.Parallel()

	form := url.Values{}
	form.Add("symptom_ids", "2")
	form.Add("symptom_ids", overflowUintStringForTest())
	form.Add("symptom_ids", "not-a-number")

	request := httptest.NewRequest(http.MethodPost, "/day", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	payload := parseDayPayloadForTest(t, request)
	if len(payload.SymptomIDs) != 1 || payload.SymptomIDs[0] != 2 {
		t.Fatalf("expected only in-range symptom IDs, got %#v", payload.SymptomIDs)
	}
}

func TestParseDayPayloadFromFormWithFahrenheitPreference(t *testing.T) {
	t.Parallel()

	form := url.Values{}
	form.Set("bbt", "98.60")

	request := httptest.NewRequest(http.MethodPost, "/day", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	payload := parseDayPayloadForUser(t, request, &models.User{TemperatureUnit: services.TemperatureUnitFahrenheit})
	if payload.BBT == nil || *payload.BBT != 37.00 {
		t.Fatalf("expected converted BBT 37.00, got %v", payload.BBT)
	}
}

func parseDayPayloadForTest(t *testing.T, request *http.Request) dayPayload {
	t.Helper()
	return parseDayPayloadForUser(t, request, &models.User{TemperatureUnit: services.DefaultTemperatureUnit})
}

func parseDayPayloadForUser(t *testing.T, request *http.Request, user *models.User) dayPayload {
	t.Helper()

	app := fiber.New()
	app.Post("/day", func(c fiber.Ctx) error {
		payload, err := parseDayPayload(c, user)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(payload)
	})

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	var payload dayPayload
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return payload
}

// parseDayPayloadStatusForJSON / parseDayPayloadStatusForForm run parseDayPayload
// behind the same one-route app and report only the status, so the two
// transports of one endpoint can be compared without either helper knowing
// which of them is under suspicion.
func parseDayPayloadStatusForJSON(t *testing.T, body string) int {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/day", strings.NewReader(body))
	request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	return parseDayPayloadStatus(t, request)
}

func parseDayPayloadStatusForForm(t *testing.T, form url.Values) int {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/day", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return parseDayPayloadStatus(t, request)
}

func parseDayPayloadStatus(t *testing.T, request *http.Request) int {
	t.Helper()

	user := &models.User{TemperatureUnit: services.DefaultTemperatureUnit}
	app := fiber.New()
	app.Post("/day", func(c fiber.Ctx) error {
		if _, err := parseDayPayload(c, user); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(fiber.Map{"ok": true})
	})

	return mustAppResponse(t, app, request).StatusCode
}

func overflowUintStringForTest() string {
	if strconv.IntSize == 32 {
		return "4294967296"
	}
	return "18446744073709551616"
}
