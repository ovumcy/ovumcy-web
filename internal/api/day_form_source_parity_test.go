package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestNoJSDayFormSourceNamesOnePageForSuccessAndRefusal (WEB-291): the success
// redirect and the refusal's back link read `source` through one function, so a
// request spelling it in another case or with padding lands on the same page
// either way — it used to reach the calendar on success and the dashboard on
// refusal.
func TestNoJSDayFormSourceNamesOnePageForSuccessAndRefusal(t *testing.T) {
	t.Parallel()

	forbidden := englishCopy(t, "common.error.forbidden")
	cases := map[string]struct {
		path   func(iso string) string
		fields url.Values
	}{
		"day delete":  {path: func(iso string) string { return "/api/v1/days/" + iso }, fields: url.Values{"_method": {"DELETE"}}},
		"day save":    {path: func(iso string) string { return "/api/v1/days/" + iso }, fields: url.Values{"_method": {"PUT"}, "is_period": {"true"}}},
		"cycle start": {path: func(iso string) string { return "/api/v1/days/" + iso + "/cycle-start" }, fields: url.Values{}},
	}
	for name, c := range cases {
		for _, refused := range []bool{false, true} {
			label := name + map[bool]string{false: ", accepted", true: ", refused"}[refused]
			t.Run(label, func(t *testing.T) {
				t.Parallel()
				ctx := newRefusalPageContext(t, "source-parity-"+strings.NewReplacer(" ", "-", ",", "").Replace(label)+"@example.com")
				_, iso := noJSDay()
				body := url.Values{}
				for key, values := range c.fields {
					body[key] = values
				}
				body.Set("csrf_token", ctx.csrfToken)
				if refused {
					body.Set("csrf_token", "not-the-token")
				}
				request := httptest.NewRequest(http.MethodPost, c.path(iso)+"?source=%20Calendar%20", strings.NewReader(body.Encode()))
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				request.Header.Set("Accept", noJSBrowserAccept)
				request.Header.Set("Accept-Language", "en")
				request.Header.Set("Cookie", ctx.authCookie+"; "+ctx.csrfCookie.Name+"="+ctx.csrfCookie.Value)
				response := mustAppResponse(t, ctx.app, request)

				if refused {
					assertRefusalPageCarrying(t, response, http.StatusForbidden, forbidden, calendarLanding(iso))
					return
				}
				defer func() { _ = response.Body.Close() }()
				assertStatusCode(t, response, http.StatusSeeOther)
				if location := response.Header.Get("Location"); location != calendarLanding(iso) {
					t.Fatalf("Location %q, want %q", location, calendarLanding(iso))
				}
			})
		}
	}
}
