package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestDashboardEggWhiteBadgeNamesTheObservationWhileBleedingOutsideCycleStart
// pins WEB-471 at the render layer: a trying-to-conceive account with four
// completed cycles logs spotting and egg-white mucus on cycle day 11. The
// spotting withholds the fertility half (bleeding_outside_cycle_start), so the
// badge must read the observation, never "Best timing: fertile window".
func TestDashboardEggWhiteBadgeNamesTheObservationWhileBleedingOutsideCycleStart(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "egg-white-withheld@example.com", "StrongPass1", true)
	today := time.Now().UTC().Truncate(24 * time.Hour)
	periodStart := today.AddDate(0, 0, -10)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": periodStart,
		"usage_goal":        models.UsageGoalTrying,
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
	spotting := models.DailyLog{UserID: user.ID, Date: today, IsPeriod: true, Flow: models.FlowSpotting, CervicalMucus: models.CervicalMucusEggWhite}
	if err := database.Create(&spotting).Error; err != nil {
		t.Fatalf("create spotting log: %v", err)
	}
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	dashboard := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))

	badge := htmlFindElement(dashboard, htmlNodeHasAttr("data-fertility-badge"))
	if badge == nil {
		t.Fatal("expected the egg-white badge to stay on the dashboard")
	}
	if key := htmlAttr(badge, "data-fertility-badge-key"); key != "dashboard.fertility_badge_observed" {
		t.Fatalf("badge renders from %q, want dashboard.fertility_badge_observed", key)
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
}
