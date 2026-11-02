package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/net/html"
	"gorm.io/gorm"
)

func TestDashboardStableHeaderRendersEnglishPredictionWindow(t *testing.T) {
	// One captured instant is the handler's clock and the fixture's today, so a
	// UTC midnight between two wall-clock reads cannot move one of them.
	now := time.Now().UTC()
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{now: func() time.Time { return now }})
	user := createOnboardingTestUser(t, database, "dashboard-date-localization@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	today := services.DateAtLocation(now, time.UTC)
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
	header := dashboardElementByDataAttr(document, "data-dashboard-status-header")
	if header == nil {
		t.Fatal("expected the dashboard status header")
	}
	assertRenderedWindowIsTheContextRange(t, database, user.ID, now, header, "dashboard.next_period_days")
	if htmlAttr(header, "data-cycle-hero-approximate") == "true" {
		t.Fatalf("did not expect an exact stable cycle context to be marked approximate")
	}
}

func TestDashboardEnglishRendersOvulationRangeForIrregularMode(t *testing.T) {
	nowUTC := time.Now().UTC()
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{now: func() time.Time { return nowUTC }})
	user := createOnboardingTestUser(t, database, "dashboard-irregular-ovulation@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	today := services.DateAtLocation(nowUTC, time.UTC)
	cycleStarts := []time.Time{
		today.AddDate(0, 0, -84),
		today.AddDate(0, 0, -58),
		today.AddDate(0, 0, -26),
		today.AddDate(0, 0, -5),
	}
	for _, day := range cycleStarts {
		if err := database.Create(&models.DailyLog{UserID: user.ID, Date: day, IsPeriod: true, CycleStart: true, Flow: models.FlowMedium}).Error; err != nil {
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
	header := dashboardElementByDataAttr(document, "data-dashboard-status-header")
	if header == nil {
		t.Fatal("expected irregular range state to keep the dashboard status header")
	}
	assertRenderedWindowIsTheContextRange(t, database, user.ID, nowUTC, header, "dashboard.next_period_start_window")
	if htmlAttr(header, "data-cycle-hero-approximate") != "true" {
		t.Fatalf("expected the irregular range header to be marked approximate")
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
		if err := database.Create(&models.DailyLog{UserID: user.ID, Date: day, IsPeriod: true, CycleStart: true, Flow: models.FlowMedium}).Error; err != nil {
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
	ribbon := dashboardElementByDataAttr(document, "data-dashboard-cycle-ribbon")
	if ribbon == nil {
		t.Fatal("expected the dashboard status header ribbon slot in sparse irregular state")
	}
	if got := htmlAttr(ribbon, "data-cycle-ribbon-visible"); got != "false" {
		t.Fatalf("did not expect a drawn ribbon before sparse irregular history becomes reliable, got %q", got)
	}
	explainer := dashboardElementByDataAttr(document, "data-dashboard-prediction-explainer")
	if explainer == nil {
		t.Fatal("expected dashboard prediction explainer block in sparse irregular state")
	}
	if got := htmlAttr(explainer, "data-explainer-key"); got != "prediction.explainer.irregular_sparse" {
		t.Fatalf("expected dashboard explainer key %q, got %q", "prediction.explainer.irregular_sparse", got)
	}
}

// assertRenderedWindowIsTheContextRange parses the "<start> — <end>" the header
// renders in English and compares the two calendar days with the range the
// dashboard context resolved for the same owner and instant
// (DisplayNextPeriodRangeStart/End). A shape-only pattern accepts "Foo 99, 0000";
// here the text must name those exact days.
func assertRenderedWindowIsTheContextRange(t *testing.T, database *gorm.DB, userID uint, now time.Time, header *html.Node, wantKey string) {
	t.Helper()

	node := dashboardElementByDataAttr(header, "data-next-period-key")
	if node == nil || htmlAttr(node, "data-next-period-key") != wantKey {
		t.Fatalf("expected the next-period line to carry data-next-period-key=%q", wantKey)
	}
	text := normalizeHTMLText(htmlNodeText(node))
	// The suffix words of the start-window string are not part of the second date.
	if cut := strings.Index(text, " ("); cut >= 0 {
		text = text[:cut]
	}

	var user models.User
	if err := database.First(&user, userID).Error; err != nil {
		t.Fatalf("reload owner: %v", err)
	}
	var logs []models.DailyLog
	if err := database.Where("user_id = ?", userID).Find(&logs).Error; err != nil {
		t.Fatalf("load owner logs: %v", err)
	}
	today := services.DateAtLocation(now, time.UTC)
	// The same derivation the dashboard service runs: stats over the two-year
	// history window read at the request instant, then the display context.
	stats := services.BuildCycleStatsFromLogs(&user, services.FilterLogsToStatsHistory(logs, today, time.UTC), now, time.UTC)
	cycle := services.BuildDashboardCycleContext(&user, logs, stats, today, time.UTC)
	wantStart, wantEnd := cycle.DisplayNextPeriodStart, cycle.DisplayNextPeriodEnd
	if wantKey == "dashboard.next_period_start_window" {
		if !cycle.DisplayNextPeriodUseRange {
			t.Fatal("fixture: the dashboard context resolved no next-period range, so there is no start window to compare")
		}
		wantStart, wantEnd = cycle.DisplayNextPeriodRangeStart, cycle.DisplayNextPeriodRangeEnd
	}

	parts := strings.Split(text, "—")
	if len(parts) != 2 {
		t.Fatalf("expected a %q window, got %q", "<start> — <end>", text)
	}
	parse := func(raw string) time.Time {
		raw = strings.TrimSpace(raw)
		for _, layout := range []string{"Jan 2, 2006", "January 2, 2006"} {
			if parsed, err := time.Parse(layout, raw); err == nil {
				return parsed
			}
		}
		t.Fatalf("window bound %q in %q is not an English date", raw, text)
		return time.Time{}
	}
	gotStart, gotEnd := parse(parts[0]), parse(parts[1])
	if got, want := services.CalendarDayKey(gotStart), services.CalendarDayKey(wantStart); got != want {
		t.Fatalf("rendered window starts %s, the dashboard context starts it on %s (%q)", got, want, text)
	}
	if got, want := services.CalendarDayKey(gotEnd), services.CalendarDayKey(wantEnd); got != want {
		t.Fatalf("rendered window ends %s, the dashboard context ends it on %s (%q)", got, want, text)
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
