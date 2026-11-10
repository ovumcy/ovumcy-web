package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/net/html"
)

// TestDashboardEggWhiteBadgeNamesTheObservationWhileBleedingOutsideCycleStart
// pins WEB-471 and WEB-476 at the render layer: an account with four completed
// cycles logs egg-white mucus on cycle day 11. Spotting the same day withholds
// the fertility half (bleeding_outside_cycle_start), so the badge must read the
// observation, never "Best timing: fertile window", and carry the neutral
// variant whatever the goal: the warning or positive colour is a timing cue of
// its own. Without the spotting the goal styling stays.
func TestDashboardEggWhiteBadgeNamesTheObservationWhileBleedingOutsideCycleStart(t *testing.T) {
	for name, testCase := range map[string]struct {
		goal        string
		spotting    bool
		wantVariant string
		wantClass   string
		wantKey     string
	}{
		"withheld/trying_to_conceive": {goal: models.UsageGoalTrying, spotting: true, wantVariant: "neutral", wantKey: "dashboard.fertility_badge_observed"},
		"withheld/avoid_pregnancy":    {goal: models.UsageGoalAvoid, spotting: true, wantVariant: "neutral", wantKey: "dashboard.fertility_badge_observed"},
		"shown/trying_to_conceive":    {goal: models.UsageGoalTrying, wantVariant: "positive", wantClass: "dashboard-status-item-positive", wantKey: "dashboard.fertility_badge_positive"},
		"shown/avoid_pregnancy":       {goal: models.UsageGoalAvoid, wantVariant: "warning", wantClass: "dashboard-status-item-warning", wantKey: "dashboard.fertility_badge_warning"},
	} {
		t.Run(name, func(t *testing.T) {
			badge := renderDashboardEggWhiteBadge(t, testCase.goal, testCase.spotting)
			if variant := htmlAttr(badge, "data-fertility-badge-variant"); variant != testCase.wantVariant {
				t.Fatalf("badge variant=%q, want %q", variant, testCase.wantVariant)
			}
			for _, goalClass := range []string{"dashboard-status-item-warning", "dashboard-status-item-positive"} {
				if htmlHasClass(badge, goalClass) != (goalClass == testCase.wantClass) {
					t.Fatalf("badge class=%q, want goal class %q only", htmlAttr(badge, "class"), testCase.wantClass)
				}
			}
			if key := htmlAttr(badge, "data-fertility-badge-key"); key != testCase.wantKey {
				t.Fatalf("badge renders from %q, want %s", key, testCase.wantKey)
			}
			if !testCase.spotting {
				return
			}
			manager, err := i18n.NewManager(i18n.LangEN)
			if err != nil {
				t.Fatalf("init i18n manager: %v", err)
			}
			text := normalizeHTMLText(htmlNodeText(badge))
			if want := normalizeHTMLText(manager.Messages("en")["dashboard.fertility_badge_observed"]); text != want {
				t.Fatalf("badge reads %q, want %q", text, want)
			}
			if strings.Contains(strings.ToLower(text), "fertile window") {
				t.Fatalf("badge claims a fertile window while fertility is withheld: %q", text)
			}
		})
	}
}

func renderDashboardEggWhiteBadge(t *testing.T, goal string, spotting bool) *html.Node {
	t.Helper()
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "egg-white-withheld@example.com", "StrongPass1", true)
	today := time.Now().UTC().Truncate(24 * time.Hour)
	periodStart := today.AddDate(0, 0, -10)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": periodStart,
		"usage_goal":        goal,
	}).Error; err != nil {
		t.Fatalf("update user cycle settings: %v", err)
	}
	for back := 4; back >= 0; back-- {
		cycleStart := periodStart.AddDate(0, 0, -28*back)
		for offset := range 5 {
			entry := models.DailyLog{UserID: user.ID, Date: cycleStart.AddDate(0, 0, offset), IsPeriod: true, Flow: models.FlowMedium}
			if err := database.Create(&entry).Error; err != nil {
				t.Fatalf("create period log: %v", err)
			}
		}
	}
	todayLog := models.DailyLog{UserID: user.ID, Date: today, CervicalMucus: models.CervicalMucusEggWhite}
	if spotting {
		todayLog.IsPeriod, todayLog.Flow = true, models.FlowSpotting
	}
	if err := database.Create(&todayLog).Error; err != nil {
		t.Fatalf("create today log: %v", err)
	}
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	sendRequestTimezoneForTest(request, testRequestTimezone)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	dashboard := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))

	badge := htmlFindElement(dashboard, htmlNodeHasAttr("data-fertility-badge"))
	if badge == nil {
		t.Fatal("expected the egg-white badge to stay on the dashboard")
	}
	return badge
}
