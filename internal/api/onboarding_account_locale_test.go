package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/net/html"
)

// TestOnboardingCompleteStoresTheRenderedLanguageAndItsWeekStart pins WEB-216
// and WEB-257 at the route: an account that finishes onboarding on German pages
// keeps German as its language (the .ics and webhook egress read that column)
// and starts its weeks on Monday; a language the owner already chose is kept.
func TestOnboardingCompleteStoresTheRenderedLanguageAndItsWeekStart(t *testing.T) {
	cases := []struct {
		name           string
		stored         string
		cookie         string
		acceptLanguage string
		wantLanguage   string
		wantWeekStart  string
	}{
		{name: "nothing stored", stored: "", cookie: "de", wantLanguage: "de", wantWeekStart: models.WeekStartMonday},
		{name: "chosen language kept", stored: "en", cookie: "de", wantLanguage: "en", wantWeekStart: models.WeekStartSunday},
		{name: "negotiated language stored", stored: "", acceptLanguage: "fr-CA,fr;q=0.9", wantLanguage: "fr", wantWeekStart: models.WeekStartMonday},
		// The page fell back to DEFAULT_LANGUAGE: no language was picked, so none is pinned.
		{name: "default fallback not stored", stored: "", acceptLanguage: "ja", wantLanguage: "", wantWeekStart: models.DefaultWeekStart},
		{name: "unsupported cookie not stored", stored: "", cookie: "xx", acceptLanguage: "de", wantLanguage: "", wantWeekStart: models.DefaultWeekStart},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, database := newOnboardingTestApp(t)
			email := "onboarding-locale-" + string(rune('a'+index)) + "@example.com"
			user := createOnboardingTestUser(t, database, email, "StrongPass1", false)
			authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

			start := time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC)
			if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
				"last_period_start":  start,
				"interface_language": tc.stored,
			}).Error; err != nil {
				t.Fatalf("seed onboarding state: %v", err)
			}

			req := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/complete", strings.NewReader(""))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("HX-Request", "true")
			cookies := authCookie
			if tc.cookie != "" {
				cookies += "; " + languageCookieName + "=" + tc.cookie
			}
			req.Header.Set("Cookie", cookies)
			req.Header.Set("Accept-Language", tc.acceptLanguage)
			bindDayWriteForTest(t, req, user.ID)
			resp := mustAppResponse(t, app, req)
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("complete status = %d, want 200 (body %q)", resp.StatusCode, mustReadBodyString(t, resp.Body))
			}

			var reloaded models.User
			if err := database.First(&reloaded, user.ID).Error; err != nil {
				t.Fatalf("reload user: %v", err)
			}
			if reloaded.InterfaceLanguage != tc.wantLanguage {
				t.Fatalf("expected interface_language %q, got %q", tc.wantLanguage, reloaded.InterfaceLanguage)
			}
			if reloaded.WeekStartsOn != tc.wantWeekStart {
				t.Fatalf("expected week_starts_on %q, got %q", tc.wantWeekStart, reloaded.WeekStartsOn)
			}
		})
	}
}

// TestSettingsInterfaceFormMarksALanguageTheAccountHasNotStored pins the
// server half of WEB-216's Save fix: the interface form says when the account
// has no language of its own, which is what keeps its Save enabled.
func TestSettingsInterfaceFormMarksALanguageTheAccountHasNotStored(t *testing.T) {
	for _, stored := range []string{"", "en"} {
		t.Run("stored="+stored, func(t *testing.T) {
			ctx := newSettingsSecurityTestContext(t, "settings-interface-unsaved-"+stored+"@example.com")
			if err := ctx.database.Model(&models.User{}).Where("id = ?", ctx.user.ID).Update("interface_language", stored).Error; err != nil {
				t.Fatalf("seed interface_language: %v", err)
			}

			request := httptest.NewRequest(http.MethodGet, "/settings", nil)
			request.Header.Set("Accept-Language", "en")
			request.Header.Set("Cookie", ctx.authCookie)
			response, err := ctx.app.Test(request, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("settings request failed: %v", err)
			}
			defer func() { _ = response.Body.Close() }()

			document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
			form := htmlFindElement(document, func(node *html.Node) bool {
				return node.Type == html.ElementNode && htmlHasAttr(node, "data-settings-interface-form")
			})
			if form == nil {
				t.Fatal("expected interface settings form")
			}
			hint := htmlFindElement(form, func(node *html.Node) bool {
				return node.Type == html.ElementNode && htmlHasAttr(node, "data-settings-interface-language-unsaved-hint")
			})
			wantUnsaved := stored == ""
			if got := htmlHasAttr(form, "data-settings-interface-language-unsaved"); got != wantUnsaved {
				t.Fatalf("expected the unsaved-language marker = %v, got %v", wantUnsaved, got)
			}
			if (hint != nil) != wantUnsaved {
				t.Fatalf("expected the unsaved-language hint = %v, got %v", wantUnsaved, hint != nil)
			}
		})
	}
}
