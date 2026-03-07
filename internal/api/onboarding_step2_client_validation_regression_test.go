package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOnboardingStep2IncludesClientSideCrossValidationHooks(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "onboarding-step2-client-validation@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/onboarding", nil)
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("onboarding request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read onboarding body: %v", err)
	}
	rendered := string(body)

	if !strings.Contains(rendered, `hx-post="/onboarding/step2"`) {
		t.Fatalf("expected onboarding step2 form to post to /onboarding/step2")
	}
	if !strings.Contains(rendered, `id="onboarding-step2-status"`) {
		t.Fatalf("expected onboarding step2 status target")
	}
	if !strings.Contains(rendered, `name="cycle_length"`) {
		t.Fatalf("expected onboarding cycle length input")
	}
	if !strings.Contains(rendered, `name="period_length"`) || !strings.Contains(rendered, `max="14"`) {
		t.Fatalf("expected onboarding period length input with max=14")
	}
	if !strings.Contains(rendered, `name="auto_period_fill"`) {
		t.Fatalf("expected onboarding auto period fill control")
	}
	if !strings.Contains(rendered, "Period duration is incompatible with cycle length.") {
		t.Fatalf("expected onboarding step2 to render localized incompatible-values message")
	}
}
