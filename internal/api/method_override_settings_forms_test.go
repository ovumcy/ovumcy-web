package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/net/html"

	"github.com/gofiber/fiber/v3"
)

// WEB-107: the settings forms that htmx submits as PUT or DELETE also carry
// action + method="post" for the no-JavaScript path. Without the hidden
// _method field that POST reached a URL registered only for the real verb
// (405) or, for the calendar feed, the POST route that does the opposite
// thing. These tests render each form, submit exactly the hidden inputs the
// page gave it plus what a person would type, and read the effect from the
// database — so dropping the hidden field from a template fails here too, not
// only in the template guard.

// noJSForm is one form as a browser without JavaScript would submit it.
type noJSForm struct {
	action  string
	fields  url.Values
	cookies map[string]string
	// secret is the manual TOTP secret the 2FA setup page shows, when it does.
	secret string
}

// renderNoJSForm GETs page with cookies and returns the first form match
// selects, its hidden inputs, and the cookies the browser holds afterwards.
func renderNoJSForm(t *testing.T, app *fiber.App, page string, cookies map[string]string, match func(*html.Node) bool) noJSForm {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, page, nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", cookieHeaderFromMap(cookies))
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))

	form := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "form" && match(node)
	})
	if form == nil {
		t.Fatalf("GET %s rendered no matching form", page)
	}
	if !strings.EqualFold(htmlAttr(form, "method"), http.MethodPost) {
		t.Fatalf("form on %s: method=%q, want post", page, htmlAttr(form, "method"))
	}

	fields := url.Values{}
	for _, input := range htmlFindElements(form, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "input" && htmlAttr(node, "type") == "hidden"
	}) {
		fields.Add(htmlAttr(input, "name"), htmlAttr(input, "value"))
	}

	held := map[string]string{}
	for name, value := range cookies {
		held[name] = value
	}
	for _, cookie := range response.Cookies() {
		held[cookie.Name] = cookie.Value
	}
	secret := strings.TrimSpace(htmlNodeText(htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-totp-manual-secret")
	})))
	return noJSForm{action: htmlAttr(form, "action"), fields: fields, cookies: held, secret: secret}
}

func cookieHeaderFromMap(cookies map[string]string) string {
	pairs := make([]string, 0, len(cookies))
	for name, value := range cookies {
		pairs = append(pairs, name+"="+value)
	}
	return strings.Join(pairs, "; ")
}

