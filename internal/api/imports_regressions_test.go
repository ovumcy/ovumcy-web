package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/services"
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
	// Decode the DTO so all three result counts are pinned to the wire, not just
	// a substring of one of them.
	var result struct {
		OK       bool `json:"ok"`
		Added    int  `json:"added"`
		Skipped  int  `json:"skipped"`
		Rejected int  `json:"rejected"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode import result: %v", err)
	}
	if !result.OK || result.Added != 2 || result.Skipped != 0 || result.Rejected != 0 {
		t.Fatalf("expected {ok:true added:2 skipped:0 rejected:0}, got %+v", result)
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
	// The exact error key is pinned once in TestMapImportErrorCoversAllBranches;
	// here we only assert the request-level status, not a duplicated wire-string.
}

// TestMapImportErrorCoversAllBranches unit-pins every arm of the import error
// mapping so the 413/500 paths are guaranteed without forcing those runtime
// failures end-to-end.
func TestMapImportErrorCoversAllBranches(t *testing.T) {
	t.Parallel()

	cases := []struct {
		err    error
		status int
		key    string
	}{
		{services.ErrImportMalformed, http.StatusBadRequest, "invalid import file"},
		{services.ErrImportTooLarge, http.StatusRequestEntityTooLarge, "import file too large"},
		{services.ErrImportWriteFailed, http.StatusInternalServerError, "failed to import data"},
		{errors.New("unexpected"), http.StatusInternalServerError, "failed to import data"},
	}
	for _, tc := range cases {
		spec := mapImportError(tc.err)
		if spec.Status != tc.status || spec.Key != tc.key {
			t.Fatalf("mapImportError(%v) = {status:%d key:%q}, want {status:%d key:%q}", tc.err, spec.Status, spec.Key, tc.status, tc.key)
		}
	}
}

// TestImportJSONHTMXSuccessReturnsStatusMarkup pins the HTMX success branch:
// an HX-Request returns dismissible status-ok markup rather than JSON.
func TestImportJSONHTMXSuccessReturnsStatusMarkup(t *testing.T) {
	t.Parallel()

	ctx := newSettingsSecurityTestContext(t, "import-htmx@example.com")
	request := newImportRequest(ctx, `{"entries":[{"date":"2026-07-01","period":true,"flow":"medium","cycle_factors":[]}]}`, true)
	request.Header.Set("HX-Request", "true")

	response, err := ctx.app.Test(request, -1)
	if err != nil {
		t.Fatalf("htmx import failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.StatusCode)
	}
	payload, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(payload), "status-ok") {
		t.Fatalf("expected dismissible status-ok markup for htmx success, got %s", string(payload))
	}
}

// TestImportJSONFormFallbackRedirects pins the non-JSON, non-HTMX branch: a
// plain form-style client is redirected back to settings.
func TestImportJSONFormFallbackRedirects(t *testing.T) {
	t.Parallel()

	ctx := newSettingsSecurityTestContext(t, "import-form@example.com")
	request := httptest.NewRequest(http.MethodPost, "/api/v1/imports/json", strings.NewReader(`{"entries":[]}`))
	request.Header.Set("Content-Type", "text/plain")
	request.Header.Set("Cookie", settingsCookieHeader(ctx.authCookie, ctx.csrfCookie))
	request.Header.Set("X-CSRF-Token", ctx.csrfToken)

	response, err := ctx.app.Test(request, -1)
	if err != nil {
		t.Fatalf("form import failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect for non-JSON client, got %d", response.StatusCode)
	}
	if loc := response.Header.Get("Location"); loc != "/settings" {
		t.Fatalf("expected redirect to /settings, got %q", loc)
	}
}
