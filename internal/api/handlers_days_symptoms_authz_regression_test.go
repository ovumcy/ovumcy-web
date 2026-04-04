package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestSymptomRoutesRequireAuthJSON(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	testCases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{
			name:   "list",
			method: http.MethodGet,
			path:   "/api/symptoms",
		},
		{
			name:   "create",
			method: http.MethodPost,
			path:   "/api/symptoms",
			body:   `{"name":"Joint stiffness","icon":"J","color":"#334455"}`,
		},
		{
			name:   "archive",
			method: http.MethodPost,
			path:   "/api/symptoms/1/archive",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			request.Header.Set("Accept", "application/json")

			response, err := app.Test(request, -1)
			if err != nil {
				t.Fatalf("symptom auth-required request failed: %v", err)
			}
			defer response.Body.Close()

			if response.StatusCode != http.StatusUnauthorized {
				t.Fatalf("expected status 401, got %d", response.StatusCode)
			}
			if got := readAPIError(t, response.Body); got != "unauthorized" {
				t.Fatalf("expected unauthorized error, got %q", got)
			}
		})
	}
}

func TestSymptomRoutesRejectUnsupportedLegacyRoleJSON(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "symptom-routes-legacy@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("role", "partner").Error; err != nil {
		t.Fatalf("set unsupported legacy role: %v", err)
	}
	user.Role = "partner"
	authCookie := issueAuthCookieForUser(t, user)

	testCases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{
			name:   "list",
			method: http.MethodGet,
			path:   "/api/symptoms",
		},
		{
			name:   "create",
			method: http.MethodPost,
			path:   "/api/symptoms",
			body:   `{"name":"Joint stiffness","icon":"J","color":"#334455"}`,
		},
		{
			name:   "archive",
			method: http.MethodPost,
			path:   "/api/symptoms/1/archive",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			if tc.body != "" {
				request.Header.Set("Content-Type", "application/json")
			}
			request.Header.Set("Accept", "application/json")
			request.Header.Set("Cookie", authCookie)

			response, err := app.Test(request, -1)
			if err != nil {
				t.Fatalf("unsupported legacy role symptom request failed: %v", err)
			}
			defer response.Body.Close()

			if response.StatusCode != http.StatusForbidden {
				t.Fatalf("expected status 403, got %d", response.StatusCode)
			}
			if got := readAPIError(t, response.Body); got != "web sign-in unavailable" {
				t.Fatalf("expected unsupported-role sign-in error, got %q", got)
			}
		})
	}
}
