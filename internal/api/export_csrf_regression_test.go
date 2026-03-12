package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestExportCSVRejectsMissingCSRFFromAuthenticatedPost(t *testing.T) {
	t.Parallel()

	ctx := newSettingsSecurityTestContext(t, "export-csrf-missing@example.com")

	request := httptest.NewRequest(http.MethodPost, "/api/export/csv", strings.NewReader(url.Values{
		"from": {"2026-02-01"},
		"to":   {"2026-02-28"},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", ctx.authCookie)

	response, err := ctx.app.Test(request, -1)
	if err != nil {
		t.Fatalf("export request without csrf failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", response.StatusCode)
	}
}

func TestExportCSVAcceptsValidCSRFPost(t *testing.T) {
	t.Parallel()

	ctx := newSettingsSecurityTestContext(t, "export-csrf-valid@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/export/csv", url.Values{
		"from": {"2026-02-01"},
		"to":   {"2026-02-28"},
	}, nil)
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
