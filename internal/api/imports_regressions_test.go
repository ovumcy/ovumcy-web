package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newImportRequest(ctx settingsSecurityTestContext, body string, withCSRF bool) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/imports/json", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if withCSRF {
		request.Header.Set("Cookie", settingsCookieHeader(ctx.authCookie, ctx.csrfCookie))
		request.Header.Set("X-CSRF-Token", ctx.csrfToken)
		return request
	}
	request.Header.Set("Cookie", ctx.authCookie)
	return request
}

// TestImportJSONRejectsMissingCSRF pins the endpoint-defense-in-depth invariant:
// the restore is state-mutating, so the global CSRF middleware must reject a
// request that omits the token, before any data is written.
func TestImportJSONRejectsMissingCSRF(t *testing.T) {
	t.Parallel()

	ctx := newSettingsSecurityTestContext(t, "import-csrf-missing@example.com")
	body := `{"entries":[{"date":"2026-07-01","period":true,"flow":"medium","cycle_factors":[]}]}`

	response, err := ctx.app.Test(newImportRequest(ctx, body, false), -1)
	if err != nil {
		t.Fatalf("import without csrf failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403 without csrf, got %d", response.StatusCode)
	}
}

// TestImportJSONSucceedsWithCSRF is the valid-token happy path: an owner with a
// CSRF token restores two days and receives the additive result counts.
func TestImportJSONSucceedsWithCSRF(t *testing.T) {
	t.Parallel()

	ctx := newSettingsSecurityTestContext(t, "import-csrf-valid@example.com")
	body := `{"entries":[` +
		`{"date":"2026-07-01","period":true,"flow":"medium","cycle_factors":[]},` +
		`{"date":"2026-07-02","period":false,"cycle_factors":[]}` +
		`]}`

	response, err := ctx.app.Test(newImportRequest(ctx, body, true), -1)
	if err != nil {
		t.Fatalf("import with csrf failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.StatusCode)
	}
	payload, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(payload), `"added":2`) {
		t.Fatalf("expected added:2 in response, got %s", string(payload))
	}
}

// TestImportJSONRejectsMalformedFile maps a non-JSON upload to a 400 with the
// stable, PII-free error key the settings JS keys its message off.
func TestImportJSONRejectsMalformedFile(t *testing.T) {
	t.Parallel()

	ctx := newSettingsSecurityTestContext(t, "import-malformed@example.com")

	response, err := ctx.app.Test(newImportRequest(ctx, "{not valid json", true), -1)
	if err != nil {
		t.Fatalf("import malformed failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", response.StatusCode)
	}
	payload, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(payload), "invalid import file") {
		t.Fatalf("expected error key 'invalid import file', got %s", string(payload))
	}
}
