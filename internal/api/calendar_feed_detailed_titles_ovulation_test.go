package api

import (
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestCalendarFeedDetailedTitlesNameAConfirmedOvulation covers the ovulation half
// of the opt-in end to end. The period-only fixture of
// TestCalendarFeedServesDetailedTitlesOnlyToAnOptedInLink has its fertility
// projection withheld by the medical-safety gate, so a confirmed thermal shift is
// the event that reaches the feed with the ovulation kind.
func TestCalendarFeedDetailedTitlesNameAConfirmedOvulation(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "feed-titles-ovulation@example.com", "StrongPass1", true)
	updateStatsOverviewUser(t, database, user, map[string]any{"track_bbt": true})
	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	seedLateThermalShiftCycle(t, database, user, today, 0)

	token, columns, err := services.GenerateCalendarFeedToken([]byte(testAppSecretKey))
	if err != nil {
		t.Fatalf("GenerateCalendarFeedToken: %v", err)
	}
	columns.DetailedTitles = true
	if err := db.NewRepositories(database).Users.SaveCalendarFeedToken(t.Context(), user.ID, columns); err != nil {
		t.Fatalf("SaveCalendarFeedToken: %v", err)
	}

	_, body := mustServeCalendarFeed(t, app, token, "detailed link with a confirmed ovulation")
	if !strings.Contains(body, "SUMMARY:Ovulation (estimate)") {
		t.Fatalf("an opted-in link did not name the confirmed ovulation:\n%s", body)
	}
}
