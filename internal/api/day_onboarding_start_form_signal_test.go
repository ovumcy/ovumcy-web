package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

// storedStartFormFixture onboards an owner whose stored start is today with no
// day row: onboarding with auto-fill off records the start and writes nothing,
// and the calendar paints that day as a period day from the stored start alone.
func storedStartFormFixture(t *testing.T, email string) (*fiber.App, *gorm.DB, models.User, string, time.Time) {
	t.Helper()
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, email, "StrongPass1", true)
	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"last_period_start": today,
		"period_length":     5,
		"auto_period_fill":  false,
	}).Error; err != nil {
		t.Fatalf("seed stored start: %v", err)
	}
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	return app, database, user, authCookie, today
}

func storedStartAfterSave(t *testing.T, database *gorm.DB, userID uint) *time.Time {
	t.Helper()
	persisted := models.User{}
	if err := database.First(&persisted, userID).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	return persisted.LastPeriodStart
}

// renderedDayForm GETs a page and returns the day form's markup from marker
// on, so the period toggle and hidden fields read are the form's own.
func renderedDayForm(t *testing.T, app *fiber.App, authCookie, path, marker string) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	body := mustReadBodyString(t, response.Body)
	index := strings.Index(body, marker)
	if index < 0 {
		t.Fatalf("%s renders no %q form", path, marker)
	}
	return body[index:]
}

// formPeriodToggleChecked reports whether the form's is_period checkbox renders
// checked.
func formPeriodToggleChecked(t *testing.T, form string) bool {
	t.Helper()
	index := strings.Index(form, `name="is_period"`)
	if index < 0 {
		t.Fatal("the day form renders no is_period checkbox")
	}
	end := strings.Index(form[index:], ">")
	return strings.Contains(form[index:index+end], " checked")
}

const storedStartFieldMarkup = `<input type="hidden" name="period_from_stored_start" value="true">`

// formPostedAsRendered is what the browser posts for the form as rendered, plus
// the owner's own edits: the period box as shown and the hidden field when
// rendered.
func formPostedAsRendered(t *testing.T, form string, edits url.Values) url.Values {
	t.Helper()
	values := url.Values{}
	if formPeriodToggleChecked(t, form) {
		values.Set("is_period", "true")
	}
	if strings.Contains(form, storedStartFieldMarkup) {
		values.Set("period_from_stored_start", "true")
	}
	for key, value := range edits {
		values[key] = value
	}
	return values
}

func putDayForm(t *testing.T, app *fiber.App, authCookie, dateISO string, form url.Values) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/"+dateISO, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
}

// TestTheDashboardTicksAStoredStartAndPostsWhereTheTickCameFrom: the Today
// form shows the stored start's period ticked and carries the hidden field
// saying the tick came from the stored start, while the delete button and the
// entry-exists flag still read the absent row.
func TestTheDashboardTicksAStoredStartAndPostsWhereTheTickCameFrom(t *testing.T) {
	app, _, _, authCookie, _ := storedStartFormFixture(t, "stored-start-dashboard-render@example.com")
	form := renderedDayForm(t, app, authCookie, "/dashboard", "data-dashboard-save-form")
	if !formPeriodToggleChecked(t, form) {
		t.Fatal("the dashboard's Today form shows the period unticked on a stored start the calendar paints")
	}
	if !strings.Contains(form, storedStartFieldMarkup) {
		t.Fatal("the dashboard's Today form does not post that its period tick came from the stored start")
	}
	if !strings.Contains(form, `data-today-entry-exists="false"`) {
		t.Fatal("the stored start's tick made the dashboard treat today as a saved entry")
	}
	if strings.Contains(form, "data-dashboard-clear-button") {
		t.Fatal("the stored start's tick put a delete button on a day without a row")
	}
}

// TestADashboardMoodSaveKeepsARowlessStoredStart: the owner adds a mood on
// the Today form of a stored start dated today and leaves the period box as
// shown. That is not an un-mark: the start stays.
func TestADashboardMoodSaveKeepsARowlessStoredStart(t *testing.T) {
	app, database, user, authCookie, today := storedStartFormFixture(t, "stored-start-dashboard-mood@example.com")
	form := renderedDayForm(t, app, authCookie, "/dashboard", "data-dashboard-save-form")
	putDayForm(t, app, authCookie, today.Format("2006-01-02"), formPostedAsRendered(t, form, url.Values{"mood": {"3"}}))
	if storedStartAfterSave(t, database, user.ID) == nil {
		t.Fatal("a mood save from the dashboard cleared the stored onboarding start dated today")
	}
}

// TestAJSONMoodOnlyPutKeepsARowlessStoredStart: a JSON write never showed a
// tick, so saving a mood on the stored start's date without a period is not
// an un-mark.
func TestAJSONMoodOnlyPutKeepsARowlessStoredStart(t *testing.T) {
	app, database, user, authCookie, today := storedStartFormFixture(t, "stored-start-json-mood@example.com")
	putDayPayloadExpectOK(t, app, authCookie, today.Format("2006-01-02"), map[string]any{"mood": 3}, "mood-only JSON PUT")
	if storedStartAfterSave(t, database, user.ID) == nil {
		t.Fatal("a mood-only JSON PUT cleared the stored onboarding start on a date without a row")
	}
}

// TestUntickingAStoredStartInEitherFormWithdrawsIt: un-ticking the period the
// form showed from the stored start posts the hidden field without is_period,
// and that withdraws the start.
func TestUntickingAStoredStartInEitherFormWithdrawsIt(t *testing.T) {
	for _, surface := range []struct {
		name, email, marker string
		path                func(time.Time) string
	}{
		{"dashboard", "stored-start-untick-dashboard@example.com", "data-dashboard-save-form", func(time.Time) string { return "/dashboard" }},
		{"day editor", "stored-start-untick-editor@example.com", `name="source" value="calendar"`, func(day time.Time) string { return "/calendar/day/" + day.Format("2006-01-02") + "?mode=edit" }},
	} {
		t.Run(surface.name, func(t *testing.T) {
			app, database, user, authCookie, today := storedStartFormFixture(t, surface.email)
			form := renderedDayForm(t, app, authCookie, surface.path(today), surface.marker)
			if !strings.Contains(form, storedStartFieldMarkup) {
				t.Fatalf("the %s form does not post that its period tick came from the stored start", surface.name)
			}
			posted := formPostedAsRendered(t, form, url.Values{"mood": {"3"}})
			posted.Del("is_period")
			putDayForm(t, app, authCookie, today.Format("2006-01-02"), posted)
			if stored := storedStartAfterSave(t, database, user.ID); stored != nil {
				t.Fatalf("last_period_start = %v after the %s un-tick, want it withdrawn", stored, surface.name)
			}
		})
	}
}
