package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"golang.org/x/net/html"
)

func TestLoginInvalidCredentialsRedirectDoesNotPersistEmail(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "login-email@example.com", "StrongPass1", true)

	response := mustAppResponse(t, app, loginInvalidCredentialsRequest(user.Email))
	assertStatusCode(t, response, http.StatusSeeOther)

	redirectURL := mustParseLocationHeader(t, response)
	assertCleanLoginRedirect(t, redirectURL)

	flashValue := responseCookieValue(response.Cookies(), flashCookieName)
	if flashValue == "" {
		t.Fatalf("expected flash cookie in login redirect response")
	}

	rendered := followLoginPageWithFlash(t, app, redirectURL.String(), flashValue)
	assertBodyContainsAll(t, rendered,
		bodyStringMatch{fragment: `id="login-email"`, message: "expected login email input in page"},
	)
	// Assert the error via its stable data-error-key hook, not the localized copy
	// — never strings.Contains on a localized phrase.
	if htmlAuthErrorByKey(mustParseHTMLDocument(t, rendered), "auth.error.invalid_credentials") == nil {
		t.Fatalf("expected invalid-credentials auth error surfaced via data-error-key, got %q", rendered)
	}
	// Email PII must not round-trip through the flash cookie (H-2): the address
	// is no longer echoed back into the form after a failed login.
	assertBodyNotContainsAll(t, rendered,
		bodyStringMatch{fragment: `value="login-email@example.com"`, message: "did not expect login email to be restored from flash"},
	)

	cleanPage := loadCleanLoginPage(t, app)
	if htmlAuthErrorByKey(mustParseHTMLDocument(t, cleanPage), "auth.error.invalid_credentials") != nil {
		t.Fatalf("did not expect auth error to persist after flash is consumed, got %q", cleanPage)
	}
	assertBodyNotContainsAll(t, cleanPage,
		bodyStringMatch{fragment: `value="login-email@example.com"`, message: "did not expect login email to persist after flash is consumed"},
	)
}