// submit POSTs the form's hidden inputs merged with typed, as a browser does
// for a form with method="post" and no enctype. drop removes named fields
// first, for the negative cases.
func (form noJSForm) submit(t *testing.T, app *fiber.App, typed url.Values, drop ...string) *http.Response {
	t.Helper()
	if form.action == "" {
		t.Fatal("form has no action: a no-JS submit would post to the page itself")
	}
	body := cloneFormValues(form.fields)
	for name, values := range typed {
		body[name] = values
	}
	for _, name := range drop {
		body.Del(name)
	}
	request := httptest.NewRequest(http.MethodPost, form.action, strings.NewReader(body.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "text/html,application/xhtml+xml")
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", cookieHeaderFromMap(form.cookies))
	return mustAppResponse(t, app, request)
}

type noJSFormCase struct {
	app     *fiber.App
	page    string
	cookies map[string]string
	match   func(*html.Node) bool
	typed   func(t *testing.T, form noJSForm) url.Values
	verb    string
	// redirect is the Location a successful no-JS submit must answer with: a
	// browser follows a 303 See Other and lands there; anything else leaves the
	// person on a raw response document.
	redirect string
	happened func(t *testing.T) bool
}

func authCookieMap(t *testing.T, authCookie string) map[string]string {
	t.Helper()
	name, value, ok := strings.Cut(authCookie, "=")
	if !ok {
		t.Fatalf("malformed auth cookie %q", authCookie)
	}
	return map[string]string{name: value}
}

func formWithAttr(name string, value string) func(*html.Node) bool {
	return func(node *html.Node) bool { return htmlHasAttr(node, name) && htmlAttr(node, name) == value }
}

func formWithFlag(name string) func(*html.Node) bool {
	return func(node *html.Node) bool { return htmlHasAttr(node, name) }
}

func reloadUserForNoJSForm(t *testing.T, ctx settingsSecurityTestContext) models.User {
	t.Helper()
	var user models.User
	if err := ctx.database.First(&user, ctx.user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	return user
}

func calendarRevokeCase(t *testing.T) (noJSFormCase, settingsSecurityTestContext) {
	ctx := newSettingsSecurityTestContext(t, "nojs-calendar-revoke@example.com")
	armCalendarFeedForUser(t, ctx.database, ctx.user.ID)
	armed := reloadUserForNoJSForm(t, ctx).CalendarFeedSelector
	if armed == "" {
		t.Fatal("precondition: the armed feed stored no selector")
	}
	return noJSFormCase{
		app:      ctx.app,
		page:     "/settings",
		cookies:  authCookieMap(t, ctx.authCookie),
		match:    formWithFlag("data-settings-calendar-feed-revoke"),
		verb:     http.MethodDelete,
		redirect: "/settings",
		happened: func(t *testing.T) bool {
			return reloadUserForNoJSForm(t, ctx).CalendarFeedSelector == ""
		},
	}, ctx
}

func TestNoJSSettingsFormsPerformTheActionTheyName(t *testing.T) {
	t.Parallel()

	cases := map[string]func(t *testing.T) noJSFormCase{
		"calendar feed revoke": func(t *testing.T) noJSFormCase {
			c, _ := calendarRevokeCase(t)
			return c
		},
		"symptom hide": func(t *testing.T) noJSFormCase {
			ctx := newSettingsSecurityTestContext(t, "nojs-symptom-hide@example.com")
			symptom := models.SymptomType{UserID: ctx.user.ID, Name: "NoJS custom", Icon: "A", Color: "#111111"}
			if err := ctx.database.Create(&symptom).Error; err != nil {
				t.Fatalf("create symptom: %v", err)
			}
			target := "/api/v1/symptoms/" + strconv.FormatUint(uint64(symptom.ID), 10)
			return noJSFormCase{
				app:      ctx.app,
				page:     "/settings",
				cookies:  authCookieMap(t, ctx.authCookie),
				match:    formWithAttr("hx-delete", target),
				verb:     http.MethodDelete,
				redirect: "/settings",
				happened: func(t *testing.T) bool {
					var stored models.SymptomType
					if err := ctx.database.First(&stored, symptom.ID).Error; err != nil {
						t.Fatalf("reload symptom: %v", err)
					}
					return stored.ArchivedAt != nil
				},
			}
		},
		"change password": func(t *testing.T) noJSFormCase {
			ctx := newSettingsSecurityTestContext(t, "nojs-change-password@example.com")
			return noJSFormCase{
				app:      ctx.app,
				page:     "/settings",
				cookies:  authCookieMap(t, ctx.authCookie),
				match:    formWithAttr("id", "settings-change-password-form"),
				verb:     http.MethodPut,
				redirect: "/settings",
				typed: func(*testing.T, noJSForm) url.Values {
					return url.Values{
						"current_password": {"StrongPass1"},
						"new_password":     {"EvenStronger2"},
						"confirm_password": {"EvenStronger2"},
					}
				},
				happened: func(t *testing.T) bool {
					user := reloadUserForNoJSForm(t, ctx)
					return bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("EvenStronger2")) == nil
				},
			}
		},
		"oidc identity unlink": func(t *testing.T) noJSFormCase {
			fixture := newOIDCStepupFixture(t, "nojs-oidc-unlink@example.com")
			fixture.oidcStub.enabled = true
			giveLinkFixtureAPassword(t, fixture)
			fixture.oidcStub.linkedIdentities = []services.LinkedOIDCIdentity{
				{ID: fixture.identity.ID, Issuer: fixture.stepupIssuer, LinkedAt: time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)},
			}
			return noJSFormCase{
				app:      fixture.app,
				page:     "/settings",
				cookies:  authCookieMap(t, fixture.authCookie),
				match:    formWithFlag("data-oidc-unlink-form"),
				verb:     http.MethodDelete,
				redirect: "/settings",
				typed: func(*testing.T, noJSForm) url.Values {
					return url.Values{"password": {linkFixturePassword}}
				},
				happened: func(t *testing.T) bool {
					if fixture.oidcStub.unlinkCalls == 0 {
						return false
					}
					if fixture.oidcStub.lastUnlinkIdentityID != fixture.identity.ID || fixture.oidcStub.lastUnlinkUserID != fixture.user.ID {
						t.Fatalf("unlinked identity %d for user %d, want identity %d for user %d",
							fixture.oidcStub.lastUnlinkIdentityID, fixture.oidcStub.lastUnlinkUserID, fixture.identity.ID, fixture.user.ID)
					}
					return true
				},
			}
		},
		"2fa disable": func(t *testing.T) noJSFormCase {
			ctx := newTOTPSettingsContext(t, "nojs-2fa-disable@example.com")
			if err := getTOTPServiceForTest(ctx.database).EnableTOTP(context.Background(), ctx.user.ID, ctx.user.AuthSessionVersion, "JBSWY3DPEHPK3PXP"); err != nil {
				t.Fatalf("EnableTOTP: %v", err)
			}
			ctx.refreshAuthCookie(t)
			return noJSFormCase{
				app:      ctx.app,
				page:     "/settings/2fa",
				cookies:  authCookieMap(t, ctx.authCookie),
				match:    formWithAttr("hx-delete", "/api/v1/users/current/2fa"),
				verb:     http.MethodDelete,
				redirect: "/settings",
				typed: func(*testing.T, noJSForm) url.Values {
					return url.Values{"password": {"StrongPass1"}}
				},
				happened: func(t *testing.T) bool {
					return !totpStateForUser(t, ctx, ctx.user.ID).TOTPEnabled
				},
			}
		},
		"2fa enable": func(t *testing.T) noJSFormCase {
			ctx := newTOTPSettingsContext(t, "nojs-2fa-enable@example.com")
			return noJSFormCase{
				app:      ctx.app,
				page:     "/settings/2fa",
				cookies:  authCookieMap(t, ctx.authCookie),
				match:    formWithAttr("hx-put", "/api/v1/users/current/2fa"),
				verb:     http.MethodPut,
				redirect: "/settings",
				typed: func(t *testing.T, form noJSForm) url.Values {
					code, err := totp.GenerateCode(form.secret, time.Now())
					if err != nil {
						t.Fatalf("GenerateCode: %v", err)
					}
					return url.Values{"code": {code}, "password": {"StrongPass1"}}
				},
				happened: func(t *testing.T) bool {
					return totpStateForUser(t, ctx, ctx.user.ID).TOTPEnabled
				},
			}
		},
	}

	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			runNoJSFormCase(t, build(t))
		})
	}
}

