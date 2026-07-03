package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func evaluateAuthPageBuilder(t *testing.T, query url.Values, handler fiber.Handler) map[string]any {
	t.Helper()
	return evaluateAuthPageBuilderWithCookie(t, query, "", handler)
}

func evaluateAuthPageBuilderWithCookie(t *testing.T, query url.Values, cookieHeader string, handler fiber.Handler) map[string]any {
	t.Helper()

	app := fiber.New()
	app.Get("/", handler)

	requestPath := "/"
	if encoded := query.Encode(); encoded != "" {
		requestPath += "?" + encoded
	}
	request := httptest.NewRequest(http.MethodGet, requestPath, nil)
	if cookieHeader != "" {
		request.Header.Set("Cookie", cookieHeader)
	}
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("app test failed: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code: got %d", response.StatusCode)
	}

	payload := map[string]any{}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode payload failed: %v", err)
	}
	return payload
}
