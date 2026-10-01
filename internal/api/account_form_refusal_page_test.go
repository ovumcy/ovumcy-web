package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/net/html"
)

// WEB-135: the account forms of the settings page and the two-factor page
// submitted without JavaScript answer a refusal raised before their handler (an
// idle CSRF token) as a page: the localized message and one link back to the
// page hosting the form, built from the route, with no cookie on it. A refusal
// the handler itself raises keeps the flash redirect it always had.

// accountFormSubject is an app with a signed-in owner, as one form's page needs.
type accountFormSubject struct {
	app     *fiber.App
	cookies map[string]string
}

type accountFormRefusalCase struct {
	page  string
	match func(*html.Node) bool
	back  string
	build func(t *testing.T, email string) accountFormSubject
}

func accountFormRefusalCases() map[string]accountFormRefusalCase {
	owner := func(t *testing.T, email string) accountFormSubject {
		ctx := newRefusalPageContext(t, email)
		return accountFormSubject{app: ctx.app, cookies: authCookieMap(t, ctx.authCookie)}
	}
	// signedInWithIdentity is the OIDC fixture: an owner with one linked identity,
	// with or without a local password, on an app where SSO is enabled.
	signedInWithIdentity := func(local bool) func(t *testing.T, email string) accountFormSubject {
		return func(t *testing.T, email string) accountFormSubject {
			fixture := newOIDCStepupFixtureWithOptions(t, email, onboardingTestAppOptions{envelopeTransportErrors: true}, nil)
			fixture.oidcStub.enabled = true
			if local {
				giveLinkFixtureAPassword(t, fixture)
			}
			fixture.oidcStub.linkedIdentities = []services.LinkedOIDCIdentity{
				{ID: fixture.identity.ID, Issuer: fixture.stepupIssuer, LinkedAt: time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)},
			}
			return accountFormSubject{app: fixture.app, cookies: authCookieMap(t, fixture.authCookie)}
		}
	}
	return map[string]accountFormRefusalCase{
		"profile": {
			page:  "/settings",
			match: formWithAttr("hx-patch", "/api/v1/users/current/profile"),
			back:  "/settings",
			build: owner,
		},
		"password change": {
			page:  "/settings",
			match: formWithAttr("hx-put", "/api/v1/users/current/password"),
			back:  "/settings",
			build: owner,
		},
		"recovery code": {
			page:  "/settings",
			match: formWithAttr("action", "/api/v1/users/current/recovery-code"),
			back:  "/settings",
			build: owner,
		},
		"local password step-up": {
			page:  "/settings",
			match: formWithFlag("data-settings-local-password-form"),
			back:  "/settings",
			build: signedInWithIdentity(false),
		},
		"identity link step-up": {
			page:  "/settings",
			match: formWithFlag("data-oidc-link-form"),
			back:  "/settings",
			build: signedInWithIdentity(true),
		},
		"identity unlink": {
			page:  "/settings",
			match: formWithFlag("data-oidc-unlink-form"),
			back:  "/settings",
			build: signedInWithIdentity(true),
		},
		"2fa enrol": {
			page:  "/settings/2fa",
			match: formWithAttr("hx-put", "/api/v1/users/current/2fa"),
			back:  "/settings/2fa",
			build: owner,
		},
		"2fa disable": {
			page:  "/settings/2fa",
			match: formWithAttr("hx-delete", "/api/v1/users/current/2fa"),
			back:  "/settings/2fa",
			build: func(t *testing.T, email string) accountFormSubject {
				ctx := newRefusalPageContext(t, email)
				if err := getTOTPServiceForTest(ctx.database).EnableTOTP(context.Background(), ctx.user.ID, ctx.user.AuthSessionVersion, "JBSWY3DPEHPK3PXP"); err != nil {
					t.Fatalf("EnableTOTP: %v", err)
				}
				ctx.refreshAuthCookie(t)
				return accountFormSubject{app: ctx.app, cookies: authCookieMap(t, ctx.authCookie)}
			},
		},
	}
}

func accountFormEmail(prefix string, name string) string {
	return prefix + "-" + strings.ReplaceAll(name, " ", "-") + "@example.com"
}

// TestNoJSAccountAndTwoFactorFormsAnswerAPreHandlerRefusalAsAPage submits each
// form without its CSRF token, as a page left open past the token's life does.
func TestNoJSAccountAndTwoFactorFormsAnswerAPreHandlerRefusalAsAPage(t *testing.T) {
	t.Parallel()

	forbidden := englishCopy(t, "common.error.forbidden")
	for name, c := range accountFormRefusalCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			subject := c.build(t, accountFormEmail("refusal-account", name))
			form := renderNoJSForm(t, subject.app, c.page, subject.cookies, c.match)

			response := form.submit(t, subject.app, url.Values{"password": {"StrongPass1"}}, "csrf_token")
			assertRefusalPageCarrying(t, response, http.StatusForbidden, forbidden, c.back)
		})
	}
}

