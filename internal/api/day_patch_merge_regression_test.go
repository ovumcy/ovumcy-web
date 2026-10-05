package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

// PATCH /api/v1/days/{date} merges: only the fields the body names change, and
// an absent field never clears anything, the cycle start included. PUT stays a
// full replace, documented as such. The scenario is a cycle-start day holding
// a period, a flow, a temperature and notes, written with one field.

type patchMergeFixture struct {
	app        *fiber.App
	database   *gorm.DB
	user       models.User
	authCookie string
	day        time.Time
	path       string
}

func newPatchMergeFixture(t *testing.T, email string) patchMergeFixture {
	t.Helper()
	app, database := newOnboardingTestApp(t)
	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	user := createOnboardingTestUserAt(t, database, email, "StrongPass1", true, today.AddDate(-1, 0, 0))

	// Three cycle starts 28 days apart, the last of them the day under test, so
	// the cycle statistics depend on that day staying a cycle start.
	for _, offset := range []int{-84, -56} {
		seedPatchMergeDay(t, database, models.DailyLog{UserID: user.ID, Date: today.AddDate(0, 0, offset), IsPeriod: true, CycleStart: true, Flow: models.FlowMedium})
	}
	day := today.AddDate(0, 0, -28)
	seedPatchMergeDay(t, database, models.DailyLog{
		UserID:     user.ID,
		Date:       day,
		IsPeriod:   true,
		CycleStart: true,
		Flow:       models.FlowHeavy,
		BBT:        new(36.45),
		Notes:      "cycle start notes",
	})

	return patchMergeFixture{
		database:   database,
		user:       user,
		authCookie: loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1"),
		day:        day,
		path:       "/api/v1/days/" + day.Format("2006-01-02"),
		app:        app,
	}
}

func seedPatchMergeDay(t *testing.T, database *gorm.DB, entry models.DailyLog) {
	t.Helper()
	if err := database.Create(&entry).Error; err != nil {
		t.Fatalf("seed day %s: %v", entry.Date.Format("2006-01-02"), err)
	}
}

func (fixture patchMergeFixture) stored(t *testing.T) models.DailyLog {
	t.Helper()
	entry, err := fetchLogByDateForTest(fixture.database, fixture.user.ID, fixture.day, time.UTC)
	if err != nil {
		t.Fatalf("load day: %v", err)
	}
	return entry
}

// cycleStats reads the cycle-length figures of the stats overview, the
// numbers a lost cycle start moves.
func (fixture patchMergeFixture) cycleStats(t *testing.T) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/stats/overview", nil)
	request.Header.Set("Cookie", fixture.authCookie)
	request.Header.Set("Accept", "application/json")
	response := mustAppResponse(t, fixture.app, request)
	assertStatusCode(t, response, http.StatusOK)
	var overview struct {
		CurrentCycleDay    int     `json:"current_cycle_day"`
		MedianCycleLength  int     `json:"median_cycle_length"`
		LastCycleLength    int     `json:"last_cycle_length"`
		AverageCycleLength float64 `json:"average_cycle_length"`
	}
	if err := json.NewDecoder(response.Body).Decode(&overview); err != nil {
		t.Fatalf("decode stats overview: %v", err)
	}
	encoded, _ := json.Marshal(overview)
	return string(encoded)
}

func (fixture patchMergeFixture) sendJSON(t *testing.T, method string, body string) int {
	t.Helper()
	status, raw := sendDayRequest(t, fixture.app, method, fixture.path, fixture.authCookie, "", body)
	if status != http.StatusOK {
		t.Logf("%s %s answered %d: %s", method, fixture.path, status, raw)
	}
	return status
}

