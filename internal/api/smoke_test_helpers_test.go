package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func smokeGET(t *testing.T, app *fiber.App, authCookie string, path string, expectedStatus int) string {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("GET %s failed: %v", path, err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("GET %s read body failed: %v", path, err)
	}
	if response.StatusCode != expectedStatus {
		t.Fatalf("GET %s expected status %d, got %d body=%s", path, expectedStatus, response.StatusCode, string(body))
	}
	return string(body)
}
