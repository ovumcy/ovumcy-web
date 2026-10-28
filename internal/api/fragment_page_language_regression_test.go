package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// An htmx swap renders its fragment in the request's language, which the
// language cookie decides — and a switch in a second tab rewrites that cookie
// under a page already rendered in the old language. The page names its own
// language in the X-Ovumcy-Page-Lang header; when the two disagree the
// response carries HX-Refresh, so htmx reloads the whole page in one language
// instead of swapping a German fragment into a French page (WEB-232).
func TestHTMXRequestFromAPageInAnotherLanguageAsksForAFullRefresh(t *testing.T) {
	cases := []struct {
		name        string
		htmx        bool
		cookie      string
		pageLang    string
		wantRefresh bool
	}{
		{name: "cookie switched in another tab", htmx: true, cookie: "de", pageLang: "fr", wantRefresh: true},
		{name: "page and cookie agree", htmx: true, cookie: "fr", pageLang: "fr"},
		{name: "page language spelled with a region and case", htmx: true, cookie: "fr", pageLang: "FR-fr"},
		{name: "no page language header", htmx: true, cookie: "de"},
		{name: "unsupported page language", htmx: true, cookie: "de", pageLang: "xx"},
		{name: "script fetch that is not htmx", cookie: "de", pageLang: "fr", wantRefresh: true},
		{name: "plain navigation names no page language", cookie: "de"},
		{name: "no cookie, page differs from Accept-Language", htmx: true, pageLang: "fr", wantRefresh: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			app, _ := newOnboardingTestApp(t)

			request := httptest.NewRequest(http.MethodGet, "/login", nil)
			request.Header.Set("Accept-Language", "en")
			if testCase.htmx {
				request.Header.Set("HX-Request", "true")
			}
			if testCase.cookie != "" {
				request.Header.Set("Cookie", languageCookieName+"="+testCase.cookie)
			}
			if testCase.pageLang != "" {
				request.Header.Set("X-Ovumcy-Page-Lang", testCase.pageLang)
			}

			response, err := app.Test(request, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer func() { _ = response.Body.Close() }()

			gotRefresh := response.Header.Get("HX-Refresh") == "true"
			if gotRefresh != testCase.wantRefresh {
				t.Fatalf("HX-Refresh = %q, want refresh %v", response.Header.Get("HX-Refresh"), testCase.wantRefresh)
			}
		})
	}
}

// The refresh must not cost the owner the write that carried it: the save is
// applied as usual and only the swap is replaced by a reload.
func TestHTMXDaySaveFromAPageInAnotherLanguageStillSavesAndRefreshes(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "fragment-page-language@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	today := time.Now().UTC().Format("2006-01-02")
	request := dashboardSaveRequest(today, url.Values{"notes": {"saved across a language switch"}}, authCookie+"; "+languageCookieName+"=de")
	request.Header.Set("X-Ovumcy-Page-Lang", "fr")
	bindDayWriteForTest(t, request, user.ID)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("save request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected save status 200, got %d", response.StatusCode)
	}
	if got := response.Header.Get("HX-Refresh"); got != "true" {
		t.Fatalf("expected HX-Refresh true on a cross-language swap, got %q", got)
	}

	var saved models.DailyLog
	if err := database.Where("user_id = ?", user.ID).First(&saved).Error; err != nil {
		t.Fatalf("expected the day to be saved: %v", err)
	}
	if !strings.Contains(saved.Notes, "saved across a language switch") {
		t.Fatalf("expected saved notes, got %q", saved.Notes)
	}
}
