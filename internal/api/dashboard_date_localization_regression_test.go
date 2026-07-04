package api

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/net/html"
)

func TestDashboardStableHeroRendersEnglishPredictionWindowWithoutFallbackStatus(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "dashboard-date-localization@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	today := services.DateAtLocation(time.Now().UTC(), time.UTC)
	lastPeriodStart := today.AddDate(0, 0, -8)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": lastPeriodStart,
	}).Error; err != nil {
		t.Fatalf("update user cycle context: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("dashboard request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	hero := dashboardElementByDataAttr(document, "data-dashboard-cycle-hero")
	if hero == nil {
		t.Fatal("expected stable dashboard cycle hero")
	}
	footerText := dashboardElementTextByDataAttr(t, hero, "data-dashboard-cycle-hero-next-period")
	exactWindowPattern := regexp.MustCompile(`[A-Z][a-z]{2,8} \d{1,2}, \d{4}\s*—\s*[A-Z][a-z]{2,8} \d{1,2}, \d{4}`)
	if !exactWindowPattern.MatchString(footerText) {
		t.Fatalf("expected stable dashboard hero footer to render an English-localized next period window, got %q", footerText)
	}
	if dashboardElementByDataAttr(document, "data-dashboard-status-line") != nil {
		t.Fatalf("did not expect duplicated dashboard status line when hero is visible")
	}
	if htmlAttr(hero, "data-cycle-hero-approximate") == "true" {
		t.Fatalf("did not expect exact stable cycle hero to be marked approximate")
	}
}

func TestDashboardEnglishRendersOvulationRangeForIrregularMode(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "dashboard-irregular-ovulation@example.com", "StrongPass1", true)
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
		if err := database.Create(&models.DailyLog{UserID: user.ID, Date: day, IsPeriod: true, Flow: models.FlowMedium}).Error; err != nil {
			t.Fatalf("create irregular cycle start %s: %v", day.Format("2006-01-02"), err)
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

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("dashboard request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	hero := dashboardElementByDataAttr(document, "data-dashboard-cycle-hero")
	if hero == nil {
		t.Fatal("expected irregular range state to keep the dashboard cycle hero")
	}
	footerText := dashboardElementTextByDataAttr(t, hero, "data-dashboard-cycle-hero-next-period")
	if !regexp.MustCompile(`[A-Z][a-z]{2,8} \d{1,2}, \d{4}\s*—\s*[A-Z][a-z]{2,8} \d{1,2}, \d{4}`).MatchString(footerText) {
		t.Fatalf("expected English-localized next period range in dashboard hero footer, got %q", footerText)
	}
	if dashboardElementByDataAttr(document, "data-dashboard-status-line") != nil {
		t.Fatalf("did not expect duplicated dashboard status line when hero is visible")
	}
	if htmlAttr(hero, "data-cycle-hero-approximate") != "true" {
		t.Fatalf("expected irregular range dashboard hero to be marked approximate")
	}
	explainer := dashboardElementByDataAttr(document, "data-dashboard-prediction-explainer")
	if explainer == nil {
		t.Fatal("expected dashboard prediction explainer block in irregular range state")
	}
	if got := htmlAttr(explainer, "data-explainer-key"); got != "prediction.explainer.irregular_ranges" {
		t.Fatalf("expected dashboard explainer key %q, got %q", "prediction.explainer.irregular_ranges", got)
	}
}

func TestDashboardEnglishRendersSharedSparsePredictionExplanationForIrregularMode(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "dashboard-irregular-sparse@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	nowUTC := time.Now().UTC()
	today := services.DateAtLocation(nowUTC, time.UTC)
	cycleStarts := []time.Time{
		today.AddDate(0, 0, -84),
		today.AddDate(0, 0, -56),
		today.AddDate(0, 0, -28),
	}
	for _, day := range cycleStarts {
		if err := database.Create(&models.DailyLog{UserID: user.ID, Date: day, IsPeriod: true, Flow: models.FlowMedium}).Error; err != nil {
			t.Fatalf("create sparse irregular cycle start %s: %v", day.Format("2006-01-02"), err)
		}
	}
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"irregular_cycle":   true,
		"cycle_length":      29,
		"period_length":     5,
		"last_period_start": cycleStarts[len(cycleStarts)-1],
	}).Error; err != nil {
		t.Fatalf("update sparse irregular cycle context: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("dashboard request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	if dashboardElementByDataAttr(document, "data-dashboard-cycle-hero") != nil {
		t.Fatal("did not expect dashboard cycle hero before sparse irregular history becomes reliable")
	}
	explainer := dashboardElementByDataAttr(document, "data-dashboard-prediction-explainer")
	if explainer == nil {
		t.Fatal("expected dashboard prediction explainer block in sparse irregular state")
	}
	if got := htmlAttr(explainer, "data-explainer-key"); got != "prediction.explainer.irregular_sparse" {
		t.Fatalf("expected dashboard explainer key %q, got %q", "prediction.explainer.irregular_sparse", got)
	}
}

func dashboardElementByDataAttr(root *html.Node, attr string) *html.Node {
	if root == nil {
		return nil
	}
	return htmlFindElement(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, attr)
	})
}

func dashboardElementTextByDataAttr(t *testing.T, root *html.Node, attr string) string {
	t.Helper()

	node := dashboardElementByDataAttr(root, attr)
	if node == nil {
		t.Fatalf("expected dashboard element with %s", attr)
	}
	return normalizeHTMLText(htmlNodeText(node))
}