// runNoJSFormCase renders the case's form, proves the submit is refused without
// its csrf_token, then submits it as a browser without JavaScript would and
// checks the redirect and the effect.
func runNoJSFormCase(t *testing.T, c noJSFormCase) {
	t.Helper()
	form := renderNoJSForm(t, c.app, c.page, c.cookies, c.match)
	if got := form.fields.Get("_method"); got != c.verb {
		t.Fatalf("rendered _method=%q, want %q", got, c.verb)
	}
	typed := url.Values{}
	if c.typed != nil {
		typed = c.typed(t, form)
	}

	refused := form.submit(t, c.app, typed, "csrf_token")
	if refused.StatusCode != http.StatusForbidden {
		t.Fatalf("without csrf_token: status %d, want 403", refused.StatusCode)
	}
	if c.happened(t) {
		t.Fatal("without csrf_token the action ran anyway")
	}

	if c.typed != nil {
		typed = c.typed(t, form)
	}
	response := form.submit(t, c.app, typed)
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("no-JS submit: status %d, want 303 See Other: %s", response.StatusCode, mustReadBodyString(t, response.Body))
	}
	if location := response.Header.Get("Location"); location != c.redirect {
		t.Fatalf("no-JS submit: Location %q, want %q", location, c.redirect)
	}
	if !c.happened(t) {
		t.Fatalf("no-JS submit answered %d but the action did not run", response.StatusCode)
	}
}

