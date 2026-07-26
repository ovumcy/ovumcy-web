package api

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
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

	response, err := ctx.app.Test(newImportRequest(ctx, body, false), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("import without csrf failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

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

	response, err := ctx.app.Test(newImportRequest(ctx, body, true), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("import with csrf failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

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

	response, err := ctx.app.Test(newImportRequest(ctx, "{not valid json", true), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("import malformed failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

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

// importBodyCapTestLimit is the BodyLimit the compressed-restore regressions
// run their app with. Small enough that a payload crossing it stays a few
// hundred bytes on the wire once gzipped, so the transport cap under test is
// the DECODED size and never the wire size.
const importBodyCapTestLimit = 4096

// gzipImportPayload builds a restore payload of exactly decodedSize bytes and
// returns it gzipped. The payload is one valid day entry padded with trailing
// spaces, which encoding/json accepts after a top-level value — so the only
// thing that varies between the over-cap and at-cap cases is the decoded length.
func gzipImportPayload(t *testing.T, decodedSize int) []byte {
	t.Helper()

	entry := `{"entries":[{"date":"2026-07-01","period":true,"flow":"medium","cycle_factors":[]}]}`
	if decodedSize < len(entry) {
		t.Fatalf("decodedSize %d is below the minimum valid payload of %d bytes", decodedSize, len(entry))
	}
	payload := entry + strings.Repeat(" ", decodedSize-len(entry))

	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write([]byte(payload)); err != nil {
		t.Fatalf("gzip payload: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	if compressed.Len() >= importBodyCapTestLimit {
		t.Fatalf("gzipped payload is %d bytes, at or above the %d wire cap; the test would exercise the wire limit instead of the decoded one", compressed.Len(), importBodyCapTestLimit)
	}
	return compressed.Bytes()
}

func newGzipImportRequest(ctx settingsSecurityTestContext, compressed []byte) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/imports/json", bytes.NewReader(compressed))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Content-Encoding", "gzip")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", settingsCookieHeader(ctx.authCookie, ctx.csrfCookie))
	request.Header.Set("X-CSRF-Token", ctx.csrfToken)
	return request
}

func countImportedDays(t *testing.T, ctx settingsSecurityTestContext) int64 {
	t.Helper()

	var rows int64
	if err := ctx.database.Model(&models.DailyLog{}).Where("user_id = ?", ctx.user.ID).Count(&rows).Error; err != nil {
		t.Fatalf("count imported days: %v", err)
	}
	return rows
}

// TestImportJSONRejectsGzipBodyExceedingDecompressedCap pins the compressed
// half of the transport body cap. A gzipped body is small on the wire, so the
// pre-routing rejection never fires; the cap is applied to the DECODED stream
// inside fiber's body accessor, while the request is already routed. Left
// unguarded, the accessor substitutes an internal error string for the payload,
// which the import service reports as a malformed file — so an oversized upload
// answered "400 invalid import file", blaming the owner's export for a size
// rejection, and the import pipeline ran on bytes that were never the payload.
//
// The second half of the test is the positive anchor for the row assertion: the
// same entry, gzipped under the cap, must still restore normally — proving the
// day-row observable is live and that the guard does not reject valid
// compressed restores.
func TestImportJSONRejectsGzipBodyExceedingDecompressedCap(t *testing.T) {
	t.Parallel()

	ctx := newSettingsSecurityTestContextWithOptions(t, "import-gzip-overflow@example.com", onboardingTestAppOptions{
		enableCSRF: true,
		bodyLimit:  importBodyCapTestLimit,
	})

	oversized := gzipImportPayload(t, importBodyCapTestLimit+1)
	response, err := ctx.app.Test(newGzipImportRequest(ctx, oversized), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("oversized gzip import failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for a body whose decompressed size passes the cap, got %d", response.StatusCode)
	}
	var payload struct {
		Error       string `json:"error"`
		ErrorDetail struct {
			Key      string `json:"key"`
			Category string `json:"category"`
			Target   string `json:"target"`
		} `json:"error_detail"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode 413 envelope: %v", err)
	}
	if payload.Error != "request_too_large" {
		t.Fatalf("expected the stable transport key request_too_large, got %q", payload.Error)
	}
	if payload.ErrorDetail.Key != "request_too_large" || payload.ErrorDetail.Category != "too_large" || payload.ErrorDetail.Target != "global" {
		t.Fatalf("expected the shared global too_large error_detail, got %+v", payload.ErrorDetail)
	}
	if rows := countImportedDays(t, ctx); rows != 0 {
		t.Fatalf("expected the rejected upload to reach no import pipeline, got %d persisted days", rows)
	}

	accepted := gzipImportPayload(t, importBodyCapTestLimit)
	anchor, err := ctx.app.Test(newGzipImportRequest(ctx, accepted), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("under-cap gzip import failed: %v", err)
	}
	defer func() { _ = anchor.Body.Close() }()

	if anchor.StatusCode != http.StatusOK {
		t.Fatalf("expected a gzipped body at the cap to restore normally, got %d", anchor.StatusCode)
	}
	if rows := countImportedDays(t, ctx); rows != 1 {
		t.Fatalf("expected the accepted upload to persist 1 day, got %d", rows)
	}
}

// TestImportJSONAcceptsGzipBodyAtTheDecompressedCap pins the other side of the
// boundary: a decoded body of exactly BodyLimit bytes is domain input, not a
// transport rejection, and flows through the normal restore result. Together
// with the overflow case above this fixes the cap at "limit passes, limit+1
// rejects" rather than somewhere nearby.
func TestImportJSONAcceptsGzipBodyAtTheDecompressedCap(t *testing.T) {
	t.Parallel()

	ctx := newSettingsSecurityTestContextWithOptions(t, "import-gzip-boundary@example.com", onboardingTestAppOptions{
		enableCSRF: true,
		bodyLimit:  importBodyCapTestLimit,
	})

	response, err := ctx.app.Test(newGzipImportRequest(ctx, gzipImportPayload(t, importBodyCapTestLimit)), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("at-cap gzip import failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 at the cap, got %d", response.StatusCode)
	}
	var result struct {
		OK       bool `json:"ok"`
		Added    int  `json:"added"`
		Skipped  int  `json:"skipped"`
		Rejected int  `json:"rejected"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		t.Fatalf("decode import result: %v", err)
	}
	if !result.OK || result.Added != 1 || result.Skipped != 0 || result.Rejected != 0 {
		t.Fatalf("expected {ok:true added:1 skipped:0 rejected:0}, got %+v", result)
	}
}

// TestImportJSONHTMXSuccessReturnsStatusMarkup pins the HTMX success branch:
// an HX-Request returns dismissible status-ok markup rather than JSON.
func TestImportJSONHTMXSuccessReturnsStatusMarkup(t *testing.T) {
	t.Parallel()

	ctx := newSettingsSecurityTestContext(t, "import-htmx@example.com")
	request := newImportRequest(ctx, `{"entries":[{"date":"2026-07-01","period":true,"flow":"medium","cycle_factors":[]}]}`, true)
	request.Header.Set("HX-Request", "true")

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("htmx import failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

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

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("form import failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 redirect for non-JSON client, got %d", response.StatusCode)
	}
	if loc := response.Header.Get("Location"); loc != "/settings" {
		t.Fatalf("expected redirect to /settings, got %q", loc)
	}
}
