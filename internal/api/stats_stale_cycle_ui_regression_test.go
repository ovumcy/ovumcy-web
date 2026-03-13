package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func TestStatsPageShowsUnknownPhaseWhenCycleDataIsStale(t *testing.T) {
	rendered := renderStatsPageWithStaleCycleData(t)
	if !strings.Contains(rendered, "Enter the start of your next period to complete your first cycle and unlock insights.") {
		t.Fatalf("expected updated empty-state guidance before the first completed cycle")
	}
}

func TestStatsPageEmptyStateUsesDedicatedProgressMeterWithoutInlineStyle(t *testing.T) {
	rendered := renderStatsPageWithStaleCycleData(t)

	progressTag := regexp.MustCompile(`<div[^>]*class="stats-empty-progress"[^>]*>`).FindString(rendered)
	if progressTag == "" {
		t.Fatalf("expected stats empty state to render a dedicated progress meter, got %q", rendered)
	}
	if strings.Contains(progressTag, "style=") {
		t.Fatalf("expected progress meter tag to avoid inline style attributes under strict CSP, got %q", progressTag)
	}
}

func renderStatsPageWithStaleCycleData(t *testing.T) string {
	t.Helper()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "stats-stale-ui@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	lastPeriodStart := services.DateAtLocation(time.Now().UTC(), time.UTC).AddDate(0, 0, -60)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": lastPeriodStart,
	}).Error; err != nil {
		t.Fatalf("update user cycle context: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/stats", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("stats request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read stats body: %v", err)
	}
	return string(body)
}