// assertCalendarFeedRegenerated proves the request ran as the POST it arrived
// as: GenerateCalendarFeed replaced the feed rather than revoking it.
func assertCalendarFeedRegenerated(t *testing.T, ctx settingsSecurityTestContext, before string) {
	t.Helper()
	after := reloadUserForNoJSForm(t, ctx).CalendarFeedSelector
	if after == "" {
		t.Fatal("the feed was revoked: the ignored _method was honoured")
	}
	if after == before {
		t.Fatal("the feed is unchanged: the request never reached the POST route")
	}
}

// TestNoJSCalendarFeedRevokeHonoursOnlyTheFormField pins the other arms on the
// form where getting it wrong does the opposite of what was asked: a POST to
// the feed URL is GenerateCalendarFeed.
func TestNoJSCalendarFeedRevokeHonoursOnlyTheFormField(t *testing.T) {
	t.Parallel()

	revoked := func(t *testing.T, ctx settingsSecurityTestContext) bool {
		return reloadUserForNoJSForm(t, ctx).CalendarFeedSelector == ""
	}

	for _, verb := range []string{"GET", "CONNECT", "HEAD", "TRACE", "POST", "garbage"} {
		t.Run("_method="+verb+" is refused", func(t *testing.T) {
			t.Parallel()
			c, ctx := calendarRevokeCase(t)
			before := reloadUserForNoJSForm(t, ctx).CalendarFeedSelector
			form := renderNoJSForm(t, c.app, c.page, c.cookies, c.match)
			response := form.submit(t, c.app, url.Values{"_method": {verb}})
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("status %d, want 400", response.StatusCode)
			}
			if after := reloadUserForNoJSForm(t, ctx).CalendarFeedSelector; after != before {
				t.Fatalf("feed changed under a refused override: %q -> %q", before, after)
			}
		})
	}

	t.Run("_method in the query string is not read", func(t *testing.T) {
		t.Parallel()
		c, ctx := calendarRevokeCase(t)
		form := renderNoJSForm(t, c.app, c.page, c.cookies, c.match)
		before := reloadUserForNoJSForm(t, ctx).CalendarFeedSelector
		form.action += "?_method=DELETE"
		_ = form.submit(t, c.app, nil, "_method")
		assertCalendarFeedRegenerated(t, ctx, before)
	})

	t.Run("_method in a JSON body is not read", func(t *testing.T) {
		t.Parallel()
		c, ctx := calendarRevokeCase(t)
		form := renderNoJSForm(t, c.app, c.page, c.cookies, c.match)
		request := httptest.NewRequest(http.MethodPost, form.action, strings.NewReader(`{"_method":"DELETE"}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json")
		request.Header.Set("X-CSRF-Token", form.fields.Get("csrf_token"))
		request.Header.Set("Cookie", cookieHeaderFromMap(form.cookies))
		before := reloadUserForNoJSForm(t, ctx).CalendarFeedSelector
		_ = mustAppResponse(t, c.app, request)
		assertCalendarFeedRegenerated(t, ctx, before)
	})

	t.Run("htmx's real DELETE is unaffected by the field it now carries", func(t *testing.T) {
		t.Parallel()
		c, ctx := calendarRevokeCase(t)
		form := renderNoJSForm(t, c.app, c.page, c.cookies, c.match)
		body := cloneFormValues(form.fields)
		body.Set("_method", "PUT")
		request := httptest.NewRequest(http.MethodDelete, form.action, strings.NewReader(body.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("HX-Request", "true")
		request.Header.Set("Cookie", cookieHeaderFromMap(form.cookies))
		response := mustAppResponse(t, c.app, request)
		if response.StatusCode >= http.StatusBadRequest {
			t.Fatalf("htmx DELETE: status %d", response.StatusCode)
		}
		if !revoked(t, ctx) {
			t.Fatal("htmx DELETE did not revoke the feed")
		}
	})
}
