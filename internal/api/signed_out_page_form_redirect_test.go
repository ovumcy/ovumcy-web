package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/httpx"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// WEB-264: a form submitted without JavaScript after the session ended used to
// land on the bare status fragment — no layout, no lang attribute — whose link
// back only bounced off AuthRequired to the sign-in page anyway. It is a 303 to
// /login now, the notice riding the flash the sign-in page reads. htmx and JSON
// clients keep their 401 exactly as before.

// sendSignedOutForm posts a form with a valid CSRF proof and no session, the
// way a page left open past the end of its session submits.
func sendSignedOutForm(t *testing.T, ctx settingsSecurityTestContext, target string, fields url.Values, headers map[string]string) *http.Response {
	t.Helper()
	body := url.Values{}
	for key, values := range fields {
		body[key] = values
	}
	body.Set("csrf_token", ctx.csrfToken)
	request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept-Language", "de")
	request.Header.Set("Cookie", ctx.csrfCookie.Name+"="+ctx.csrfCookie.Value)
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	return mustAppResponse(t, ctx.app, request)
}

// assertSentToSignIn requires the 303 to a bare /login carrying the sealed
// "unauthorized" notice, and returns that flash value.
func assertSentToSignIn(t *testing.T, response *http.Response) string {
	t.Helper()
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusSeeOther)
	location, err := url.Parse(response.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	assertCleanLoginRedirect(t, location)
	for _, cookie := range response.Cookies() {
		if cookie.Name != flashCookieName || cookie.Value == "" {
			continue
		}
		if got := decodeFlashCookieForTest(t, cookie.Value).AuthError; got != unauthorizedErrorSpec().Key {
			t.Fatalf("flash AuthError = %q, want %q", got, unauthorizedErrorSpec().Key)
		}
		return cookie.Value
	}
	t.Fatal("the redirect to /login carries no flash notice")
	return ""
}

func TestNoJSDayFormWithoutASessionIsSentToSignIn(t *testing.T) {
	t.Parallel()

	german := strings.TrimSpace(mustLocaleMessages(t, i18n.LangDE)["common.error.unauthorized"])
	cases := map[string]struct {
		action string
		fields url.Values
	}{
		"calendar day save":         {action: "/api/v1/days/{iso}?source=calendar", fields: url.Values{"_method": {"PUT"}, "is_period": {"true"}}},
		"calendar day delete":       {action: "/api/v1/days/{iso}?source=calendar", fields: url.Values{"_method": {"DELETE"}}},
		"dashboard day save":        {action: "/api/v1/days/{iso}", fields: url.Values{"_method": {"PUT"}, "is_period": {"true"}}},
		"calendar cycle-start mark": {action: "/api/v1/days/{iso}/cycle-start?source=calendar"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := newRefusalPageContext(t, "signed-out-"+strings.ReplaceAll(name, " ", "-")+"@example.com")
			_, iso := noJSDay()

			response := sendSignedOutForm(t, ctx, strings.ReplaceAll(c.action, "{iso}", iso), c.fields, map[string]string{"Accept": noJSBrowserAccept})
			flash := assertSentToSignIn(t, response)

			request := httptest.NewRequest(http.MethodGet, "/login", nil)
			request.Header.Set("Accept-Language", "de")
			request.Header.Set("Cookie", flashCookieName+"="+flash)
			page := mustAppResponse(t, ctx.app, request)
			assertStatusCode(t, page, http.StatusOK)
			if body := mustReadBodyString(t, page.Body); !strings.Contains(body, german) {
				t.Fatalf("the sign-in page does not show the German notice %q", german)
			}
		})
	}
}

