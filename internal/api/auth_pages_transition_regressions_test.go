package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestRecoveryCodePageRendersCopyDownloadAndContinueControls(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	form := url.Values{
		"email":            {"recovery-controls@example.com"},
		"password":         {"StrongPass1"},
		"confirm_password": {"StrongPass1"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept-Language", "en")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("register request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/recovery-code" {
		t.Fatalf("expected redirect to /recovery-code, got %q", location)
	}

	authCookie := responseCookieValue(response.Cookies(), authCookieName)
	recoveryCookie := responseCookieValue(response.Cookies(), recoveryCodeCookieName)
	if authCookie == "" || recoveryCookie == "" {
		t.Fatalf("expected auth and recovery cookies in register response")
	}

	recoveryRequest := httptest.NewRequest(http.MethodGet, "/recovery-code", nil)
	recoveryRequest.Header.Set("Accept-Language", "en")
	recoveryRequest.Header.Set("Cookie", authCookieName+"="+authCookie+"; "+recoveryCodeCookieName+"="+recoveryCookie)

	recoveryResponse, err := app.Test(recoveryRequest, -1)
	if err != nil {
		t.Fatalf("recovery page request failed: %v", err)
	}
	defer recoveryResponse.Body.Close()

	if recoveryResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected recovery status 200, got %d", recoveryResponse.StatusCode)
	}

	body, err := io.ReadAll(recoveryResponse.Body)
	if err != nil {
		t.Fatalf("read recovery page body: %v", err)
	}

	rendered := string(body)
	assertBodyContainsAll(t, rendered,
		bodyStringMatch{fragment: `id="recovery-code"`, message: "expected dedicated recovery-code field"},
		bodyStringMatch{fragment: `Copy code`, message: "expected copy control on recovery page"},
		bodyStringMatch{fragment: `Download code`, message: "expected download control on recovery page"},
		bodyStringMatch{fragment: `id="recovery-code-saved"`, message: "expected recovery confirmation checkbox"},
	)
}
