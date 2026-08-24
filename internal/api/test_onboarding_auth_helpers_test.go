package api

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// sharedTestConstantSpellings names the values this package's tests must reach
// through a constant rather than by repeating the literal. Both entries are
// values the app itself derives behaviour from: a helper that mints a token
// under a second copy of the secret, or reads a cookie under a second copy of
// its name, keeps agreeing with the app only until one copy moves — and the
// negative tests that depend on it (owner-only, unsupported-role, cross-owner)
// would still see their expected 403 afterwards, refused at cookie-open rather
// than at the role gate, which is the silent half of the failure.
var sharedTestConstantSpellings = []struct {
	literal   string
	constName string
}{
	{literal: `"test-secret-key"`, constName: "testAppSecretKey"},
	{literal: `"ovumcy_auth"`, constName: "authCookieName"},
}

// TestTestHelpersUseTheSharedConstantsNotTheirValues sweeps the package's
// shared helper files — test_*_helpers_test.go, the recipes every other test
// builds its app and its cookies from — for a literal that duplicates one of
// those constants. The scope is deliberately the helpers and not the whole
// package: a copy inside one regression test misleads that test alone, while a
// copy inside a helper is inherited by every caller that never sees it.
func TestTestHelpersUseTheSharedConstantsNotTheirValues(t *testing.T) {
	t.Run("the sweep can tell the two spellings apart", func(t *testing.T) {
		for _, spelling := range sharedTestConstantSpellings {
			restated := "codec, err := newSecureCookieCodec([]byte(" + spelling.literal + "))"
			if findings := restatedConstantLines("fixture_test.go", restated, spelling.literal, spelling.constName); len(findings) != 1 {
				t.Fatalf("expected the restated %s to be reported once, got %v", spelling.constName, findings)
			}
			viaConstant := "codec, err := newSecureCookieCodec([]byte(" + spelling.constName + "))"
			if findings := restatedConstantLines("fixture_test.go", viaConstant, spelling.literal, spelling.constName); len(findings) != 0 {
				t.Fatalf("expected the %s spelling to pass, got %v", spelling.constName, findings)
			}
		}
	})

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	declared := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "test_") || !strings.HasSuffix(name, "_helpers_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, spelling := range sharedTestConstantSpellings {
			if strings.Contains(string(source), "= "+spelling.literal) {
				declared[spelling.constName] = true
			}
			for _, finding := range restatedConstantLines(name, string(source), spelling.literal, spelling.constName) {
				t.Errorf("%s: %s is the value of %s — use the constant", finding, spelling.literal, spelling.constName)
			}
		}
	}
	// The cookie name is a production constant, so only the test-owned secret
	// has a declaration to find here; without it the sweep above would be
	// judging a value nothing defines.
	if !declared["testAppSecretKey"] {
		t.Fatal("testAppSecretKey is no longer declared in this package: the sweep has nothing to point offenders at")
	}
}

// restatedConstantLines reports every line of source that spells literal
// without naming the constant that already holds it — the declaration itself,
// and this sweep's own table, name it and pass.
func restatedConstantLines(name string, source string, literal string, constName string) []string {
	findings := []string{}
	for index, line := range strings.Split(source, "\n") {
		if strings.Contains(line, literal) && !strings.Contains(line, constName) {
			findings = append(findings, fmt.Sprintf("%s:%d", name, index+1))
		}
	}
	return findings
}

func loginAndExtractAuthCookie(t *testing.T, app *fiber.App, email string, password string) string {
	t.Helper()

	form := url.Values{
		"email":    {email},
		"password": {password},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("login request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected login status 303, got %d", response.StatusCode)
	}

	for _, cookie := range response.Cookies() {
		if cookie.Name == authCookieName && cookie.Value != "" {
			return cookie.Name + "=" + cookie.Value
		}
	}

	t.Fatal("auth cookie is missing in login response")
	return ""
}

func loginAndExtractAuthCookieWithCSRF(t *testing.T, app *fiber.App, email string, password string) string {
	t.Helper()

	csrfRequest := httptest.NewRequest(http.MethodGet, "/login", nil)
	csrfResponse, err := app.Test(csrfRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("load login page for csrf token failed: %v", err)
	}
	defer func() { _ = csrfResponse.Body.Close() }()

	if csrfResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected login page status 200, got %d", csrfResponse.StatusCode)
	}

	body, err := io.ReadAll(csrfResponse.Body)
	if err != nil {
		t.Fatalf("read login page body for csrf token failed: %v", err)
	}
	csrfToken := extractCSRFTokenFromAuthPage(t, string(body))
	csrfCookie := responseCookie(csrfResponse.Cookies(), "ovumcy_csrf")
	if csrfCookie == nil || strings.TrimSpace(csrfCookie.Value) == "" {
		t.Fatal("csrf cookie is missing in login page response")
	}

	form := url.Values{
		"email":      {email},
		"password":   {password},
		"csrf_token": {csrfToken},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", csrfCookie.Name+"="+csrfCookie.Value)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("login request with csrf failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected login status 303, got %d", response.StatusCode)
	}

	for _, cookie := range response.Cookies() {
		if cookie.Name == authCookieName && cookie.Value != "" {
			return cookie.Name + "=" + cookie.Value
		}
	}

	t.Fatal("auth cookie is missing in login response")
	return ""
}

func issueAuthCookieForUser(t *testing.T, user models.User) string {
	t.Helper()

	service := services.NewAuthService(nil)
	// The services builder mints for any role on purpose: this helper exists to
	// hand an unsupported-role account a well-formed cookie, so the owner-only
	// route matrix can prove the request is refused at the gate rather than at
	// the mint. Going through the handler's own buildTokenWithSessionID would
	// refuse to issue one and leave those routes untested.
	token, _, err := service.BuildAuthSessionTokenWithSessionID([]byte(testAppSecretKey), user.ID, user.Role, user.AuthSessionVersion, time.Hour, time.Now())
	if err != nil {
		t.Fatalf("build auth session token: %v", err)
	}

	codec, err := newSecureCookieCodec([]byte(testAppSecretKey))
	if err != nil {
		t.Fatalf("init secure cookie codec: %v", err)
	}
	sealed, err := codec.seal(authCookieName, []byte(token))
	if err != nil {
		t.Fatalf("seal auth cookie token: %v", err)
	}
	return authCookieName + "=" + sealed
}

var csrfTokenMetaPatternForAuthTests = regexp.MustCompile(`<meta name="csrf-token" content="([^"]+)"`)

func extractCSRFTokenFromAuthPage(t *testing.T, html string) string {
	t.Helper()

	match := csrfTokenMetaPatternForAuthTests.FindStringSubmatch(html)
	if len(match) < 2 {
		t.Fatalf("expected csrf token meta tag in auth page html")
	}

	token := strings.TrimSpace(match[1])
	if token == "" {
		t.Fatalf("expected non-empty csrf token value")
	}
	return token
}
