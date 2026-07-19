package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/net/html"
)

// TestOnboardingDateFieldLocalizesAccessibilityLabels pins the server-rendered
// i18n + a11y contract for the onboarding segmented date field across every
// supported locale: the hidden field carries the request language, each segment
// input exposes the localized aria-label, and the onboarding flow container
// exposes the localized quick-pick labels. Assertions locate nodes by stable id
// / data attribute (not markup order), so a legitimate reorder of the segments
// does not false-fail. The actual quick-pick click behavior (clicking
// "Yesterday" fills the date) is browser-side and covered by the Playwright
// onboarding spec, not here.
func TestOnboardingDateFieldLocalizesAccessibilityLabels(t *testing.T) {
	cases := []struct {
		lang       string
		day        string
		month      string
		year       string
		today      string
		yesterday  string
		twoDaysAgo string
	}{
		{"en", "Day", "Month", "Year", "Today", "Yesterday", "2 days ago"},
		{"ru", "День", "Месяц", "Год", "Сегодня", "Вчера", "2 дня назад"},
		{"es", "Día", "Mes", "Año", "Hoy", "Ayer", "Hace 2 días"},
		{"fr", "Jour", "Mois", "Année", "Aujourd'hui", "Hier", "Il y a 2 jours"},
		{"de", "Tag", "Monat", "Jahr", "Heute", "Gestern", "Vor 2 Tagen"},
		{"it", "Giorno", "Mese", "Anno", "Oggi", "Ieri", "2 giorni fa"},
	}

	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			document := renderOnboardingForLanguage(t, tc.lang)

			hidden := htmlElementByID(document, "last-period-start")
			if hidden == nil {
				t.Fatalf("onboarding hidden date input #last-period-start not found")
			}
			if got := htmlAttr(hidden, "lang"); got != tc.lang {
				t.Fatalf("hidden date input lang = %q, want %q", got, tc.lang)
			}

			assertDateSegmentAriaLabel(t, document, "last-period-start-day", tc.day)
			assertDateSegmentAriaLabel(t, document, "last-period-start-month", tc.month)
			assertDateSegmentAriaLabel(t, document, "last-period-start-year", tc.year)

			flow := htmlFindElement(document, func(node *html.Node) bool {
				return node.Type == html.ElementNode && htmlHasAttr(node, "data-onboarding-flow")
			})
			if flow == nil {
				t.Fatalf("onboarding flow container [data-onboarding-flow] not found")
			}
			assertQuickPickLabel(t, flow, "data-today-label", tc.today)
			assertQuickPickLabel(t, flow, "data-yesterday-label", tc.yesterday)
			assertQuickPickLabel(t, flow, "data-two-days-ago-label", tc.twoDaysAgo)
		})
	}
}

func renderOnboardingForLanguage(t *testing.T, lang string) *html.Node {
	t.Helper()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "onboarding-lang-"+lang+"@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/onboarding", nil)
	request.Header.Set("Cookie", authCookie+"; ovumcy_lang="+lang)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("onboarding request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected onboarding status 200 for %s, got %d", lang, response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read onboarding body: %v", err)
	}
	return mustParseHTMLDocument(t, string(body))
}

func assertDateSegmentAriaLabel(t *testing.T, document *html.Node, segmentID string, want string) {
	t.Helper()

	segment := htmlElementByID(document, segmentID)
	if segment == nil {
		t.Fatalf("onboarding date segment #%s not found", segmentID)
	}
	if got := htmlAttr(segment, "aria-label"); got != want {
		t.Fatalf("segment #%s aria-label = %q, want %q", segmentID, got, want)
	}
}

func assertQuickPickLabel(t *testing.T, flow *html.Node, attr string, want string) {
	t.Helper()

	if got := htmlAttr(flow, attr); got != want {
		t.Fatalf("onboarding %s = %q, want %q", attr, got, want)
	}
}
