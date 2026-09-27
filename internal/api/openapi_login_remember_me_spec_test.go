package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/services"
	"github.com/pquerna/otp/totp"
)

var (
	rememberMeSpecPersistentDays = regexp.MustCompile("its `Expires` is set (\\d+) days after sign-in, and the session token sealed inside it expires at the same time")
	rememberMeSpecSessionDays    = regexp.MustCompile("the session token inside still expires (\\d+) days after sign-in")
)

// rememberMeSpecLifetimes reads the two lifetimes LoginRequest.remember_me
// publishes, so the endpoint judges the spec's own numbers rather than a copy
// of the server constants.
func rememberMeSpecLifetimes(t *testing.T) (description string, persistent time.Duration, sessionScoped time.Duration) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read openapi spec: %v", err)
	}
	block := openAPIYAMLBlock(t, string(data), "components", "schemas", "LoginRequest", "properties", "remember_me")
	description = strings.Join(block, " ")

	days := func(pattern *regexp.Regexp) time.Duration {
		match := pattern.FindStringSubmatch(description)
		if match == nil {
			t.Fatalf("LoginRequest.remember_me: docs/openapi.yaml no longer states %q:\n  %s", pattern, description)
		}
		count, err := strconv.Atoi(match[1])
		if err != nil || count <= 0 {
			t.Fatalf("LoginRequest.remember_me: unusable day count %q", match[1])
		}
		return time.Duration(count) * 24 * time.Hour
	}
	return description, days(rememberMeSpecPersistentDays), days(rememberMeSpecSessionDays)
}

// TestOpenAPILoginRememberMeDeclaresTheCookieItIssues pins what
// docs/openapi.yaml says about LoginRequest.remember_me against the cookie
// POST /api/v1/sessions actually sets: the persistent lifetime, the
// session-scoped cookie's own token lifetime, and the attributes. The spec
// once promised a ~7-day Expires for a remembered sign-in that has always been
// given 30 days, and nothing compared the two.
//
// Both lifetimes are read out of the spec, and both are checked twice: on the
// cookie's Expires (what the browser keeps) and on the sealed token's own
// expiry (what the server accepts), because the second is the one that ends a
// session-scoped cookie in a browser that never closes.
func TestOpenAPILoginRememberMeDeclaresTheCookieItIssues(t *testing.T) {
	description, persistent, sessionScoped := rememberMeSpecLifetimes(t)
	for _, phrase := range []string{
		"neither `Expires` nor `Max-Age`",
		"`HttpOnly`, `SameSite=Lax` and `Path=/`",
		"`Secure` when the server runs with `COOKIE_SECURE` enabled",
	} {
		if !strings.Contains(description, phrase) {
			t.Fatalf("LoginRequest.remember_me: docs/openapi.yaml no longer states %q:\n  %s", phrase, description)
		}
	}

	for _, cookieSecure := range []bool{false, true} {
		app, database := newOnboardingTestAppWithCookieSecure(t, cookieSecure)
		for _, transport := range []string{"json", "form"} {
			for _, remember := range []bool{false, true} {
				name := transport + "/remember=" + strconv.FormatBool(remember) + "/cookie_secure=" + strconv.FormatBool(cookieSecure)
				t.Run(name, func(t *testing.T) {
					email := "remember-spec-" + transport + "-" + strconv.FormatBool(remember) + "-" + strconv.FormatBool(cookieSecure) + "@example.com"
					createOnboardingTestUser(t, database, email, "StrongPass1", true)

					request := rememberMeSpecLoginRequest(transport, email, remember)
					issuedFrom := time.Now()
					response := mustAppResponse(t, app, request)
					issuedTo := time.Now()
					if transport == "json" {
						assertStatusCode(t, response, http.StatusOK)
					} else {
						assertStatusCode(t, response, http.StatusSeeOther)
					}

					want := sessionScoped
					if remember {
						want = persistent
					}
					assertRememberMeSpecCookie(t, response, remember, cookieSecure, want, issuedFrom, issuedTo)
				})
			}
		}
	}
}

