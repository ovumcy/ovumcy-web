package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestResetPasswordInvalidTokenJSONResponseDoesNotExposeSecrets(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	const plaintextToken = "plain-reset-token"
	const plaintextPassword = "EvenStronger2"

	resetCookieValue := mustSealResetCookieValueForTest(t, []byte("test-secret-key"), plaintextToken, false)
	form := url.Values{
		"password":         {plaintextPassword},
		"confirm_password": {plaintextPassword},
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/password-resets/redeem", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", resetPasswordCookieName+"="+resetCookieValue)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("reset-password json request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read reset-password json body: %v", err)
	}
	assertSecretNotExposedInResponse(t, string(body), response, plaintextToken, plaintextPassword)
}

func TestResetPasswordInvalidTokenHTMLResponseDoesNotExposeSecrets(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	const plaintextToken = "plain-reset-token"
	const plaintextPassword = "EvenStronger2"

	resetCookieValue := mustSealResetCookieValueForTest(t, []byte("test-secret-key"), plaintextToken, false)
	form := url.Values{
		"password":         {plaintextPassword},
		"confirm_password": {plaintextPassword},
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/password-resets/redeem", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", resetPasswordCookieName+"="+resetCookieValue)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("reset-password html request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/reset-password" {
		t.Fatalf("expected redirect /reset-password, got %q", location)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read reset-password html body: %v", err)
	}
	assertSecretNotExposedInResponse(t, string(body), response, plaintextToken, plaintextPassword)
}

func assertSecretNotExposedInResponse(t *testing.T, body string, response *http.Response, secrets ...string) {
	t.Helper()

	headerValues := strings.Join(response.Header.Values("Set-Cookie"), "\n")
	location := response.Header.Get("Location")

	for _, secret := range secrets {
		if strings.TrimSpace(secret) == "" {
			continue
		}
		if strings.Contains(body, secret) {
			t.Fatalf("did not expect secret in response body: %q", secret)
		}
		if strings.Contains(location, secret) {
			t.Fatalf("did not expect secret in redirect location: %q", secret)
		}
		if strings.Contains(headerValues, secret) {
			t.Fatalf("did not expect secret in response headers: %q", secret)
		}
	}
}
