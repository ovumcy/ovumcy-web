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

// TestStatsPageWithholdsTheReliabilityAndModeTilesDuringAPregnancyPause is the
// render regression for two tiles the pause left behind on /stats: the
// "prediction reliability: building pattern / based on N cycles" card, which
// grades predictions the page has stopped making, and the third stat tile, which
// under a pause fell into its "facts only / this mode" branch — wording that
// belongs to the unpredictable-cycle setting, not to a pregnancy.
//
// Every account has three completed cycles behind it (the repro) and differs
// only in the pause and the mode flag, so each paused case has an unpaused twin
// proving the two hooks asserted absent are the ones this page renders. The
// unpredictable-cycle account is the control for the mode tile itself: its
// "facts only" tile must survive, so the pause — not suppression in general —
// is what withholds it.
func TestStatsPageWithholdsTheReliabilityAndModeTilesDuringAPregnancyPause(t *testing.T) {
	cases := []struct {
		name            string
		account         string
		flag            string
		pregnancyPaused bool
		wantReliability bool
	}{
		{name: "regular, no pause", account: "regular", wantReliability: true},
		{name: "regular, paused", account: "regular-paused", pregnancyPaused: true},
		{name: "irregular, no pause", account: "irregular", flag: "irregular_cycle", wantReliability: true},
		{name: "irregular, paused", account: "irregular-paused", flag: "irregular_cycle", pregnancyPaused: true},
		// Unpredictable mode has no reliability card by its own gate; what this
		// control pins is that the mode tile is still there without a pause.
		{name: "unpredictable, no pause", account: "unpredictable", flag: "unpredictable_cycle"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			app, database := newOnboardingTestApp(t)
			user := createOnboardingTestUser(t, database, "stats-pause-tiles-"+testCase.account+"@example.com", "StrongPass1", true)
			today := services.DateAtLocation(time.Now().UTC(), time.UTC)
			updates := map[string]any{
				"cycle_length":      28,
				"period_length":     5,
				"last_period_start": today.AddDate(0, 0, -11),
			}
			if testCase.flag != "" {
				updates[testCase.flag] = true
			}
			if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(updates).Error; err != nil {
				t.Fatalf("seed account: %v", err)
			}
			// Four recorded starts 28 days apart: three completed cycles and the
			// running one on day 12.
			for _, offsetDays := range []int{-95, -67, -39, -11} {
				if err := database.Create(&models.DailyLog{
					UserID:     user.ID,
					Date:       today.AddDate(0, 0, offsetDays),
					IsPeriod:   true,
					CycleStart: true,
				}).Error; err != nil {
					t.Fatalf("seed cycle start %d: %v", offsetDays, err)
				}
			}
			if testCase.pregnancyPaused {
				if err := database.Create(&models.DailyLog{
					UserID:        user.ID,
					Date:          today.AddDate(0, 0, -2),
					PregnancyTest: models.PregnancyTestPositive,
				}).Error; err != nil {
					t.Fatalf("seed positive pregnancy test: %v", err)
				}
			}

			authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
			request := httptest.NewRequest(http.MethodGet, "/stats", nil)
			request.Header.Set("Accept-Language", "en")
			request.Header.Set("Cookie", authCookie)
			response, err := app.Test(request, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("stats request failed: %v", err)
			}
			defer func() { _ = response.Body.Close() }()

			body := mustReadBodyString(t, response.Body)
			document := mustParseHTMLDocument(t, body)
			reliability := dashboardElementByDataAttr(document, "data-prediction-reliability")
			modeCard := dashboardElementByDataAttr(document, "data-stats-mode-card")

			if testCase.wantReliability && reliability == nil {
				t.Error("anchor: expected the reliability card for three completed cycles without a pause")
			}
			if testCase.pregnancyPaused {
				if reliability != nil {
					t.Errorf("expected the reliability card withheld during a pregnancy pause, got %q", htmlAttr(reliability, "data-prediction-reliability"))
				}
				if modeCard != nil {
					t.Error("expected the mode tile withheld during a pregnancy pause")
				}
				if strings.Contains(body, "Facts only") || strings.Contains(body, "Predictions off") {
					t.Error("expected no facts-only tile wording during a pregnancy pause")
				}
				return
			}
			if modeCard == nil {
				t.Error("anchor: expected the mode tile without a pause")
			}
		})
	}
}
