package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestFlashCookieUsesSealedTransport(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "sealed-flash@example.com", "StrongPass1", true)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(url.Values{
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

	assertSealedCookieEnvelope(t, flashCookie.Value, &FlashPayload{})
}

// TestSealedEnvelopeAroundPlaintextFlashPayloadIsRefused pins the half of the
// "sealed cookies" invariant that a shape check on the response cannot reach: a
// value wearing the v2 envelope over base64url(plaintext JSON) is not a sealed
// cookie, and the login page must refuse it. The same payload bytes are
// presented twice — once sealed under the app's own key, once merely encoded —
// so the seal is the only difference between the two requests, and the sealed
// one is the positive anchor proving the page has a rendering path to lose.
func TestSealedEnvelopeAroundPlaintextFlashPayloadIsRefused(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	serialized, err := json.Marshal(FlashPayload{
		AuthError:   "invalid credentials",
		ForgotEmail: "forged-flash@example.com",
	})
	if err != nil {
		t.Fatalf("marshal flash payload: %v", err)
	}

	// Positive anchor: sealed, the very same payload renders its error.
	sealedResponse := loginPageWithFlashCookie(t, app, sealCookieForTestApp(t, flashCookieName, serialized))
	sealedBody := mustReadBodyString(t, sealedResponse.Body)
	if htmlAuthErrorByKey(mustParseHTMLDocument(t, sealedBody), "auth.error.invalid_credentials") == nil {
		t.Fatal("expected a sealed flash payload to surface its auth error via data-error-key")
	}

	// The forgery: same bytes, same envelope, no seal.
	forged := secureCookieVersion + "." + base64.RawURLEncoding.EncodeToString(serialized)
	forgedResponse := loginPageWithFlashCookie(t, app, forged)
	forgedBody := mustReadBodyString(t, forgedResponse.Body)
	if htmlAuthErrorByKey(mustParseHTMLDocument(t, forgedBody), "auth.error.invalid_credentials") != nil {
		t.Fatal("a plaintext flash payload behind the version envelope must not be honored")
	}
	assertBodyNotContainsAll(t, forgedBody,
		bodyStringMatch{fragment: "forged-flash@example.com", message: "did not expect a forged flash email to reach the page"},
	)

	cleared := responseCookie(forgedResponse.Cookies(), flashCookieName)
	if cleared == nil || cleared.Value != "" {
		t.Fatalf("expected the refused flash cookie to be cleared, got %#v", cleared)
	}
}

func loginPageWithFlashCookie(t *testing.T, app *fiber.App, flashValue string) *http.Response {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/login", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", flashCookieName+"="+flashValue)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	return response
}

// assertSealedCookieEnvelope is the shared sealing assertion for cookie values
// carried in the "<version>.<base64url payload>" envelope that
// secureCookieCodec.seal writes. It decodes the payload only: decoding the whole
// value fails unconditionally on the "." separator, which is what left the
// earlier form of this check — a plaintext test nested under `if err == nil` —
// permanently unexecuted. plaintextTarget is a pointer to the struct the cookie
// would carry in the clear; a payload that parses into it is not ciphertext.
func assertSealedCookieEnvelope(t *testing.T, rawValue string, plaintextTarget any) {
	t.Helper()

	version, encodedPayload, found := strings.Cut(strings.TrimSpace(rawValue), ".")
	if !found {
		t.Fatalf("expected a %q version envelope in cookie value, got %q", secureCookieVersion+".", rawValue)
	}
	if version != secureCookieVersion {
		t.Fatalf("expected cookie envelope version %q, got %q", secureCookieVersion, version)
	}

	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(encodedPayload))
	if err != nil {
		t.Fatalf("expected a base64url sealed payload, got %q: %v", encodedPayload, err)
	}
	if len(payload) == 0 {
		t.Fatal("expected a non-empty sealed payload")
	}
	if json.Unmarshal(payload, plaintextTarget) == nil {
		t.Fatalf("expected the cookie payload to be sealed ciphertext; it parsed as plaintext %T: %#v", plaintextTarget, plaintextTarget)
	}
}

func TestLegacyPlainFlashCookieIsIgnored(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	// Raw legacy shape: older builds persisted login_email in the flash cookie.
	// The field has since been removed, but the cookie must still be ignored
	// when presented as legacy plaintext.
	legacyPayload := []byte(`{"auth_error":"invalid email or password","login_email":"legacy-flash@example.com"}`)
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

	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(url.Values{
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

	tamperedValue := tamperSealedCookieValueForTest(t, flashCookie.Value)

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

func tamperSealedCookieValueForTest(t *testing.T, rawValue string) string {
	t.Helper()

	version, encodedPayload, found := strings.Cut(strings.TrimSpace(rawValue), ".")
	if !found || version != secureCookieVersion || strings.TrimSpace(encodedPayload) == "" {
		t.Fatalf("expected sealed cookie value with %q prefix, got %q", secureCookieVersion+".", rawValue)
	}

	payload, err := base64.RawURLEncoding.DecodeString(encodedPayload)
	if err != nil {
		t.Fatalf("decode sealed cookie payload: %v", err)
	}
	if len(payload) == 0 {
		t.Fatal("expected non-empty sealed cookie payload")
	}

	payload[len(payload)-1] ^= 0x01

	return version + "." + base64.RawURLEncoding.EncodeToString(payload)
}
