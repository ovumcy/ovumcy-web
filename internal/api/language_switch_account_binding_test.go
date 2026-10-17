package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// languageSwitchFromPage is the switcher's no-JavaScript form post as a browser
// sends it: urlencoded, accepting HTML, the binding (when the page rendered one)
// in the body. binding "" sends no field.
func languageSwitchFromPage(cookie, binding string) *http.Request {
	form := url.Values{"lang": {"ru"}, "next": {"/onboarding"}}
	if binding != "" {
		form.Set(dayFormAccountWireName, binding)
	}
	request := httptest.NewRequest(http.MethodPost, LanguageSwitchPath, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "text/html")
	request.Header.Set("Accept-Language", "en")
	if cookie != "" {
		request.Header.Set("Cookie", cookie)
	}
	return request
}

// TestLanguageSwitchStoresOnAnAccountOnlyForThePageRenderedForIt drives the
// stale-tab hazard on the public switcher: a page rendered for the first
// account (or for nobody) and used after the second account signed in, in
// another tab, switches the cookie and leaves the second account's stored
// language alone. Positive control: the second account's own page stores it.
// Every case is answered the same way, so the answer says nothing about which
// of them it was.
func TestLanguageSwitchStoresOnAnAccountOnlyForThePageRenderedForIt(t *testing.T) {
	t.Parallel()

	fixture := newOnboardingAccountFixture(t, "lang-switch-binding")

	for _, testCase := range []struct {
		name        string
		cookie      string
		binding     func() string
		extraHeader string
		wantStored  string
	}{
		{name: "page of another account", cookie: fixture.secondCookie, binding: func() string { return fixture.firstBinding }, wantStored: "de"},
		{name: "page with no binding (signed-out page, or a client that sends none)", cookie: fixture.secondCookie, binding: func() string { return "" }, wantStored: "de"},
		{name: "a binding that names no account", cookie: fixture.secondCookie, binding: func() string { return "not-a-binding" }, wantStored: "de"},
		{name: "own binding in the form beside a stale one in the header", cookie: fixture.secondCookie, binding: func() string { return fixture.secondBinding }, extraHeader: "stale", wantStored: "de"},
		{name: "the account's own page", cookie: fixture.secondCookie, binding: func() string { return fixture.secondBinding }, wantStored: "ru"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			seedStoredInterfaceLanguage(t, fixture.database, fixture.second.ID, "de")
			seedStoredInterfaceLanguage(t, fixture.database, fixture.first.ID, "de")

			request := languageSwitchFromPage(testCase.cookie, testCase.binding())
			if testCase.extraHeader == "stale" {
				request.Header.Set(dayFormAccountWireHeader, fixture.firstBinding)
			}
			response := mustAppResponse(t, fixture.app, request)

			assertStatusCode(t, response, http.StatusSeeOther)
			if location := response.Header.Get("Location"); location != "/onboarding" {
				t.Fatalf("expected the switch to redirect to /onboarding, got %q", location)
			}
			if got := responseCookieValue(response.Cookies(), languageCookieName); got != "ru" {
				t.Fatalf("expected ovumcy_lang=ru on every switch, got %q", got)
			}
			if body := mustReadBodyString(t, response.Body); strings.TrimSpace(body) != "" {
				t.Fatalf("expected an empty body on every switch, got %q", body)
			}
			if got := storedInterfaceLanguage(t, fixture.database, fixture.second.ID); got != testCase.wantStored {
				t.Fatalf("expected the signed-in account to hold %q after the switch, got %q", testCase.wantStored, got)
			}
			if got := storedInterfaceLanguage(t, fixture.database, fixture.first.ID); got != "de" {
				t.Fatalf("expected the other account to be untouched, got %q", got)
			}
		})
	}
}

// TestLanguageSwitchFormCarriesTheRenderingAccountsBinding pins the half the
// matrix above takes for granted: the no-JavaScript form posts the binding a
// signed-in page was rendered with, and a signed-out page renders none (there is
// no account for it to name).
func TestLanguageSwitchFormCarriesTheRenderingAccountsBinding(t *testing.T) {
	t.Parallel()

	fixture := newOnboardingAccountFixture(t, "lang-switch-markup")

	switchForm := func(t *testing.T, path, cookie string) *html.Node {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		if cookie != "" {
			request.Header.Set("Cookie", cookie)
		}
		response := mustAppResponse(t, fixture.app, request)
		assertStatusCode(t, response, http.StatusOK)
		document, err := html.Parse(strings.NewReader(mustReadBodyString(t, response.Body)))
		if err != nil {
			t.Fatalf("%s: parse page: %v", path, err)
		}
		var form *html.Node
		walkElementsWithAttribute(document, "data-language-switch-form", func(element *html.Node) { form = element })
		if form == nil {
			t.Fatalf("%s: expected the language switch form", path)
		}
		return form
	}

	t.Run("signed-in onboarding page", func(t *testing.T) {
		for _, account := range []struct{ cookie, binding string }{
			{fixture.firstCookie, fixture.firstBinding},
			{fixture.secondCookie, fixture.secondBinding},
		} {
			got, found := hiddenFieldValue(switchForm(t, "/onboarding", account.cookie), dayFormAccountWireName)
			if !found || got != account.binding || got == "" {
				t.Fatalf("expected the switcher to carry the rendering account's binding %q, got %q (found %t)", account.binding, got, found)
			}
		}
	})

	t.Run("signed-out page", func(t *testing.T) {
		if got, found := hiddenFieldValue(switchForm(t, "/login", ""), dayFormAccountWireName); found {
			t.Fatalf("expected the signed-out switcher to carry no binding, got %q", got)
		}
	})
}
