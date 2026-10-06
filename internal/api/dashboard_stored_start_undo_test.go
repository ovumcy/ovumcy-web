package api

import (
	"net/url"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

const (
	tickFromStoredStartAttr   = `data-today-period-from-stored-start="true"`
	noTickFromStoredStartAttr = `data-today-period-from-stored-start="false"`
)

// TestTheDashboardTellsItsUndoATodayTickedFromAStoredStartIsNotEmpty: the
// autosave keeps one step back, and a step back to a day rendered without a
// saved row is taken as a DELETE, which withdraws a row-less stored start. So
// the Today form says, apart from the raw entry-exists flag, that its period
// tick came from the stored start, and stops saying so once a row exists.
func TestTheDashboardTellsItsUndoATodayTickedFromAStoredStartIsNotEmpty(t *testing.T) {
	app, _, _, authCookie, today := storedStartFormFixture(t, "stored-start-dashboard-undo-attr@example.com")
	form := renderedDayForm(t, app, authCookie, "/dashboard", "data-dashboard-save-form")
	if !strings.Contains(form, tickFromStoredStartAttr) {
		t.Fatal("the dashboard does not tell its undo that today's period tick came from the stored start")
	}
	if !strings.Contains(form, `data-today-entry-exists="false"`) {
		t.Fatal("the stored start's tick made the dashboard report a saved row")
	}

	putDayForm(t, app, authCookie, today.Format("2006-01-02"), formPostedAsRendered(t, form, url.Values{"mood": {"3"}}))
	saved := renderedDayForm(t, app, authCookie, "/dashboard", "data-dashboard-save-form")
	if !strings.Contains(saved, noTickFromStoredStartAttr) {
		t.Fatal("the dashboard still credits the stored start with a tick a saved row now holds")
	}
}

// TestUndoingADashboardMoodSaveOnAStoredStartKeepsIt: the undo of the first
// mood save re-sends the form as rendered — the period ticked and the hidden
// stored-start field — and that write keeps the stored start.
func TestUndoingADashboardMoodSaveOnAStoredStartKeepsIt(t *testing.T) {
	app, database, user, authCookie, today := storedStartFormFixture(t, "stored-start-dashboard-undo-mood@example.com")
	form := renderedDayForm(t, app, authCookie, "/dashboard", "data-dashboard-save-form")
	dateISO := today.Format("2006-01-02")
	putDayForm(t, app, authCookie, dateISO, formPostedAsRendered(t, form, url.Values{"mood": {"3"}}))
	putDayForm(t, app, authCookie, dateISO, formPostedAsRendered(t, form, nil))
	if storedStartAfterSave(t, database, user.ID) == nil {
		t.Fatal("undoing a mood save on the stored start's day withdrew the stored onboarding start")
	}
}

// TestUndoingADashboardUntickOfAStoredStartRestoresThePeriodDay: the un-tick
// withdraws the stored start; its undo re-sends the period ticked, which
// writes today as a period row. The day reads as a period day again, now
// from the row; the withdrawn stored start stays withdrawn.
func TestUndoingADashboardUntickOfAStoredStartRestoresThePeriodDay(t *testing.T) {
	app, database, user, authCookie, today := storedStartFormFixture(t, "stored-start-dashboard-undo-untick@example.com")
	form := renderedDayForm(t, app, authCookie, "/dashboard", "data-dashboard-save-form")
	dateISO := today.Format("2006-01-02")
	unticked := formPostedAsRendered(t, form, nil)
	unticked.Del("is_period")
	putDayForm(t, app, authCookie, dateISO, unticked)
	if storedStartAfterSave(t, database, user.ID) != nil {
		t.Fatal("precondition: the form's un-tick did not withdraw the stored start")
	}

	putDayForm(t, app, authCookie, dateISO, formPostedAsRendered(t, form, nil))
	var periodRows int64
	if err := database.Model(&models.DailyLog{}).Where("user_id = ? AND is_period = ?", user.ID, true).Count(&periodRows).Error; err != nil {
		t.Fatalf("count period rows: %v", err)
	}
	if periodRows != 1 {
		t.Fatalf("undoing the un-tick left %d period rows, want today's one", periodRows)
	}
	if storedStartAfterSave(t, database, user.ID) != nil {
		t.Fatal("undoing the un-tick brought the stored start back beside today's period row")
	}
}
