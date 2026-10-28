package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"golang.org/x/net/html"
)

// The clear-data pre-check reads its refusal copy off the form. An exhausted
// re-auth budget (429) must read the same words as the delete-account path —
// common.error.too_many_requests — in every locale, not the generic
// "request failed" copy.
func TestSettingsClearDataFormCarriesTheTooManyRequestsCopyInEveryLocale(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "settings-clear-data-too-many@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("init i18n: %v", err)
	}
	for _, language := range manager.SupportedLanguages() {
		t.Run(language, func(t *testing.T) {
			want := manager.Messages(language)["common.error.too_many_requests"]
			if want == "" {
				t.Fatalf("locale %s has no common.error.too_many_requests", language)
			}

			request := httptest.NewRequest(http.MethodGet, "/settings", nil)
			request.Header.Set("Accept-Language", language)
			request.Header.Set("Cookie", authCookie)
			response := mustAppResponse(t, app, request)
			assertStatusCode(t, response, http.StatusOK)
			document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))

			forms := htmlFindElements(document, func(node *html.Node) bool {
				return node.Data == "form" && htmlHasAttr(node, "data-clear-data-verify-form")
			})
			if len(forms) != 1 {
				t.Fatalf("expected one clear-data verify form, got %d", len(forms))
			}
			if got := htmlAttr(forms[0], "data-clear-data-too-many"); got != want {
				t.Fatalf("data-clear-data-too-many = %q, want %q", got, want)
			}
		})
	}
}
