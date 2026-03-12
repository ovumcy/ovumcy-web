package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSetupStatusRouteIsNotPubliclyExposed(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	request := httptest.NewRequest(http.MethodGet, "/api/auth/setup-status", nil)
	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("setup-status request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("expected setup-status route to be absent with 404, got %d", response.StatusCode)
	}
}
