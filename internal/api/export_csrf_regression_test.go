package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestExportCSVDoesNotRequireCSRFForGET documents that the v1 export
// surface is GET-only; CSRF middleware does not gate GET requests, so an
// authenticated client can pull the CSV with the auth cookie alone. This
// replaces the previous regression that asserted POST-without-CSRF was
// rejected with 403: that contract collapses with the POST -> GET flip.
func TestExportCSVDoesNotRequireCSRFForGET(t *testing.T) {
	t.Parallel()

	ctx := newSettingsSecurityTestContext(t, "export-csrf-missing@example.com")

	request := httptest.NewRequest(http.MethodGet, "/api/v1/exports/csv?from=2026-02-01&to=2026-02-28", nil)
	request.Header.Set("Cookie", ctx.authCookie)

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("export GET without csrf failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}
	if got := response.Header.Get("Content-Type"); !strings.Contains(got, "text/csv") {
		t.Fatalf("expected text/csv content type, got %q", got)
	}
}

func TestExportCSVSucceedsForAuthenticatedGET(t *testing.T) {
	t.Parallel()

	ctx := newSettingsSecurityTestContext(t, "export-csrf-valid@example.com")

	request := httptest.NewRequest(http.MethodGet, "/api/v1/exports/csv?from=2026-02-01&to=2026-02-28", nil)
	request.Header.Set("Cookie", ctx.authCookie)

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("export GET failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}
	if got := response.Header.Get("Content-Type"); !strings.Contains(got, "text/csv") {
		t.Fatalf("expected text/csv content type, got %q", got)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read export body: %v", err)
	}
	if !strings.Contains(string(body), "Date,Period") {
		t.Fatalf("expected csv header in export response")
	}
}
