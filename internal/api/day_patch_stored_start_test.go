package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// A partial write (PATCH) changes only the fields it names. On the date of a
// stored onboarding start that has no day row, a PATCH that leaves is_period
// out says nothing about the period, so it must never withdraw the start —
// neither over JSON, which has no key for the form's stored-start marker, nor
// from a form that posts the marker beside an unchecked box: a partial write
// reads an absent is_period as "not stated", never as an un-tick. A PATCH that
// states is_period=false over a stored period day on that date is the same
// un-mark a full write is, and withdraws the start.

// TestAPatchThatOmitsIsPeriodKeepsARowlessStoredStart pins both shapes of the
// omitted is_period on a row-less stored start.
func TestAPatchThatOmitsIsPeriodKeepsARowlessStoredStart(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		app, database, user, authCookie, today := storedStartFormFixture(t, "stored-start-patch-json@example.com")
		path := "/api/v1/days/" + today.Format("2006-01-02")
		status, raw := sendDayRequest(t, app, http.MethodPatch, path, authCookie, "", `{"mood":3}`)
		if status != http.StatusOK {
			t.Fatalf("mood-only JSON PATCH answered %d: %s", status, raw)
		}
		if storedStartAfterSave(t, database, user.ID) == nil {
			t.Fatal("a mood-only JSON PATCH cleared the stored onboarding start on a date without a row")
		}
	})

	t.Run("form with the stored-start marker", func(t *testing.T) {
		app, database, user, authCookie, today := storedStartFormFixture(t, "stored-start-patch-form@example.com")
		form := url.Values{"mood": {"3"}, "period_from_stored_start": {"true"}}
		request := httptest.NewRequest(http.MethodPatch, "/api/v1/days/"+today.Format("2006-01-02"), strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("HX-Request", "true")
		request.Header.Set("Accept-Language", "en")
		request.Header.Set("Cookie", authCookie)
		response := mustAppResponse(t, app, request)
		assertStatusCode(t, response, http.StatusOK)
		if storedStartAfterSave(t, database, user.ID) == nil {
			t.Fatal("a form PATCH without is_period withdrew the stored onboarding start because it posted the stored-start marker")
		}
	})
}

// TestAPatchStatingNoPeriodOverTheStoredStartDayWithdrawsIt: the start's date
// holds a saved period day, and a PATCH that states is_period=false un-marks it
// exactly as a PUT does.
func TestAPatchStatingNoPeriodOverTheStoredStartDayWithdrawsIt(t *testing.T) {
	app, database, user, authCookie, today := storedStartFormFixture(t, "stored-start-patch-untick@example.com")
	seedPatchMergeDay(t, database, models.DailyLog{UserID: user.ID, Date: today, IsPeriod: true, Flow: models.FlowMedium})
	path := "/api/v1/days/" + today.Format("2006-01-02")
	status, raw := sendDayRequest(t, app, http.MethodPatch, path, authCookie, "", `{"is_period":false}`)
	if status != http.StatusOK {
		t.Fatalf("is_period=false JSON PATCH answered %d: %s", status, raw)
	}
	if stored := storedStartAfterSave(t, database, user.ID); stored != nil {
		t.Fatalf("last_period_start = %v after a PATCH un-marked the start's period day, want it withdrawn", stored)
	}
}
