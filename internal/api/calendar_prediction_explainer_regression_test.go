package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/net/html"
)

func TestCalendarRendersSharedPredictionExplainerKeys(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "calendar-prediction-copy@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	nowUTC := time.Now().UTC()
	today := services.DateAtLocation(nowUTC, time.UTC)

	cycleStarts := []time.Time{
		today.AddDate(0, 0, -84),
		today.AddDate(0, 0, -58),
		today.AddDate(0, 0, -26),
		today.AddDate(0, 0, -5),
	}
	for _, day := range cycleStarts {
		if err := database.Create(&models.DailyLog{
			UserID:     user.ID,
			Date:       day,
			IsPeriod:   true,
			CycleStart: true,
			Flow:       models.FlowMedium,
		}).Error; err != nil {
			t.Fatalf("create cycle start %s: %v", day.Format("2006-01-02"), err)
		}
	}
	factorLogs := map[time.Time][]string{
		cycleStarts[0].AddDate(0, 0, 2): {models.CycleFactorStress},
		cycleStarts[1].AddDate(0, 0, 2): {models.CycleFactorTravel},
		cycleStarts[2].AddDate(0, 0, 2): {models.CycleFactorStress},
	}
	for day, keys := range factorLogs {
		if err := database.Create(&models.DailyLog{
			UserID:          user.ID,
			Date:            day,
			CycleFactorKeys: keys,
		}).Error; err != nil {
			t.Fatalf("create factor log %s: %v", day.Format("2006-01-02"), err)
		}
	}
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"irregular_cycle":   true,
		"cycle_length":      29,
		"period_length":     5,
		"last_period_start": cycleStarts[len(cycleStarts)-1],
	}).Error; err != nil {
		t.Fatalf("update user irregular cycle context: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/calendar?month="+today.Format("2006-01")+"&day="+today.Format("2006-01-02"), nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("calendar request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	explainer := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-calendar-prediction-explainer")
	})
	if explainer == nil {
		t.Fatalf("expected calendar prediction explainer block")
	}
	if got := htmlAttr(explainer, "data-explainer-primary-key"); got != "prediction.explainer.irregular_ranges" {
		t.Fatalf("expected primary explainer key %q, got %q", "prediction.explainer.irregular_ranges", got)
	}
	if got := htmlAttr(explainer, "data-explainer-secondary-key"); got != "prediction.explainer.factor_context" {
		t.Fatalf("expected secondary explainer key %q, got %q", "prediction.explainer.factor_context", got)
	}

	// The key attributes name what the template meant; the paragraphs must read
	// that key's own translation in every locale.
	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("init i18n manager: %v", err)
	}
	languages := manager.SupportedLanguages()
	if len(languages) < 6 {
		t.Fatalf("expected at least the six shipped locales, got %v", languages)
	}
	for _, language := range languages {
		t.Run(language, func(t *testing.T) {
			localized := httptest.NewRequest(http.MethodGet, "/calendar?month="+today.Format("2006-01")+"&day="+today.Format("2006-01-02"), nil)
			localized.Header.Set("Accept-Language", language)
			localized.Header.Set("Cookie", authCookie)
			localizedResponse := mustAppResponse(t, app, localized)
			assertStatusCode(t, localizedResponse, http.StatusOK)
			page := mustParseHTMLDocument(t, mustReadBodyString(t, localizedResponse.Body))
			block := htmlFindElement(page, htmlNodeHasAttr("data-calendar-prediction-explainer"))
			if block == nil {
				t.Fatal("expected calendar prediction explainer block")
			}
			paragraphs := htmlFindElements(block, func(node *html.Node) bool {
				return node.Type == html.ElementNode && node.Data == "p"
			})
			keys := []string{"prediction.explainer.irregular_ranges", "prediction.explainer.factor_context"}
			if len(paragraphs) != len(keys) {
				t.Fatalf("expected %d explainer paragraphs, got %d", len(keys), len(paragraphs))
			}
			messages := manager.Messages(language)
			for index, key := range keys {
				want := normalizeHTMLText(messages[key])
				if want == "" || want == key {
					t.Fatalf("locale %q has no translation for %s", language, key)
				}
				if got := normalizeHTMLText(htmlNodeText(paragraphs[index])); got != want {
					t.Fatalf("[%s] the paragraph behind %s reads %q, want the locale's translation %q", language, key, got, want)
				}
			}
		})
	}
}

func TestCalendarRendersUnpredictableFactsOnlyExplainerKey(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "calendar-unpredictable-copy@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	nowUTC := time.Now().UTC()
	today := services.DateAtLocation(nowUTC, time.UTC)

	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"unpredictable_cycle": true,
		"cycle_length":        28,
		"period_length":       5,
		"last_period_start":   today.AddDate(0, 0, -8),
	}).Error; err != nil {
		t.Fatalf("update user unpredictable cycle context: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/calendar?month="+today.Format("2006-01")+"&day="+today.Format("2006-01-02"), nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("calendar request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	explainer := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-calendar-prediction-explainer")
	})
	if explainer == nil {
		t.Fatalf("expected calendar prediction explainer block")
	}
	if got := htmlAttr(explainer, "data-explainer-primary-key"); got != "prediction.explainer.unpredictable" {
		t.Fatalf("expected primary explainer key %q, got %q", "prediction.explainer.unpredictable", got)
	}
}
