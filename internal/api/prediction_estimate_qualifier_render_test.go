package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/net/html"
)

// TestPredictionSurfacesRenderTheirQualifiedKeys pins WEB-390 at the render
// layer: on a history steady enough that no Approximate caption renders, the
// calendar's fertile-window legend entry, the dashboard's fertility badge and
// its next-period days still render from the catalogue keys that
// TestPredictionSurfacesCarryAnEstimateQualifier proves qualified in every locale.
func TestPredictionSurfacesRenderTheirQualifiedKeys(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "estimate-qualifier@example.com", "StrongPass1", true)
	periodStart := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -4)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": periodStart,
	}).Error; err != nil {
		t.Fatalf("update user cycle settings: %v", err)
	}
	for _, cycleStart := range []time.Time{periodStart.AddDate(0, 0, -84), periodStart.AddDate(0, 0, -56), periodStart.AddDate(0, 0, -28), periodStart} {
		for offset := range 5 {
			entry := models.DailyLog{
				UserID:   user.ID,
				Date:     cycleStart.AddDate(0, 0, offset),
				IsPeriod: true,
				Flow:     models.FlowMedium,
			}
			if cycleStart.Equal(periodStart) && offset == 4 {
				// Today's egg-white observation is what raises the fertility badge.
				entry.CervicalMucus = models.CervicalMucusEggWhite
			}
			if err := database.Create(&entry).Error; err != nil {
				t.Fatalf("create period log: %v", err)
			}
		}
	}
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	calendar := mustParseHTMLDocument(t, renderCalendarMonthHTML(t, app, authCookie, periodStart.AddDate(0, 0, 13).Format("2006-01")))
	legend := htmlFindElement(calendar, htmlNodeHasAttr("data-calendar-legend"))
	if legend == nil {
		t.Fatal("expected the calendar legend")
	}
	if fertile := htmlFindElement(legend, htmlNodeHasAttr("data-legend-key")); fertile == nil || htmlAttr(fertile, "data-legend-key") != "calendar.legend.fertility" {
		t.Fatal("expected the fertile-window legend entry rendered from calendar.legend.fertility")
	}

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	dashboard := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	nextPeriod := htmlFindElement(dashboard, htmlNodeHasAttr("data-dashboard-next-period"))
	if nextPeriod == nil {
		t.Fatal("expected the next-period estimate on the dashboard")
	}
	if dates := htmlFindElement(nextPeriod, htmlNodeHasAttr("data-next-period-key")); dates == nil || htmlAttr(dates, "data-next-period-key") != "dashboard.next_period_days" {
		t.Fatal("expected the next-period days rendered from dashboard.next_period_days")
	}
	if badge := htmlFindElement(dashboard, htmlNodeHasAttr("data-fertility-badge")); badge == nil || htmlAttr(badge, "data-fertility-badge-key") != "dashboard.high_fertility_badge" {
		t.Fatal("expected the fertility badge rendered from dashboard.high_fertility_badge")
	}
	if htmlFindElement(dashboard, htmlNodeHasAttr("data-dashboard-estimate-qualifier")) != nil {
		t.Fatal("expected a steady history to render no Approximate caption, so the inline qualifier is the only one")
	}
}

// TestPredictionSurfaceTextIsTheTranslationOfItsKeyInEveryLocale closes what the
// key-attribute checks above leave open: the data-*-key attribute names the
// catalogue entry the template meant, but the text the owner reads is whatever
// the template printed. For each supported locale the node behind the key must
// carry that locale's own translation of the key, so a node printing a raw key,
// another key's text or the wrong locale fails here while its attribute still
// reads correctly.
func TestPredictionSurfaceTextIsTheTranslationOfItsKeyInEveryLocale(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "estimate-qualifier-locales@example.com", "StrongPass1", true)
	periodStart := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -4)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": periodStart,
	}).Error; err != nil {
		t.Fatalf("update user cycle settings: %v", err)
	}
	for _, cycleStart := range []time.Time{periodStart.AddDate(0, 0, -84), periodStart.AddDate(0, 0, -56), periodStart.AddDate(0, 0, -28), periodStart} {
		for offset := range 5 {
			entry := models.DailyLog{UserID: user.ID, Date: cycleStart.AddDate(0, 0, offset), IsPeriod: true, Flow: models.FlowMedium}
			if cycleStart.Equal(periodStart) && offset == 4 {
				entry.CervicalMucus = models.CervicalMucusEggWhite
			}
			if err := database.Create(&entry).Error; err != nil {
				t.Fatalf("create period log: %v", err)
			}
		}
	}
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("init i18n manager: %v", err)
	}
	languages := manager.SupportedLanguages()
	if len(languages) < 6 {
		t.Fatalf("expected at least the six shipped locales, got %v", languages)
	}

	get := func(t *testing.T, path, language string) *html.Node {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Header.Set("Accept-Language", language)
		request.Header.Set("Cookie", authCookie)
		response := mustAppResponse(t, app, request)
		assertStatusCode(t, response, http.StatusOK)
		return mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	}
	assertText := func(t *testing.T, node *html.Node, messages map[string]string, language, key string) {
		t.Helper()
		want := normalizeHTMLText(messages[key])
		if want == "" || want == key {
			t.Fatalf("locale %q has no translation for %s", language, key)
		}
		if got := normalizeHTMLText(htmlNodeText(node)); got != want {
			t.Fatalf("[%s] the node behind %s reads %q, want the locale's translation %q", language, key, got, want)
		}
	}

	for _, language := range languages {
		t.Run(language, func(t *testing.T) {
			messages := manager.Messages(language)

			calendar := get(t, "/calendar?month="+periodStart.AddDate(0, 0, 13).Format("2006-01"), language)
			legend := htmlFindElement(calendar, htmlNodeHasAttr("data-calendar-legend"))
			if legend == nil {
				t.Fatal("expected the calendar legend")
			}
			fertile := htmlFindElement(legend, htmlNodeHasAttr("data-legend-key"))
			if fertile == nil || htmlAttr(fertile, "data-legend-key") != "calendar.legend.fertility" {
				t.Fatal("expected the fertile-window legend entry")
			}
			assertText(t, fertile, messages, language, "calendar.legend.fertility")

			dashboard := get(t, "/dashboard", language)
			badge := htmlFindElement(dashboard, htmlNodeHasAttr("data-fertility-badge"))
			if badge == nil || htmlAttr(badge, "data-fertility-badge-key") != "dashboard.high_fertility_badge" {
				t.Fatal("expected the fertility badge")
			}
			assertText(t, badge, messages, language, "dashboard.high_fertility_badge")
		})
	}
}
