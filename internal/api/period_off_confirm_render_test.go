package api

import (
	"bytes"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// The day forms carry the question they ask before un-ticking a period day
// whose un-tick reaches past it (WEB-267): the cycle start moves, or the
// auto-fill walk clears the days after it. A plain un-tick carries none.

var periodOffRenderNow = time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)

func newPeriodOffOwner(t *testing.T, email string, autoFill bool) (*fiber.App, *gorm.DB, string, uint) {
	t.Helper()
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{now: func() time.Time { return periodOffRenderNow }})
	user := createOnboardingTestUserAt(t, database, email, "StrongPass1", true, time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC))
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("auto_period_fill", autoFill).Error; err != nil {
		t.Fatalf("set auto-fill: %v", err)
	}
	return app, database, loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1"), user.ID
}

func seedPeriodOffRun(t *testing.T, database *gorm.DB, userID uint, first time.Time, days int) {
	t.Helper()
	for offset := range days {
		if err := database.Create(&models.DailyLog{
			UserID:   userID,
			Date:     first.AddDate(0, 0, offset),
			IsPeriod: true,
			Flow:     models.FlowMedium,
		}).Error; err != nil {
			t.Fatalf("seed period day: %v", err)
		}
	}
}

func renderPeriodOffPage(t *testing.T, app *fiber.App, path string, language string, authCookie string) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Accept-Language", language)
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	return html.UnescapeString(mustReadBodyString(t, response.Body))
}

func seedTwoPeriodOffCycles(t *testing.T, database *gorm.DB, userID uint) {
	t.Helper()
	seedPeriodOffRun(t, database, userID, time.Date(2026, time.July, 13, 0, 0, 0, 0, time.UTC), 5)
	seedPeriodOffRun(t, database, userID, time.Date(2026, time.August, 10, 0, 0, 0, 0, time.UTC), 5)
}

// savePeriodOffDay answers the question header of an HTMX day save, and
// whether the answer carried it at all.
func savePeriodOffDay(t *testing.T, app *fiber.App, authCookie string, userID uint, date string, isPeriod bool) (string, bool) {
	t.Helper()
	body := `{"is_period":false,"flow":"none","symptom_ids":[],"notes":""}`
	if isPeriod {
		body = `{"is_period":true,"flow":"medium","symptom_ids":[],"notes":""}`
	}
	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/"+date, bytes.NewReader([]byte(body)))
	request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	bindDayWriteForTest(t, request, userID)
	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("PUT %s = %d: %s", date, response.StatusCode, mustReadBodyString(t, response.Body))
	}
	values, carried := response.Header[http.CanonicalHeaderKey(periodOffConfirmHeader)]
	if !carried || len(values) != 1 {
		return "", false
	}
	question, err := url.QueryUnescape(values[0])
	if err != nil {
		t.Fatalf("unescape %s: %v", periodOffConfirmHeader, err)
	}
	return question, true
}

func TestDayEditorAsksBeforeUntickingACycleStartDay(t *testing.T) {
	app, database, authCookie, userID := newPeriodOffOwner(t, "period-off-editor@example.com", false)
	seedTwoPeriodOffCycles(t, database, userID)

	rendered := renderPeriodOffPage(t, app, "/calendar/day/2026-08-10?mode=edit", "en", authCookie)
	for _, want := range []string{
		`data-period-off-confirm="Un-marking this day moves the start of your cycle. Your statistics will be recalculated."`,
		`data-period-off-confirm-accept="Yes, un-mark"`,
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected %s on the cycle-start day's period toggle", want)
		}
	}

	if inside := renderPeriodOffPage(t, app, "/calendar/day/2026-08-12?mode=edit", "en", authCookie); strings.Contains(inside, "data-period-off-confirm=") {
		t.Fatal("a day inside a period must un-tick without a question")
	}
}

func TestDayEditorNamesTheAutoFilledDaysAnUntickClears(t *testing.T) {
	app, database, authCookie, userID := newPeriodOffOwner(t, "period-off-fill-editor@example.com", true)
	seedTwoPeriodOffCycles(t, database, userID)

	for language, want := range map[string]string{
		"en": "Un-marking this day moves the start of your cycle and clears the period from 4 following days.",
		"ru": "а месячные будут сняты и с 4 следующих дней.",
	} {
		if rendered := renderPeriodOffPage(t, app, "/calendar/day/2026-08-10?mode=edit", language, authCookie); !strings.Contains(rendered, want) {
			t.Fatalf("%s: expected the question to name the four cleared days (%q)", language, want)
		}
	}
}

func TestDashboardAsksBeforeUntickingTodaysCycleStart(t *testing.T) {
	app, database, authCookie, userID := newPeriodOffOwner(t, "period-off-dashboard@example.com", false)
	seedPeriodOffRun(t, database, userID, time.Date(2026, time.October, 5, 0, 0, 0, 0, time.UTC), 1)

	rendered := renderPeriodOffPage(t, app, "/dashboard", "en", authCookie)
	if !strings.Contains(rendered, `data-period-off-confirm="Un-marking this day moves the start of your cycle.`) {
		t.Fatal("expected the Today form's period toggle to ask before un-ticking today's cycle start")
	}
}

// A form stays open after its save, so the save answers with the question the
// toggle asks from then on: a tick saved in the open form now reaches past the
// day, and an un-tick saved there leaves nothing to ask about.
func TestDaySaveAnswersWithTheQuestionTheSavedToggleAsks(t *testing.T) {
	app, database, authCookie, userID := newPeriodOffOwner(t, "period-off-save@example.com", true)
	seedPeriodOffRun(t, database, userID, time.Date(2026, time.July, 13, 0, 0, 0, 0, time.UTC), 5)

	question, carried := savePeriodOffDay(t, app, authCookie, userID, "2026-08-10", true)
	if want := "Un-marking this day moves the start of your cycle and clears the period from 4 following days."; !carried || !strings.Contains(question, want) {
		t.Fatalf("tick save: header carried=%t question=%q, want it to contain %q", carried, question, want)
	}

	question, carried = savePeriodOffDay(t, app, authCookie, userID, "2026-08-10", false)
	if !carried || question != "" {
		t.Fatalf("un-tick save: header carried=%t question=%q, want an empty header", carried, question)
	}
}
