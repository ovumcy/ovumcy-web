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

func TestParseDayPayloadSources(t *testing.T) {
	t.Parallel()

	app := fiber.New()
	app.Post("/day", func(c *fiber.Ctx) error {
		payload, err := parseDayPayload(c)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		return c.JSON(payload)
	})

	t.Run("parses JSON payload", func(t *testing.T) {
		body := `{"is_period":true,"flow":"heavy","symptom_ids":[1,3],"notes":"abc"}`
		req := httptest.NewRequest(http.MethodPost, "/day", strings.NewReader(body))
		req.Header.Set("Content-Type", fiber.MIMEApplicationJSON)

		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.StatusCode)
		}

		var payload dayPayload
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if !payload.IsPeriod || payload.Flow != "heavy" || len(payload.SymptomIDs) != 2 || payload.Notes != "abc" {
			t.Fatalf("unexpected payload parsed from json: %+v", payload)
		}
	})

	t.Run("parses form payload and normalizes", func(t *testing.T) {
		form := url.Values{}
		form.Set("is_period", "on")
		form.Set("flow", " Medium ")
		form.Add("symptom_ids", "2")
		form.Add("symptom_ids", "4")
		form.Set("notes", " note ")

		req := httptest.NewRequest(http.MethodPost, "/day", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.StatusCode)
		}

		var payload dayPayload
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("decode response: %v", err)
		}
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
	})

	t.Run("ignores out of range symptom ids from form payload", func(t *testing.T) {
		form := url.Values{}
		form.Add("symptom_ids", "2")
		form.Add("symptom_ids", overflowUintStringForTest())
		form.Add("symptom_ids", "not-a-number")

		req := httptest.NewRequest(http.MethodPost, "/day", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := app.Test(req, -1)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200, got %d", resp.StatusCode)
		}

		var payload dayPayload
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(payload.SymptomIDs) != 1 || payload.SymptomIDs[0] != 2 {
			t.Fatalf("expected only in-range symptom IDs, got %#v", payload.SymptomIDs)
		}
	})
}

func overflowUintStringForTest() string {
	if strconv.IntSize == 32 {
		return "4294967296"
	}
	return "18446744073709551616"
}