func loginInvalidCredentialsRequest(email string) *http.Request {
	form := url.Values{
		"email":    {email},
		"password": {"WrongPass1"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return request
}

func assertCleanLoginRedirect(t *testing.T, redirectURL *url.URL) {
	t.Helper()

	if redirectURL.Path != "/login" {
		t.Fatalf("expected redirect path /login, got %q", redirectURL.Path)
	}
	if query := strings.TrimSpace(redirectURL.RawQuery); query != "" {
		t.Fatalf("expected clean redirect without query params, got %q", query)
	}
}

func followLoginPageWithFlash(t *testing.T, app *fiber.App, location string, flashValue string) string {
	t.Helper()

	followRequest := httptest.NewRequest(http.MethodGet, location, nil)
	followRequest.Header.Set("Accept-Language", "en")
	followRequest.Header.Set("Cookie", flashCookieName+"="+flashValue)
	followResponse := mustAppResponse(t, app, followRequest)
	assertStatusCode(t, followResponse, http.StatusOK)
	return mustReadBodyString(t, followResponse.Body)
}

func loadCleanLoginPage(t *testing.T, app *fiber.App) string {
	t.Helper()

	afterFlashRequest := httptest.NewRequest(http.MethodGet, "/login", nil)
	afterFlashRequest.Header.Set("Accept-Language", "en")
	afterFlashResponse := mustAppResponse(t, app, afterFlashRequest)
	assertStatusCode(t, afterFlashResponse, http.StatusOK)
	return mustReadBodyString(t, afterFlashResponse.Body)
}

// assertAuthErrorAnnouncedForFields pins the WEB-263 contract on an auth page
// rendered with an error: the notice is an alert region with an id, and every
// field in invalidIDs is marked aria-invalid and described by that id, while
// every field in validIDs carries neither.
func assertAuthErrorAnnouncedForFields(t *testing.T, rendered string, errorKey string, invalidIDs []string, validIDs []string) {
	t.Helper()
	document := mustParseHTMLDocument(t, rendered)
	notice := htmlAuthErrorByKey(document, errorKey)
	if notice == nil {
		t.Fatalf("expected the %s error on the page", errorKey)
	}
	if htmlAttr(notice, "role") != "alert" {
		t.Fatalf("expected the auth error notice to be a role=alert region, got role=%q", htmlAttr(notice, "role"))
	}
	noticeID := htmlAttr(notice, "id")
	if noticeID == "" {
		t.Fatal("expected the auth error notice to carry an id the fields can be described by")
	}
	fieldByID := func(id string) *html.Node {
		node := htmlFindElement(document, func(node *html.Node) bool {
			return node.Type == html.ElementNode && htmlAttr(node, "id") == id
		})
		if node == nil {
			t.Fatalf("expected a field with id %q", id)
		}
		return node
	}
	for _, id := range invalidIDs {
		field := fieldByID(id)
		if htmlAttr(field, "aria-invalid") != "true" || htmlAttr(field, "aria-describedby") != noticeID {
			t.Fatalf("expected %q to be aria-invalid and described by %q, got aria-invalid=%q aria-describedby=%q", id, noticeID, htmlAttr(field, "aria-invalid"), htmlAttr(field, "aria-describedby"))
		}
	}
	for _, id := range validIDs {
		field := fieldByID(id)
		if htmlAttr(field, "aria-invalid") != "" || htmlAttr(field, "aria-describedby") != "" {
			t.Fatalf("did not expect %q to be marked invalid, got aria-invalid=%q aria-describedby=%q", id, htmlAttr(field, "aria-invalid"), htmlAttr(field, "aria-describedby"))
		}
	}
}

// TestLoginInvalidCredentialsErrorIsAnnouncedAndTiedToBothFields pins WEB-263
// on the sign-in page: a refused password renders an alert region, and both
// operands the refusal compared are marked invalid — never only one, which
// would say which of them was wrong.
func TestLoginInvalidCredentialsErrorIsAnnouncedAndTiedToBothFields(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "login-a11y@example.com", "StrongPass1", true)

	response := mustAppResponse(t, app, loginInvalidCredentialsRequest(user.Email))
	assertStatusCode(t, response, http.StatusSeeOther)
	flashValue := responseCookieValue(response.Cookies(), flashCookieName)
	if flashValue == "" {
		t.Fatalf("expected flash cookie in login redirect response")
	}

	rendered := followLoginPageWithFlash(t, app, mustParseLocationHeader(t, response).String(), flashValue)
	assertAuthErrorAnnouncedForFields(t, rendered, "auth.error.invalid_credentials",
		[]string{"login-email", "login-password"}, []string{"login-remember-me"})

	// With no error there is no notice to describe the fields by.
	cleanPage := loadCleanLoginPage(t, app)
	if strings.Contains(cleanPage, `aria-invalid="true"`) || strings.Contains(cleanPage, `aria-describedby="auth-server-error"`) {
		t.Fatalf("did not expect invalid-field markup on a clean sign-in page")
	}
}

// TestForgotPasswordInvalidCodeErrorIsTiedToBothOperands pins WEB-263 on the
// recovery page: a refused code names the code and the password it was
// compared beside, never one of them, and the clean page carries no mark.
func TestForgotPasswordInvalidCodeErrorIsTiedToBothOperands(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "forgot-a11y@example.com", "StrongPass1", true)

	form := url.Values{
		"email":         {user.Email},
		"recovery_code": {"OVUM-AAAA-BBBB-CCCC"},
		"password":      {"StrongPass1"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/password-resets", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusSeeOther)
	flashValue := responseCookieValue(response.Cookies(), flashCookieName)
	if flashValue == "" {
		t.Fatalf("expected flash cookie in the refused recovery redirect")
	}

	rendered := followLoginPageWithFlash(t, app, mustParseLocationHeader(t, response).String(), flashValue)
	assertAuthErrorAnnouncedForFields(t, rendered, "auth.error.invalid_recovery_code",
		[]string{"recovery-code", "recovery-password"}, nil)
}

// TestRegisterPasswordMismatchErrorIsTiedToTheConfirmField pins the
// field-specific half of WEB-263: a mismatch is about the confirmation field
// alone, so only that field is marked.
func TestRegisterPasswordMismatchErrorIsTiedToTheConfirmField(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	form := url.Values{
		"email":            {"register-a11y@example.com"},
		"password":         {"StrongPass1"},
		"confirm_password": {"StrongPass2"},
		"consent":          {"true"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusSeeOther)
	flashValue := responseCookieValue(response.Cookies(), flashCookieName)
	if flashValue == "" {
		t.Fatalf("expected flash cookie in register redirect response")
	}

	rendered := followLoginPageWithFlash(t, app, mustParseLocationHeader(t, response).String(), flashValue)
	assertAuthErrorAnnouncedForFields(t, rendered, "auth.error.password_mismatch",
		[]string{"register-confirm-password"}, []string{"register-email", "register-password", "register-consent"})
}
