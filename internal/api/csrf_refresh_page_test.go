package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// TestPrivacyPageIsASideEffectFreeCSRFTokenSource pins what the page script
// relies on when it re-reads its CSRF token after a refusal: it fetches /privacy
// rather than the page the owner is on, because the current page may be a
// shown-once surface whose GET mints or spends a one-time value. The page must
// therefore (a) answer 200 with a non-empty csrf-token meta to a signed-in and a
// signed-out visitor, (b) set no cookie other than the CSRF one, and (c) not be
// a shown-once route: it carries neither the HEAD refusal nor a place in the
// declared shown-once set.
func TestPrivacyPageIsASideEffectFreeCSRFTokenSource(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "privacy-refresh@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")

	for name, cookie := range map[string]string{"signed out": "", "signed in": authCookie} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/privacy", nil)
			request.Header.Set("Accept-Language", "en")
			if cookie != "" {
				request.Header.Set("Cookie", cookie)
			}
			response := mustAppResponse(t, app, request)
			if response.StatusCode != http.StatusOK {
				t.Fatalf("GET /privacy status = %d, want 200", response.StatusCode)
			}
			body := mustReadBodyString(t, response.Body)
			if token := extractCSRFTokenFromHTML(t, body); strings.TrimSpace(token) == "" {
				t.Fatal("GET /privacy carries an empty csrf-token meta")
			}
			for _, set := range response.Cookies() {
				if set.Name != "ovumcy_csrf" {
					t.Errorf("GET /privacy sets cookie %q: only the CSRF cookie may be written by the page the token refresh reads", set.Name)
				}
			}
		})
	}

	t.Run("not a shown-once surface", func(t *testing.T) {
		refusal := handlerFuncName((*Handler)(nil).refuseHEADOnShownOnceSurface)
		page := handlerFuncName((*Handler)(nil).ShowPrivacyPage)
		if refusal == "" || page == "" {
			t.Fatalf("resolved handler names are empty (refusal %q, page %q): the chain check below would be vacuous", refusal, page)
		}

		matched := 0
		for _, route := range app.GetRoutes(true) {
			if route.Method != fiber.MethodGet || route.Path != "/privacy" {
				continue
			}
			isPage := false
			for _, routeHandler := range route.Handlers {
				switch handlerFuncName(routeHandler) {
				case refusal:
					t.Errorf("GET /privacy carries %s: the token refresh would then read a shown-once surface", refusal)
				case page:
					isPage = true
				}
			}
			if isPage {
				matched++
			}
		}
		if matched != 1 {
			t.Fatalf("GET /privacy is routed to ShowPrivacyPage %d times, want exactly 1", matched)
		}

		for _, route := range append(append([]string{}, shownOnceGETRoutes...), firstPartyGuardedRoutes...) {
			if route == fiber.MethodGet+" /privacy" {
				t.Errorf("%q is declared as a shown-once or first-party-guarded route", route)
			}
		}
	})
}
