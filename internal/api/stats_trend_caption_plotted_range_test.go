package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestStatsTrendCaptionRangeSpansThePlottedCycles pins WEB-250: the chart
// caption describes the bars it sits under. Nine completed cycles are plotted
// while the cycle statistics read only the last six, so a caption that took its
// range from the statistics announced "15 to 128" under a plotted 365-day bar.
func TestStatsTrendCaptionRangeSpansThePlottedCycles(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "stats-trend-caption-range@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	currentCycleStart := today.AddDate(0, 0, -8)
	lengths := []int{28, 365, 28, 15, 128, 28, 28, 28, 28}

	starts := make([]time.Time, len(lengths)+1)
	starts[len(lengths)] = currentCycleStart
	for index := len(lengths) - 1; index >= 0; index-- {
		starts[index] = starts[index+1].AddDate(0, 0, -lengths[index])
	}
	logs := make([]models.DailyLog, 0, len(starts))
	for _, start := range starts {
		logs = append(logs, models.DailyLog{UserID: user.ID, Date: start, IsPeriod: true, CycleStart: true, Flow: models.FlowMedium})
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatalf("create period logs: %v", err)
	}
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"last_period_start": currentCycleStart,
	}).Error; err != nil {
		t.Fatalf("update user settings: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/stats", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("stats request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
	summaryNode := htmlElementByID(document, "stats-cycle-trend-summary")
	if summaryNode == nil {
		t.Fatal("expected the cycle chart summary node")
	}
	summary := htmlNodeText(summaryNode)
	for _, want := range []string{"9 completed cycles shown", "Average 75", "Range 15 to 365"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("caption %q must describe the nine plotted cycles, missing %q", summary, want)
		}
	}
}