func TestPatchDayWithOneFieldKeepsEveryOtherFieldAndTheCycleStart(t *testing.T) {
	fixture := newPatchMergeFixture(t, "patch-merge-json@example.com")
	statsBefore := fixture.cycleStats(t)

	if status := fixture.sendJSON(t, http.MethodPatch, `{"mood":3}`); status != http.StatusOK {
		t.Fatalf("PATCH {\"mood\":3}: status %d, want 200", status)
	}

	saved := fixture.stored(t)
	if saved.Mood != 3 {
		t.Fatalf("expected mood 3, got %d", saved.Mood)
	}
	if !saved.IsPeriod || !saved.CycleStart || saved.Flow != models.FlowHeavy {
		t.Fatalf("PATCH {\"mood\":3} changed the period: is_period=%v cycle_start=%v flow=%q", saved.IsPeriod, saved.CycleStart, saved.Flow)
	}
	if saved.BBT == nil || *saved.BBT != 36.45 || saved.Notes != "cycle start notes" {
		t.Fatalf("PATCH {\"mood\":3} changed fields it does not name: bbt=%v notes=%q", saved.BBT, saved.Notes)
	}
	if statsAfter := fixture.cycleStats(t); statsAfter != statsBefore {
		t.Fatalf("PATCH {\"mood\":3} moved the cycle statistics: before %s, after %s", statsBefore, statsAfter)
	}
}

// TestPutDayWithOneFieldStillReplacesTheWholeDay pins the documented PUT
// contract next to the PATCH one, and is the control showing the statistics
// read above do move when the cycle start is lost.
func TestPutDayWithOneFieldStillReplacesTheWholeDay(t *testing.T) {
	fixture := newPatchMergeFixture(t, "patch-merge-put@example.com")
	statsBefore := fixture.cycleStats(t)

	if status := fixture.sendJSON(t, http.MethodPut, `{"mood":3}`); status != http.StatusOK {
		t.Fatalf("PUT {\"mood\":3}: status %d, want 200", status)
	}

	saved := fixture.stored(t)
	if saved.Mood != 3 || saved.IsPeriod || saved.CycleStart || saved.Flow != models.FlowNone || saved.BBT != nil || saved.Notes != "" {
		t.Fatalf("expected PUT to replace the whole day, got mood=%d is_period=%v cycle_start=%v flow=%q bbt=%v notes=%q",
			saved.Mood, saved.IsPeriod, saved.CycleStart, saved.Flow, saved.BBT, saved.Notes)
	}
	if statsAfter := fixture.cycleStats(t); statsAfter == statsBefore {
		t.Fatalf("expected losing the cycle start to move the cycle statistics, both read %s", statsBefore)
	}
}

func TestPatchDayStatingNoPeriodClearsFlowAndCycleStartOnly(t *testing.T) {
	fixture := newPatchMergeFixture(t, "patch-merge-no-period@example.com")

	if status := fixture.sendJSON(t, http.MethodPatch, `{"is_period":false,"bbt":null}`); status != http.StatusOK {
		t.Fatalf("PATCH: status %d, want 200", status)
	}

	saved := fixture.stored(t)
	if saved.IsPeriod || saved.CycleStart || saved.Flow != models.FlowNone {
		t.Fatalf("expected is_period=false to clear flow and cycle start, got is_period=%v cycle_start=%v flow=%q", saved.IsPeriod, saved.CycleStart, saved.Flow)
	}
	if saved.BBT != nil {
		t.Fatalf("expected an explicit null to clear the temperature, got %v", *saved.BBT)
	}
	if saved.Notes != "cycle start notes" {
		t.Fatalf("expected the notes the body omits kept, got %q", saved.Notes)
	}
}

func TestPatchDayRefusesAnInvalidValueAndKeepsTheDay(t *testing.T) {
	fixture := newPatchMergeFixture(t, "patch-merge-invalid@example.com")

	if status := fixture.sendJSON(t, http.MethodPatch, `{"mood":9}`); status != http.StatusBadRequest {
		t.Fatalf("PATCH {\"mood\":9}: status %d, want 400", status)
	}
	if saved := fixture.stored(t); saved.Mood != 0 || !saved.CycleStart || saved.Notes != "cycle start notes" {
		t.Fatalf("expected the refused write to leave the day as stored, got mood=%d cycle_start=%v notes=%q", saved.Mood, saved.CycleStart, saved.Notes)
	}
}

