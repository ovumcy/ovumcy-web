package api

import (
	"net/http"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// A row-less stored onboarding start renders its period ticked. A partial write
// that leaves is_period out records that tick beside the field it changes, so
// the day still reads as the period day after a reload; one that states
// is_period=false against the rendered tick un-ticks it and withdraws the start.

func TestAPatchThatOmitsIsPeriodRecordsTheStoredStartTick(t *testing.T) {
	app, database, user, authCookie, today := storedStartFormFixture(t, "stored-start-patch-tick@example.com")
	status, raw := sendDayRequest(t, app, http.MethodPatch, "/api/v1/days/"+today.Format("2006-01-02"), authCookie, "", `{"mood":3}`)
	if status != http.StatusOK {
		t.Fatalf("mood-only JSON PATCH answered %d: %s", status, raw)
	}
	var stored models.DailyLog
	if err := database.Where("user_id = ?", user.ID).First(&stored).Error; err != nil {
		t.Fatalf("load day: %v", err)
	}
	if !stored.IsPeriod || stored.Mood != 3 {
		t.Fatalf("expected the rendered period tick recorded beside the mood, got is_period=%v mood=%d", stored.IsPeriod, stored.Mood)
	}
	if storedStartAfterSave(t, database, user.ID) == nil {
		t.Fatal("the stored onboarding start must stay")
	}
}

func TestAPatchUntickingARowlessStoredStartAgainstItsTickWithdrawsIt(t *testing.T) {
	app, database, user, authCookie, today := storedStartFormFixture(t, "stored-start-patch-untick-rowless@example.com")
	status, raw := sendDayRequest(t, app, http.MethodPatch, "/api/v1/days/"+today.Format("2006-01-02"), authCookie, "", `{"is_period":false,"base_is_period":true}`)
	if status != http.StatusOK {
		t.Fatalf("un-tick JSON PATCH answered %d: %s", status, raw)
	}
	if stored := storedStartAfterSave(t, database, user.ID); stored != nil {
		t.Fatalf("last_period_start = %v after the rendered tick was un-ticked, want it withdrawn", stored)
	}
}
