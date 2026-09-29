package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestEveryExportRouteRefusesABoundOutsideTheAcceptedDayRange enumerates the
// registered export routes rather than listing them, so an export added later
// is held to the same bound: a well-formed from/to outside 1900-01-01..9999-12-30
// answers the key of the field it names, exactly like a malformed one.
func TestEveryExportRouteRefusesABoundOutsideTheAcceptedDayRange(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "export-out-of-range@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	var paths []string
	for _, route := range app.GetRoutes() {
		if route.Method == http.MethodGet && strings.HasPrefix(route.Path, "/api/v1/exports/") {
			paths = append(paths, route.Path)
		}
	}
	if len(paths) < 3 {
		t.Fatalf("found export routes %v, want at least the csv, json and summary routes", paths)
	}

	cases := []struct {
		query string
		key   string
	}{
		{query: "from=0001-01-01&to=2026-02-10", key: "invalid from date"},
		{query: "from=1899-12-31&to=2026-02-10", key: "invalid from date"},
		{query: "from=2026-02-10&to=9999-12-31", key: "invalid to date"},
	}
	for _, path := range paths {
		for _, tc := range cases {
			target := path + "?" + tc.query
			response, err := app.Test(newExportRequestForTest(t, target, authCookie), testConfigNoTimeout)
			if err != nil {
				t.Fatalf("GET %s: %v", target, err)
			}
			body, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if readErr != nil {
				t.Fatalf("GET %s: read body: %v", target, readErr)
			}
			if response.StatusCode != http.StatusBadRequest {
				t.Errorf("GET %s: status %d, want 400 (%s)", target, response.StatusCode, body)
				continue
			}
			var payload struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(body, &payload); err != nil || payload.Error != tc.key {
				t.Errorf("GET %s: body %s, want the %q error", target, body, tc.key)
			}
		}
	}
}

func TestExportJSONRejectsInvalidDateRange(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "export-invalid-range@example.com", "StrongPass1", true)

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	request := newExportRequestForTest(t, "/api/v1/exports/json?from=2026-02-20&to=2026-02-10", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("export json request with invalid range failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	payload := struct {
		Error string `json:"error"`
	}{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload.Error != "invalid range" {
		t.Fatalf("expected invalid range error, got %q", payload.Error)
	}
}

func TestExportJSONRejectsInvalidFromDate(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "export-invalid-from@example.com", "StrongPass1", true)

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	request := newExportRequestForTest(t, "/api/v1/exports/json?from=not-a-date&to=2026-02-10", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("export json request with invalid from failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	payload := struct {
		Error string `json:"error"`
	}{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload.Error != "invalid from date" {
		t.Fatalf("expected invalid from date error, got %q", payload.Error)
	}
}

func TestExportJSONRejectsInvalidToDate(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "export-invalid-to@example.com", "StrongPass1", true)

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	request := newExportRequestForTest(t, "/api/v1/exports/json?from=2026-02-10&to=not-a-date", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("export json request with invalid to failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	payload := struct {
		Error string `json:"error"`
	}{}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload.Error != "invalid to date" {
		t.Fatalf("expected invalid to date error, got %q", payload.Error)
	}
}
