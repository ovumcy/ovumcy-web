package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestFlashCookieUsesSealedTransport(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "sealed-flash@example.com", "StrongPass1", true)

	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(url.Values{
		"email":    {user.Email},
		"password": {"WrongPass1"},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusSeeOther)

	flashCookie := responseCookie(response.Cookies(), flashCookieName)
	if flashCookie == nil {
		t.Fatal("expected flash cookie in login error response")
	}
	if strings.Contains(flashCookie.Value, user.Email) {
		t.Fatalf("did not expect flash cookie to expose email in plaintext: %q", flashCookie.Value)
	}

	decoded, err := base64.RawURLEncoding.DecodeString(flashCookie.Value)
	if err == nil {
		payload := FlashPayload{}
		if json.Unmarshal(decoded, &payload) == nil {
			t.Fatalf("expected flash cookie to be sealed; got plaintext payload: %#v", payload)
		}
	}
}

func TestLegacyPlainFlashCookieIsIgnored(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	legacyPayload, err := json.Marshal(FlashPayload{
		AuthError:  "invalid email or password",
		LoginEmail: "legacy-flash@example.com",
	})
	if err != nil {
		t.Fatalf("marshal legacy flash payload: %v", err)
	}
	legacyCookie := base64.RawURLEncoding.EncodeToString(legacyPayload)

	request := httptest.NewRequest(http.MethodGet, "/login", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", flashCookieName+"="+legacyCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	body := mustReadBodyString(t, response.Body)
	assertBodyNotContainsAll(t, body,
		bodyStringMatch{fragment: "legacy-flash@example.com", message: "did not expect legacy flash email to be restored"},
		bodyStringMatch{fragment: "Invalid email or password.", message: "did not expect legacy flash auth error to be rendered"},
	)
}

func TestTamperedSealedFlashCookieIsIgnoredAndCleared(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "tampered-flash@example.com", "StrongPass1", true)

	loginRequest := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(url.Values{
		"email":    {user.Email},
		"password": {"WrongPass1"},
	}.Encode()))
	loginRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	loginResponse := mustAppResponse(t, app, loginRequest)
	assertStatusCode(t, loginResponse, http.StatusSeeOther)

	flashCookie := responseCookie(loginResponse.Cookies(), flashCookieName)
	if flashCookie == nil {
		t.Fatal("expected flash cookie in login error response")
	}

	tamperedValue := flashCookie.Value
	if strings.HasSuffix(tamperedValue, "A") {
		tamperedValue = tamperedValue[:len(tamperedValue)-1] + "B"
	} else {
		tamperedValue = tamperedValue[:len(tamperedValue)-1] + "A"
	}

	pageRequest := httptest.NewRequest(http.MethodGet, "/login", nil)
	pageRequest.Header.Set("Accept-Language", "en")
	pageRequest.Header.Set("Cookie", flashCookieName+"="+tamperedValue)

	pageResponse := mustAppResponse(t, app, pageRequest)
	assertStatusCode(t, pageResponse, http.StatusOK)

	body := mustReadBodyString(t, pageResponse.Body)
	assertBodyNotContainsAll(t, body,
		bodyStringMatch{fragment: "tampered-flash@example.com", message: "did not expect tampered flash email to be restored"},
		bodyStringMatch{fragment: "Invalid email or password.", message: "did not expect tampered flash auth error to be rendered"},
	)

	clearedCookie := responseCookie(pageResponse.Cookies(), flashCookieName)
	if clearedCookie == nil {
		t.Fatal("expected tampered flash cookie to be cleared")
	}
	if clearedCookie.Value != "" {
		t.Fatalf("expected cleared flash cookie, got %#v", clearedCookie)
	}
}