func TestPatchDayOnADayWithoutARowStartsTheOmittedFieldsEmpty(t *testing.T) {
	fixture := newPatchMergeFixture(t, "patch-merge-new-day@example.com")
	fixture.day = fixture.day.AddDate(0, 0, 10)
	fixture.path = "/api/v1/days/" + fixture.day.Format("2006-01-02")

	if status := fixture.sendJSON(t, http.MethodPatch, `{"notes":"first"}`); status != http.StatusOK {
		t.Fatalf("PATCH: status %d, want 200", status)
	}
	saved := fixture.stored(t)
	if saved.ID == 0 || saved.Notes != "first" || saved.IsPeriod || saved.Mood != 0 || saved.BBT != nil {
		t.Fatalf("expected a new day holding only the stated notes, got id=%d notes=%q is_period=%v mood=%d bbt=%v", saved.ID, saved.Notes, saved.IsPeriod, saved.Mood, saved.BBT)
	}
}

// TestPatchDayFormBodyChangesOnlyThePostedFields covers the form transport: a
// field not posted — an unchecked period checkbox posts nothing — changes
// nothing, and a field the account hides is not read even when posted.
func TestPatchDayFormBodyChangesOnlyThePostedFields(t *testing.T) {
	fixture := newPatchMergeFixture(t, "patch-merge-form@example.com")
	if err := fixture.database.Model(&models.User{}).Where("id = ?", fixture.user.ID).Update("hide_notes_field", true).Error; err != nil {
		t.Fatalf("hide notes: %v", err)
	}

	request := httptest.NewRequest(http.MethodPatch, fixture.path, strings.NewReader("mood=4&notes=overwritten"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Cookie", fixture.authCookie)
	response := mustAppResponse(t, fixture.app, request)
	assertStatusCode(t, response, http.StatusOK)

	saved := fixture.stored(t)
	if saved.Mood != 4 {
		t.Fatalf("expected the posted mood, got %d", saved.Mood)
	}
	if !saved.IsPeriod || !saved.CycleStart || saved.Flow != models.FlowHeavy || saved.BBT == nil {
		t.Fatalf("expected fields not posted kept, got is_period=%v cycle_start=%v flow=%q bbt=%v", saved.IsPeriod, saved.CycleStart, saved.Flow, saved.BBT)
	}
	if saved.Notes != "cycle start notes" {
		t.Fatalf("expected the hidden notes field kept, got %q", saved.Notes)
	}
}

// TestPatchDayAcknowledgesThePeriodTipOnAStoredPeriodDay: the period tip is
// acknowledged on the day as written. A partial write that leaves is_period
// out of the body still saves a period day here, so its ack_period_tip counts.
func TestPatchDayAcknowledgesThePeriodTipOnAStoredPeriodDay(t *testing.T) {
	fixture := newPatchMergeFixture(t, "patch-merge-period-tip@example.com")
	if err := fixture.database.Model(&models.User{}).Where("id = ?", fixture.user.ID).Update("shown_period_tip", false).Error; err != nil {
		t.Fatalf("reset period tip: %v", err)
	}

	request := httptest.NewRequest(http.MethodPatch, fixture.path, strings.NewReader("mood=3&ack_period_tip=true"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Cookie", fixture.authCookie)
	response := mustAppResponse(t, fixture.app, request)
	assertStatusCode(t, response, http.StatusOK)

	if saved := fixture.stored(t); !saved.IsPeriod || saved.Mood != 3 {
		t.Fatalf("expected the stored period day kept with the posted mood, got is_period=%v mood=%d", saved.IsPeriod, saved.Mood)
	}
	var user models.User
	if err := fixture.database.Select("shown_period_tip").Where("id = ?", fixture.user.ID).First(&user).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	if !user.ShownPeriodTip {
		t.Fatal("expected ack_period_tip on a partial write of a period day to record the acknowledgement")
	}
}

func TestPatchDayMultipartBodyChangesOnlyThePostedFields(t *testing.T) {
	fixture := newPatchMergeFixture(t, "patch-merge-multipart@example.com")

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("mood", "5"); err != nil {
		t.Fatalf("write field: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	request := httptest.NewRequest(http.MethodPatch, fixture.path, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Cookie", fixture.authCookie)
	response := mustAppResponse(t, fixture.app, request)
	assertStatusCode(t, response, http.StatusOK)

	saved := fixture.stored(t)
	if saved.Mood != 5 || !saved.CycleStart || saved.Notes != "cycle start notes" {
		t.Fatalf("expected only the posted mood changed, got mood=%d cycle_start=%v notes=%q", saved.Mood, saved.CycleStart, saved.Notes)
	}
}
