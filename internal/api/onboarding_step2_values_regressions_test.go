package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/net/html"
)

func TestOnboardingPageRendersPersistedStep2Values(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "onboarding-values@example.com", "StrongPass1", false)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":     31,
		"period_length":    7,
		"auto_period_fill": true,
	}).Error; err != nil {
		t.Fatalf("update onboarding values: %v", err)
	}
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/onboarding", nil)
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("onboarding request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	rendered := string(body)
	cycleInputPattern := regexp.MustCompile(`(?s)name="cycle_length".*?value="31"`)
	if !cycleInputPattern.MatchString(rendered) {
		t.Fatalf("expected cycle slider value attribute to be rendered from DB")
	}
	periodInputPattern := regexp.MustCompile(`(?s)name="period_length".*?value="7"`)
	if !periodInputPattern.MatchString(rendered) {
		t.Fatalf("expected period slider value attribute to be rendered from DB")
	}

	autoFillPattern := regexp.MustCompile(`(?s)name="auto_period_fill".*?checked`)
	if !autoFillPattern.MatchString(rendered) {
		t.Fatalf("expected auto-period-fill checkbox to reflect persisted value")
	}
}

// TestOnboardingStep2ModeChooserLeadsWithTheNeutralDefault pins the display
// order of the mode question: the neutral default ("track my health") is the
// first option and the two alternative modes follow it. Only the order is
// pinned — the submitted value is read by name, never by position, so the
// default stays the default whether it is answered, changed, or skipped.
func TestOnboardingStep2ModeChooserLeadsWithTheNeutralDefault(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "onboarding-step2-goal-order@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/onboarding?step=2", nil)
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))

	assertUsageGoalOrder(t, htmlRadioValues(document, "usage_goal"))
}

// assertUsageGoalOrder holds the one order every usage-goal chooser renders.
func assertUsageGoalOrder(t *testing.T, values []string) {
	t.Helper()

	want := []string{models.UsageGoalHealth, models.UsageGoalAvoid, models.UsageGoalTrying}
	if len(values) != len(want) {
		t.Fatalf("expected usage-goal options %v, got %v", want, values)
	}
	for index, value := range want {
		if values[index] != value {
			t.Fatalf("expected usage-goal options %v, got %v", want, values)
		}
	}
}

// TestOnboardingStep2DropsAgeAndKeepsASkippableModeChoice pins the shape of the
// second onboarding step: the age bracket is no longer asked for here (it stays
// reachable in settings), while the usage-goal question stays visible and gains
// a plain, visible skip action rather than a small link.
func TestOnboardingStep2DropsAgeAndKeepsASkippableModeChoice(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "onboarding-step2-shape@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/onboarding?step=2", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))

	if node := htmlElementByAttr(document, "name", "age_group"); node != nil {
		t.Fatal("expected onboarding to stop collecting the age bracket")
	}
	if node := htmlElementByAttr(document, "name", "usage_goal"); node == nil {
		t.Fatal("expected the usage-goal choice to stay visible in onboarding")
	}

	skip := htmlFindElement(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && htmlHasAttr(node, "data-onboarding-usage-goal-skip")
	})
	if skip == nil {
		t.Fatal("expected a visible skip action for the usage-goal question")
	}
	if skip.Data != "button" {
		t.Fatalf("expected the skip action to be a button, got <%s>", skip.Data)
	}
}

// TestOnboardingStep2SkippedModeCompletesWithTheNeutralDefault drives the skip
// path end to end: no usage goal submitted, an age bracket submitted anyway.
// Onboarding completes, the goal falls back to the neutral default, and the age
// column is left exactly as it was — onboarding does not write it.
func TestOnboardingStep2SkippedModeCompletesWithTheNeutralDefault(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "onboarding-step2-skip@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	submitOnboardingStep1(t, app, authCookie, url.Values{
		"last_period_start": {time.Now().UTC().AddDate(0, 0, -5).Format("2006-01-02")},
	})
	submitOnboardingStep2(t, app, authCookie, url.Values{
		"cycle_length":  {"29"},
		"period_length": {"5"},
		// A forged age bracket rides along: onboarding must ignore it entirely.
		"age_group": {models.AgeGroup45Plus},
	})

	persisted := models.User{}
	if err := database.First(&persisted, user.ID).Error; err != nil {
		t.Fatalf("load persisted user: %v", err)
	}
	if !persisted.OnboardingCompleted {
		t.Fatal("expected a skipped mode choice to still complete onboarding")
	}
	if persisted.UsageGoal != models.UsageGoalHealth {
		t.Fatalf("expected the neutral default goal %q, got %q", models.UsageGoalHealth, persisted.UsageGoal)
	}
	if persisted.AgeGroup != models.AgeGroupUnknown {
		t.Fatalf("expected onboarding to leave age_group at %q, got %q", models.AgeGroupUnknown, persisted.AgeGroup)
	}
	if persisted.CycleLength != 29 {
		t.Fatalf("expected persisted cycle_length=29, got %d", persisted.CycleLength)
	}
}
