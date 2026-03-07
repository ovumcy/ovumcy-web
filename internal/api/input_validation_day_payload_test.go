package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestParseDayPayloadFromJSON(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodPost, "/day", strings.NewReader(`{"is_period":true,"flow":"heavy","symptom_ids":[1,3],"notes":"abc"}`))
	request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)

	payload := parseDayPayloadForTest(t, request)
	if !payload.IsPeriod || payload.Flow != "heavy" || len(payload.SymptomIDs) != 2 || payload.Notes != "abc" {
		t.Fatalf("unexpected payload parsed from json: %+v", payload)
	}
}

func TestParseDayPayloadFromForm(t *testing.T) {
	t.Parallel()

	form := url.Values{}
	form.Set("is_period", "on")
	form.Set("flow", " Medium ")
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
	if len(payload.SymptomIDs) != 2 || payload.SymptomIDs[0] != 2 || payload.SymptomIDs[1] != 4 {
		t.Fatalf("unexpected symptom IDs: %#v", payload.SymptomIDs)
	}
}

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

func parseDayPayloadForTest(t *testing.T, request *http.Request) dayPayload {
	t.Helper()

	app := fiber.New()
	app.Post("/day", func(c *fiber.Ctx) error {
		payload, err := parseDayPayload(c)
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

func overflowUintStringForTest() string {
	if strconv.IntSize == 32 {
		return "4294967296"
	}
	return "18446744073709551616"
}
