package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/net/html"
)

// A <progress> element has no accessible name of its own: its fallback text
// is not a label, so each meter points at visible text that names it.
func assertEveryProgressMeterIsNamed(t *testing.T, document *html.Node) int {
	t.Helper()
	meters := htmlFindElements(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "progress"
	})
	for index, meter := range meters {
		var label strings.Builder
		for _, id := range strings.Fields(htmlAttr(meter, "aria-labelledby")) {
			if target := htmlElementByID(document, id); target != nil {
				label.WriteString(htmlNodeText(target))
			}
		}
		if strings.TrimSpace(label.String()) == "" && strings.TrimSpace(htmlAttr(meter, "aria-label")) == "" {
			t.Errorf("progress meter %d has no accessible name", index)
		}
	}
	return len(meters)
}

func TestStatsEmptyStateProgressMeterHasAnAccessibleName(t *testing.T) {
	document := renderStatsPageWithoutCompletedCycles(t)
	if assertEveryProgressMeterIsNamed(t, document) == 0 {
		t.Fatal("expected the empty state to render its progress meter")
	}
}

func TestStatsPhaseMoodMetersHaveAnAccessibleName(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "stats-mood-meter@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	logs := make([]models.DailyLog, 0, 100)
	var latestStart time.Time
	for i := 3; i >= 0; i-- {
		latestStart = today.AddDate(0, 0, -8-28*i)
		logs = append(logs, models.DailyLog{UserID: user.ID, Date: latestStart, IsPeriod: true, CycleStart: true, Mood: 3})
		for day := 1; day < 24; day++ {
			logs = append(logs, models.DailyLog{UserID: user.ID, Date: latestStart.AddDate(0, 0, day), Mood: 3})
		}
	}
	if err := database.Create(&logs).Error; err != nil {
		t.Fatalf("seed logs: %v", err)
	}
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("last_period_start", latestStart).Error; err != nil {
		t.Fatalf("update user: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/stats", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	document := mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))

	meters := htmlFindElements(document, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "progress" && htmlHasClass(node, "phase-mood-meter")
	})
	if len(meters) == 0 {
		t.Fatal("expected at least one phase mood meter")
	}
	assertEveryProgressMeterIsNamed(t, document)
}