// TestOpenAPILoginRememberMeSurvivesTheTOTPChallenge pins the spec's last
// sentence: a sign-in answered with requires_totp issues no session itself, so
// the choice has to travel through the pending cookie to the one
// POST /api/v1/sessions/2fa-challenge mints.
func TestOpenAPILoginRememberMeSurvivesTheTOTPChallenge(t *testing.T) {
	description, persistent, sessionScoped := rememberMeSpecLifetimes(t)
	if !strings.Contains(description, "`POST /api/v1/sessions/2fa-challenge` issues carries the lifetime chosen here") {
		t.Fatalf("LoginRequest.remember_me: docs/openapi.yaml no longer says the 2FA challenge keeps the choice:\n  %s", description)
	}

	for _, remember := range []bool{false, true} {
		t.Run("remember="+strconv.FormatBool(remember), func(t *testing.T) {
			app, database := newOnboardingTestAppWithCSRF(t)
			user := createOnboardingTestUser(t, database, "remember-spec-totp-"+strconv.FormatBool(remember)+"@example.com", "StrongPass1", true)
			rawSecret := setupTOTPForUser(t, database, user.ID, []byte(testAppSecretKey))

			csrfToken, csrfCookieHeader := extractCSRFCookieAndToken(t, app)
			form := url.Values{"email": {user.Email}, "password": {"StrongPass1"}, "csrf_token": {csrfToken}}
			if remember {
				form.Set("remember_me", "1")
			}
			login := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(form.Encode()))
			login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			login.Header.Set("Cookie", csrfCookieHeader)
			loginResponse := mustAppResponse(t, app, login)
			assertStatusCode(t, loginResponse, http.StatusSeeOther)
			if cookie := responseCookie(loginResponse.Cookies(), authCookieName); cookie != nil && cookie.Value != "" {
				t.Fatal("the first factor alone issued ovumcy_auth; the challenge leg below would prove nothing")
			}
			pending := responseCookie(loginResponse.Cookies(), totpPendingCookieName)
			if pending == nil || pending.Value == "" {
				t.Fatal("expected a TOTP pending cookie from the first factor")
			}

			code, err := totp.GenerateCode(rawSecret, time.Now())
			if err != nil {
				t.Fatalf("GenerateCode: %v", err)
			}
			issuedFrom := time.Now()
			challengeResponse := doTOTPChallengeRequest(t, app, joinCookieHeader(pending.Name+"="+pending.Value, csrfCookieHeader), code, csrfToken)
			defer func() { _ = challengeResponse.Body.Close() }()
			issuedTo := time.Now()
			assertStatusCode(t, challengeResponse, http.StatusSeeOther)

			want := sessionScoped
			if remember {
				want = persistent
			}
			assertRememberMeSpecCookie(t, challengeResponse, remember, false, want, issuedFrom, issuedTo)
		})
	}
}

func rememberMeSpecLoginRequest(transport string, email string, remember bool) *http.Request {
	if transport == "json" {
		body := `{"email":` + strconv.Quote(email) + `,"password":"StrongPass1"`
		if remember {
			body += `,"remember_me":true`
		}
		body += "}"
		request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json")
		return request
	}
	form := url.Values{"email": {email}, "password": {"StrongPass1"}}
	if remember {
		form.Set("remember_me", "1")
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return request
}

// assertRememberMeSpecCookie checks one issued ovumcy_auth cookie against the
// spec. HTTP dates and JWT expiry both carry whole seconds, so the lower edge
// is truncated to the second before comparing.
func assertRememberMeSpecCookie(t *testing.T, response *http.Response, remember bool, cookieSecure bool, lifetime time.Duration, issuedFrom time.Time, issuedTo time.Time) {
	t.Helper()
	cookie := responseCookie(response.Cookies(), authCookieName)
	if cookie == nil || cookie.Value == "" {
		t.Fatal("expected an ovumcy_auth cookie")
	}
	earliest := issuedFrom.Add(lifetime).Truncate(time.Second)
	latest := issuedTo.Add(lifetime)
	inWindow := func(at time.Time) bool { return !at.Before(earliest) && !at.After(latest) }

	if !cookie.HttpOnly {
		t.Error("ovumcy_auth is not HttpOnly")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("ovumcy_auth SameSite = %v, want Lax", cookie.SameSite)
	}
	if cookie.Path != "/" {
		t.Errorf("ovumcy_auth Path = %q, want /", cookie.Path)
	}
	if cookie.Secure != cookieSecure {
		t.Errorf("ovumcy_auth Secure = %v with COOKIE_SECURE=%v", cookie.Secure, cookieSecure)
	}

	if remember {
		if !inWindow(cookie.Expires) {
			t.Errorf("ovumcy_auth Expires = %s, want %s after sign-in (%s..%s)", cookie.Expires, lifetime, earliest, latest)
		}
	} else {
		for _, header := range response.Header.Values("Set-Cookie") {
			if !strings.HasPrefix(header, authCookieName+"=") {
				continue
			}
			lower := strings.ToLower(header)
			if strings.Contains(lower, "expires=") || strings.Contains(lower, "max-age=") {
				t.Errorf("session-scoped ovumcy_auth carries a lifetime attribute: %s", header)
			}
		}
	}

	codec, err := newSecureCookieCodec([]byte(testAppSecretKey))
	if err != nil {
		t.Fatalf("newSecureCookieCodec: %v", err)
	}
	token, err := codec.open(authCookieName, cookie.Value)
	if err != nil {
		t.Fatalf("open ovumcy_auth: %v", err)
	}
	claims, err := services.ParseAuthSessionToken([]byte(testAppSecretKey), string(token), issuedTo)
	if err != nil {
		t.Fatalf("parse the session token inside ovumcy_auth: %v", err)
	}
	if claims.ExpiresAt == nil || !inWindow(claims.ExpiresAt.Time) {
		t.Errorf("session token expiry = %v, want %s after sign-in (%s..%s)", claims.ExpiresAt, lifetime, earliest, latest)
	}
}
