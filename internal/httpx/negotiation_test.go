package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

type negotiationSnapshot struct {
	HTMX               bool   `json:"htmx"`
	AcceptsJSON        bool   `json:"accepts_json"`
	HasJSONContentType bool   `json:"has_json_content_type"`
	ResponseFormat     string `json:"response_format"`
}

func readNegotiationSnapshot(t *testing.T, headers map[string]string) negotiationSnapshot {
	t.Helper()

	app := fiber.New()
	app.Post("/", func(c fiber.Ctx) error {
		format := "html"
		switch NegotiateResponseFormat(c) {
		case ResponseFormatHTMX:
			format = "htmx"
		case ResponseFormatJSON:
			format = "json"
		}

		return c.JSON(negotiationSnapshot{
			HTMX:               IsHTMX(c),
			AcceptsJSON:        AcceptsJSON(c),
			HasJSONContentType: HasJSONContentType(c),
			ResponseFormat:     format,
		})
	})

	request := httptest.NewRequest(http.MethodPost, "/", nil)
	for key, value := range headers {
		request.Header.Set(key, value)
	}

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("app test request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	var payload negotiationSnapshot
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode response payload: %v", err)
	}
	return payload
}

func TestIsHTMXAndAcceptsJSONViaAcceptHeader(t *testing.T) {
	snapshot := readNegotiationSnapshot(t, map[string]string{
		"HX-Request": "TrUe",
		"Accept":     "text/html, application/json",
	})

	if !snapshot.HTMX {
		t.Fatal("expected HTMX=true")
	}
	if !snapshot.AcceptsJSON {
		t.Fatal("expected AcceptsJSON=true")
	}
	if snapshot.ResponseFormat != "htmx" {
		t.Fatalf("expected HTMX response format, got %q", snapshot.ResponseFormat)
	}
}

func TestAcceptsJSONViaContentTypeAlone(t *testing.T) {
	snapshot := readNegotiationSnapshot(t, map[string]string{
		"Content-Type": "application/json; charset=utf-8",
	})

	if !snapshot.AcceptsJSON {
		t.Fatal("expected AcceptsJSON=true for a JSON Content-Type without a JSON Accept header")
	}
	if !snapshot.HasJSONContentType {
		t.Fatal("expected HasJSONContentType=true for JSON Content-Type")
	}
	if snapshot.ResponseFormat != "json" {
		t.Fatalf("expected JSON response format, got %q", snapshot.ResponseFormat)
	}
}

func TestAcceptsJSONFalseWhenHeadersDoNotContainJSON(t *testing.T) {
	snapshot := readNegotiationSnapshot(t, map[string]string{
		"Accept":       "text/html",
		"Content-Type": "application/x-www-form-urlencoded",
	})

	if snapshot.HTMX {
		t.Fatal("expected HTMX=false")
	}
	if snapshot.AcceptsJSON {
		t.Fatal("expected AcceptsJSON=false")
	}
	if snapshot.HasJSONContentType {
		t.Fatal("expected HasJSONContentType=false")
	}
	if snapshot.ResponseFormat != "html" {
		t.Fatalf("expected HTML response format, got %q", snapshot.ResponseFormat)
	}
}

func TestHTMXWinsOverJSONNegotiation(t *testing.T) {
	snapshot := readNegotiationSnapshot(t, map[string]string{
		"HX-Request":   "true",
		"Accept":       "application/json",
		"Content-Type": "application/json",
	})

	if snapshot.ResponseFormat != "htmx" {
		t.Fatalf("expected HTMX response format to win, got %q", snapshot.ResponseFormat)
	}
}

// testConfigNoTimeout restores fiber v2's app.Test(req, -1) semantics.
var testConfigNoTimeout = fiber.TestConfig{Timeout: 0, FailOnTimeout: false}