// TestSignedOutDayWriteKeepsItsAnswerForHTMXAndJSON pins the other side byte
// for byte: the redirect belongs to a browser form without JavaScript.
func TestSignedOutDayWriteKeepsItsAnswerForHTMXAndJSON(t *testing.T) {
	t.Parallel()

	spec := unauthorizedErrorSpec()
	copyKey := services.AuthErrorTranslationKey(spec.Key)
	wantFragment := httpx.StatusErrorMarkup(mustLocaleMessages(t, i18n.LangDE)[copyKey], copyKey)
	wantEnvelope, err := json.Marshal(apiErrorEnvelope(spec))
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	cases := map[string]struct {
		headers map[string]string
		want    string
	}{
		"htmx":                         {headers: map[string]string{"HX-Request": "true", "Accept": noJSBrowserAccept}, want: wantFragment},
		"JSON client":                  {headers: map[string]string{"Accept": "application/json"}, want: string(wantEnvelope)},
		"form POST with no Accept":     {headers: map[string]string{}, want: string(wantEnvelope)},
		"form POST accepting anything": {headers: map[string]string{"Accept": "*/*"}, want: string(wantEnvelope)},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := newRefusalPageContext(t, "signed-out-other-"+strings.ReplaceAll(name, " ", "-")+"@example.com")
			_, iso := noJSDay()

			response := sendSignedOutForm(t, ctx, "/api/v1/days/"+iso+"?source=calendar", url.Values{"_method": {"PUT"}, "is_period": {"true"}}, c.headers)
			defer func() { _ = response.Body.Close() }()
			assertStatusCode(t, response, http.StatusUnauthorized)
			if location := response.Header.Get("Location"); location != "" {
				t.Fatalf("Location %q, want none", location)
			}
			for _, cookie := range response.Cookies() {
				if cookie.Name == flashCookieName || cookie.Name == exemptFlashCookieName {
					t.Fatalf("refusal set the flash cookie %q", cookie.Name)
				}
			}
			if body := mustReadBodyString(t, response.Body); body != c.want {
				t.Fatalf("body = %q, want %q", body, c.want)
			}
		})
	}
}

// TestEveryNoJSPageFormRouteSendsASignedOutBrowserToSignIn walks every
// registered write route, asks the production predicate (plainPageFormBackPath)
// which of them answer a browser form as a page, and submits each one signed
// out: a page form added later is covered without a list to update.
func TestEveryNoJSPageFormRouteSendsASignedOutBrowserToSignIn(t *testing.T) {
	t.Parallel()

	ctx := newRefusalPageContext(t, "signed-out-every-page-form@example.com")
	_, iso := noJSDay()

	probe := fiber.New()
	probe.Use(MethodOverride(newBareRefusalHandler(t)))
	probe.All("/*", func(c fiber.Ctx) error {
		_, ok := plainPageFormBackPath(c)
		return c.SendString(strconv.FormatBool(ok))
	})
	formFields := func(method string) url.Values {
		if method == http.MethodPost {
			return url.Values{}
		}
		return url.Values{"_method": {method}}
	}
	admitted := func(method string, target string) bool {
		request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(formFields(method).Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Accept", noJSBrowserAccept)
		response := mustAppResponse(t, probe, request)
		defer func() { _ = response.Body.Close() }()
		return mustReadBodyString(t, response.Body) == "true"
	}

	seen := map[string]bool{}
	for _, route := range ctx.app.GetRoutes(true) {
		switch route.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		default:
			continue
		}
		target := concreteRoutePath(route, iso)
		if !admitted(route.Method, target) {
			continue
		}
		name := route.Method + " " + route.Path
		if seen[name] {
			continue
		}
		seen[name] = true
		t.Run(name, func(t *testing.T) {
			assertSentToSignIn(t, sendSignedOutForm(t, ctx, target, formFields(route.Method), map[string]string{"Accept": noJSBrowserAccept}))
		})
	}
	// The route the report was filed against must be in the walk, or the walk
	// proves nothing about it.
	if !seen[http.MethodPut+" /api/v1/days/:date"] {
		t.Fatalf("the walk never reached PUT /api/v1/days/:date; reached %v", seen)
	}
}

func mustLocaleMessages(t *testing.T, lang string) map[string]string {
	t.Helper()
	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("init i18n manager: %v", err)
	}
	return manager.Messages(lang)
}

// concreteRoutePath fills a route's parameters: the date with a real day, any
// other parameter with an id.
func concreteRoutePath(route fiber.Route, iso string) string {
	segments := strings.Split(route.Path, "/")
	for i, segment := range segments {
		switch {
		case segment == ":date":
			segments[i] = iso
		case strings.HasPrefix(segment, ":"):
			segments[i] = "1"
		}
	}
	return strings.Join(segments, "/")
}
