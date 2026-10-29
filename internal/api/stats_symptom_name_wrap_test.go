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

// A long custom symptom name must be readable on a touch screen: it wraps
// instead of being clipped with the full text only in a hover-only title.
func TestStatsLongSymptomNamesWrapInsteadOfClippingBehindATitle(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "stats-long-symptom@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	longName := "Pulsating headache behind the left eye"
	symptom := models.SymptomType{UserID: user.ID, Name: longName, Icon: "H", Color: "#111111"}
	if err := database.Create(&symptom).Error; err != nil {
		t.Fatalf("create symptom: %v", err)
	}

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	logs := make([]models.DailyLog, 0, 8)
	var latestStart time.Time
	for i := 3; i >= 0; i-- {
		latestStart = today.AddDate(0, 0, -8-28*i)
		logs = append(logs,
			models.DailyLog{UserID: user.ID, Date: latestStart, IsPeriod: true, CycleStart: true},
			models.DailyLog{UserID: user.ID, Date: latestStart.AddDate(0, 0, 2), SymptomIDs: []uint{symptom.ID}},
		)
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

	names := htmlFindElements(document, func(node *html.Node) bool {
		return htmlHasClass(node, "stats-symptom-name") && strings.Contains(htmlNodeText(node), longName)
	})
	if len(names) == 0 {
		t.Fatal("expected the long symptom name to render in the stats page")
	}
	for index, name := range names {
		if htmlHasAttr(name, "title") {
			t.Errorf("name %d: the full text must not hide behind a hover-only title", index)
		}
		if !htmlHasClass(name, "whitespace-normal!") {
			t.Errorf("name %d: expected the name to wrap rather than clip", index)
		}
	}
}