// TestNoJSIdentityUnlinkRefusalLinkIgnoresTheIdentityInTheAction rewrites the
// unlink form's action to an id that is not an identity at all: the link back
// is the settings page whatever the path carried.
func TestNoJSIdentityUnlinkRefusalLinkIgnoresTheIdentityInTheAction(t *testing.T) {
	t.Parallel()

	forbidden := englishCopy(t, "common.error.forbidden")
	c := accountFormRefusalCases()["identity unlink"]
	for name, action := range map[string]string{
		"markup in the id":    "/api/v1/users/current/oidc/identities/%22%3E%3Cscript%3Ealert(1)%3E",
		"foreign numeric id":  "/api/v1/users/current/oidc/identities/987654",
		"source query is not": "/api/v1/users/current/oidc/identities/1?source=https://evil.example/",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			subject := c.build(t, accountFormEmail("refusal-unlink-id", name))
			form := renderNoJSForm(t, subject.app, c.page, subject.cookies, c.match)
			form.action = action

			response := form.submit(t, subject.app, url.Values{"password": {"StrongPass1"}}, "csrf_token")
			assertRefusalPageCarrying(t, response, http.StatusForbidden, forbidden, "/settings")
		})
	}
}

// TestNoJSAccountFormHandlerRefusalKeepsItsFlashRedirect pins the other side of
// the same form: a refusal the handler raises (a wrong current password) is not
// the page this change adds. It stays the flash redirect to /settings, so the
// message the owner reads there is the one the settings page renders.
func TestNoJSAccountFormHandlerRefusalKeepsItsFlashRedirect(t *testing.T) {
	t.Parallel()

	c := accountFormRefusalCases()["password change"]
	subject := c.build(t, "refusal-handler-raised@example.com")
	form := renderNoJSForm(t, subject.app, c.page, subject.cookies, c.match)

	response := form.submit(t, subject.app, url.Values{
		"current_password": {"NotThePassword9"},
		"new_password":     {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
	})
	defer func() { _ = response.Body.Close() }()
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/settings" {
		t.Fatalf("Location %q, want /settings", location)
	}
	flashed := false
	for _, cookie := range response.Cookies() {
		flashed = flashed || cookie.Name == flashCookieName
	}
	if !flashed {
		t.Fatal("the handler refusal no longer carries its flash cookie")
	}
}

// TestNoJSTwoFactorEnrolWrongCodeAnswersAPageKeepingItsStatus pins a refusal
// the handler raises on the shared envelope rather than for a settings form: it
// keeps its 401 and key, and now reads as a page with the link back to the
// two-factor page instead of the JSON envelope painted as the page.
func TestNoJSTwoFactorEnrolWrongCodeAnswersAPageKeepingItsStatus(t *testing.T) {
	t.Parallel()

	c := accountFormRefusalCases()["2fa enrol"]
	subject := c.build(t, "refusal-2fa-wrong-code@example.com")
	form := renderNoJSForm(t, subject.app, c.page, subject.cookies, c.match)
	if form.secret == "" {
		t.Fatal("the enrolment page showed no manual secret")
	}

	response := form.submit(t, subject.app, url.Values{
		"code":     {invalidTOTPCodeForSkewWindow(t, form.secret)},
		"password": {"StrongPass1"},
	})
	assertRefusalPageCarrying(t, response, http.StatusUnauthorized, englishCopy(t, services.AuthErrorTranslationKey("totp invalid code")), "/settings/2fa")
}

// TestAccountFormRefusalsKeepTheirEnvelopeForOtherClients pins that the page is
// for a form submitted without JavaScript: a caller that asks for JSON, one
// that sends the real verb and an htmx request keep what they always had.
func TestAccountFormRefusalsKeepTheirEnvelopeForOtherClients(t *testing.T) {
	t.Parallel()

	ctx := newRefusalPageContext(t, "refusal-account-other-clients@example.com")
	send := func(method string, target string, body string, headers map[string]string) *http.Response {
		request := httptest.NewRequest(method, target, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Accept-Language", "en")
		request.Header.Set("Cookie", ctx.authCookie)
		for key, value := range headers {
			request.Header.Set(key, value)
		}
		return mustAppResponse(t, ctx.app, request)
	}
	assertEnvelope := func(t *testing.T, response *http.Response) {
		t.Helper()
		defer func() { _ = response.Body.Close() }()
		assertStatusCode(t, response, http.StatusForbidden)
		if contentType := response.Header.Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
			t.Fatalf("Content-Type %q, want JSON", contentType)
		}
		if body := mustReadBodyString(t, response.Body); !strings.Contains(body, `"error":"forbidden"`) {
			t.Fatalf("body %s lacks the forbidden envelope", body)
		}
	}

	t.Run("form POST that asks for JSON", func(t *testing.T) {
		assertEnvelope(t, send(http.MethodPost, "/api/v1/users/current/recovery-code", "password=StrongPass1", map[string]string{"Accept": "application/json"}))
	})
	t.Run("real DELETE, browser Accept", func(t *testing.T) {
		assertEnvelope(t, send(http.MethodDelete, "/api/v1/users/current/2fa", "password=StrongPass1", map[string]string{"Accept": noJSBrowserAccept}))
	})
	t.Run("htmx", func(t *testing.T) {
		response := send(http.MethodPut, "/api/v1/users/current/password", "current_password=x", map[string]string{"HX-Request": "true"})
		defer func() { _ = response.Body.Close() }()
		assertStatusCode(t, response, http.StatusForbidden)
		body := mustReadBodyString(t, response.Body)
		if !strings.Contains(body, "data-flash-key") || strings.Contains(body, "href=") {
			t.Fatalf("htmx refusal is not the bare status fragment: %s", body)
		}
	})
}
