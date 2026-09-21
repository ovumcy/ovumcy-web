package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/net/html"
)

// The avoid-pregnancy warning says, next to the mode chooser, that the app is
// not a method of contraception and names the situations its calendar
// estimates are least reliable in. It is rendered on both surfaces that offer
// the mode choice — onboarding step 2 and the settings cycle section — and it
// is server-rendered visible exactly when the saved goal is avoid_pregnancy, so
// the page is right before any script runs. A browser script only keeps it in
// step while the choice changes before a save.

func usageGoalAvoidWarningEnglish(t *testing.T) string {
	t.Helper()

	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("init i18n manager: %v", err)
	}
	value := strings.TrimSpace(manager.Messages(i18n.LangEN)["usage_goal.avoid_warning"])
	if value == "" {
		t.Fatal("expected usage_goal.avoid_warning to be defined in the English catalogue")
	}
	return value
}

// assertUsageGoalAvoidWarning finds the single warning on the page, checks it
// shares a scope with the usage_goal radios the script reads, carries the
// catalogue sentence, and is hidden or shown as the saved goal demands.
func assertUsageGoalAvoidWarning(t *testing.T, document *html.Node, wantVisible bool) {
	t.Helper()

	warnings := htmlElementsWithAttr(document, "data-usage-goal-avoid-warning")
	if len(warnings) != 1 {
		t.Fatalf("expected exactly one avoid-pregnancy warning, got %d", len(warnings))
	}
	warning := warnings[0]

	scope := warning.Parent
	for scope != nil && !(scope.Type == html.ElementNode && htmlHasAttr(scope, "data-usage-goal-warning-scope")) {
		scope = scope.Parent
	}
	if scope == nil {
		t.Fatal("expected the warning to sit inside a data-usage-goal-warning-scope container")
	}
	if got := htmlRadioValues(scope, "usage_goal"); len(got) != 3 {
		t.Fatalf("expected the warning scope to hold the three usage_goal radios, got %v", got)
	}

	if got, want := strings.TrimSpace(htmlNodeText(warning)), usageGoalAvoidWarningEnglish(t); got != want {
		t.Fatalf("expected warning text %q, got %q", want, got)
	}

	if hidden := htmlHasAttr(warning, "hidden"); hidden == wantVisible {
		t.Fatalf("expected warning visible=%v for the saved goal, got hidden=%v", wantVisible, hidden)
	}
}

func TestOnboardingAvoidPregnancyWarningFollowsTheSavedGoal(t *testing.T) {
	cases := []struct {
		goal        string
		wantVisible bool
	}{
		{goal: models.UsageGoalAvoid, wantVisible: true},
		{goal: models.UsageGoalHealth, wantVisible: false},
		{goal: models.UsageGoalTrying, wantVisible: false},
		{goal: "", wantVisible: false},
	}

	for _, tc := range cases {
		t.Run("goal="+tc.goal, func(t *testing.T) {
			app, database := newOnboardingTestApp(t)
			user := createOnboardingTestUser(t, database, "onboarding-avoid-warning@example.com", "StrongPass1", false)
			if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("usage_goal", tc.goal).Error; err != nil {
				t.Fatalf("seed usage goal: %v", err)
			}
			authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

			request := httptest.NewRequest(http.MethodGet, "/onboarding?step=2", nil)
			request.Header.Set("Accept-Language", "en")
			request.Header.Set("Cookie", authCookie)

			response := mustAppResponse(t, app, request)
			assertStatusCode(t, response, http.StatusOK)
			document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))

			assertUsageGoalAvoidWarning(t, document, tc.wantVisible)
		})
	}
}

func TestSettingsAvoidPregnancyWarningFollowsTheSavedGoal(t *testing.T) {
	cases := []struct {
		goal        string
		wantVisible bool
	}{
		{goal: models.UsageGoalAvoid, wantVisible: true},
		{goal: models.UsageGoalHealth, wantVisible: false},
		{goal: models.UsageGoalTrying, wantVisible: false},
	}

	for _, tc := range cases {
		t.Run("goal="+tc.goal, func(t *testing.T) {
			app, database := newOnboardingTestApp(t)
			user := createOnboardingTestUser(t, database, "settings-avoid-warning@example.com", "StrongPass1", true)
			if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("usage_goal", tc.goal).Error; err != nil {
				t.Fatalf("seed usage goal: %v", err)
			}
			authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

			document := mustParseHTMLDocument(t, renderSettingsPageForTest(t, app, authCookie))

			assertUsageGoalAvoidWarning(t, document, tc.wantVisible)
		})
	}
}
