package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// TestOnboardingDatePickerLocalizesAccessibilityLabels pins the server-rendered
// i18n + a11y contract for the onboarding month picker across every supported
// locale: the shortcut buttons carry localized text and the month-navigation
// buttons carry localized accessible names. It also pins the single-mechanism
// contract — one transport input named last_period_start, and no second
// segmented date field beside it. Assertions locate nodes by stable id / data
// attribute (not markup order), so a legitimate reorder does not false-fail.
// The picking behavior itself (clicking a day fills the value) is browser-side
// and covered by the Playwright onboarding spec, not here.
func TestOnboardingDatePickerLocalizesAccessibilityLabels(t *testing.T) {
	cases := []struct {
		lang          string
		today         string
		yesterday     string
		previousMonth string
		nextMonth     string
	}{
		{"en", "Today", "Yesterday", "Previous month", "Next month"},
		{"ru", "Сегодня", "Вчера", "Предыдущий месяц", "Следующий месяц"},
		{"es", "Hoy", "Ayer", "Mes anterior", "Mes siguiente"},
		{"fr", "Aujourd'hui", "Hier", "Mois précédent", "Mois suivant"},
		{"de", "Heute", "Gestern", "Vorheriger Monat", "Nächster Monat"},
		{"it", "Oggi", "Ieri", "Mese precedente", "Mese successivo"},
	}

	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			document := renderOnboardingForLanguage(t, tc.lang)

			transport := htmlElementByID(document, "last-period-start")
			if transport == nil {
				t.Fatalf("onboarding transport date input #last-period-start not found")
			}
			if got := htmlAttr(transport, "name"); got != "last_period_start" {
				t.Fatalf("transport input name = %q, want %q", got, "last_period_start")
			}
			if htmlAttr(transport, "min") == "" || htmlAttr(transport, "max") == "" {
				t.Fatalf("transport input must publish the allowed range as min/max")
			}

			// One value, one mechanism: the segmented day/month/year field that
			// used to duplicate this input is gone from the onboarding page.
			segments := htmlFindElements(document, func(node *html.Node) bool {
				return node.Type == html.ElementNode && htmlHasAttr(node, "data-date-field-part")
			})
			if len(segments) != 0 {
				t.Fatalf("onboarding renders %d segmented date inputs beside the picker, want 0", len(segments))
			}

			picker := htmlFindElement(document, func(node *html.Node) bool {
				return node.Type == html.ElementNode && htmlHasAttr(node, "data-onboarding-picker")
			})
			if picker == nil {
				t.Fatalf("onboarding picker [data-onboarding-picker] not found")
			}

			assertShortcutLabel(t, picker, "today", tc.today)
			assertShortcutLabel(t, picker, "yesterday", tc.yesterday)
			assertMonthNavLabel(t, picker, "data-onboarding-month-prev", tc.previousMonth)
			assertMonthNavLabel(t, picker, "data-onboarding-month-next", tc.nextMonth)
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

func assertShortcutLabel(t *testing.T, picker *html.Node, shortcut string, want string) {
	t.Helper()

	button := htmlFindElement(picker, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlAttr(node, "data-onboarding-shortcut") == shortcut
	})
	if button == nil {
		t.Fatalf("onboarding shortcut %q not found", shortcut)
	}
	if got := strings.TrimSpace(htmlNodeText(button)); got != want {
		t.Fatalf("onboarding shortcut %q text = %q, want %q", shortcut, got, want)
	}
}

func assertMonthNavLabel(t *testing.T, picker *html.Node, attr string, want string) {
	t.Helper()

	button := htmlFindElement(picker, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, attr)
	})
	if button == nil {
		t.Fatalf("onboarding month navigation [%s] not found", attr)
	}
	if got := htmlAttr(button, "aria-label"); got != want {
		t.Fatalf("onboarding [%s] aria-label = %q, want %q", attr, got, want)
	}
}
