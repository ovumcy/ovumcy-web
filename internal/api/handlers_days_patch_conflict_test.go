package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

// Two sessions on one account: the second writes the day while the first
// still shows what it rendered. The first one's partial write names only what
// it changed, each with the value it based the edit on.

func sendDayWriteForConflictTest(t *testing.T, app *fiber.App, method string, cookie string, contentType string, body string, prepare func(*http.Request)) *http.Response {
	t.Helper()
	request := httptest.NewRequest(method, "/api/v1/days/2026-02-10", bytes.NewReader([]byte(body)))
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Cookie", cookie)
	if prepare != nil {
		prepare(request)
	}
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("%s request failed: %v", method, err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func storedDayNotesForConflictTest(t *testing.T, database *gorm.DB, userID uint) (string, int) {
	t.Helper()
	day, err := services.ParseDayDate("2026-02-10", time.UTC)
	if err != nil {
		t.Fatalf("parse day: %v", err)
	}
	entry, err := fetchLogByDateForTest(database, userID, day, time.UTC)
	if err != nil {
		t.Fatalf("fetch day: %v", err)
	}
	return entry.Notes, entry.Mood
}

func TestPatchDayRefusesAFieldChangedByAnotherSessionSinceItsBase(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "patch-day-conflict@example.com", "StrongPass1", true)
	cookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	// Both tabs rendered the day with notes "rendered"; the other session then
	// wrote "remote".
	if response := sendDayWriteForConflictTest(t, app, http.MethodPut, cookie, fiber.MIMEApplicationJSON, `{"notes":"rendered","mood":2}`, nil); response.StatusCode != http.StatusOK {
		t.Fatalf("seed: expected 200, got %d", response.StatusCode)
	}
	if response := sendDayWriteForConflictTest(t, app, http.MethodPatch, cookie, fiber.MIMEApplicationJSON, `{"notes":"remote","base_notes":"rendered"}`, nil); response.StatusCode != http.StatusOK {
		t.Fatalf("a write based on the stored value: expected 200, got %d", response.StatusCode)
	}

	stale := sendDayWriteForConflictTest(t, app, http.MethodPatch, cookie, fiber.MIMEApplicationJSON, `{"notes":"mine","base_notes":"rendered"}`, nil)
	if stale.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for notes changed elsewhere, got %d", stale.StatusCode)
	}
	if key := readAPIError(t, stale.Body); key != "day changed elsewhere" {
		t.Fatalf("expected the day-changed-elsewhere key, got %q", key)
	}
	if notes, _ := storedDayNotesForConflictTest(t, database, user.ID); notes != "remote" {
		t.Fatalf("expected the refused write to keep the other session's note, got %q", notes)
	}

	// The same stale tab changing only the mood: the note it does not name is
	// not its subject, so nothing conflicts and the note survives.
	moodOnly := sendDayWriteForConflictTest(t, app, http.MethodPatch, cookie, fiber.MIMEApplicationJSON, `{"mood":4,"base_mood":2}`, nil)
	if moodOnly.StatusCode != http.StatusOK {
		t.Fatalf("expected a write of an unchanged field to save, got %d", moodOnly.StatusCode)
	}
	if notes, mood := storedDayNotesForConflictTest(t, database, user.ID); notes != "remote" || mood != 4 {
		t.Fatalf("expected mood 4 and the remote note kept, got mood=%d notes=%q", mood, notes)
	}

	// Re-sending a write already committed: the stored value equals the value
	// written, so it is not a conflict.
	if resent := sendDayWriteForConflictTest(t, app, http.MethodPatch, cookie, fiber.MIMEApplicationJSON, `{"mood":4,"base_mood":2}`, nil); resent.StatusCode != http.StatusOK {
		t.Fatalf("expected an idempotent re-send to save, got %d", resent.StatusCode)
	}
}

func TestPatchDayConflictAnswersTheDayFormWithItsLocalizedKey(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "patch-day-conflict-form@example.com", "StrongPass1", true)
	cookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	if response := sendDayWriteForConflictTest(t, app, http.MethodPut, cookie, fiber.MIMEApplicationJSON, `{"notes":"remote"}`, nil); response.StatusCode != http.StatusOK {
		t.Fatalf("seed: expected 200, got %d", response.StatusCode)
	}

	form := url.Values{"notes": {"mine"}, "base_notes": {"rendered"}, "is_period": {"false"}, "base_is_period": {"false"}}
	response := sendDayWriteForConflictTest(t, app, http.MethodPatch, cookie, fiber.MIMEApplicationForm, form.Encode(), func(request *http.Request) {
		request.Header.Set("HX-Request", "true")
		bindDayWriteForTest(t, request, user.ID)
	})
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for the day form, got %d", response.StatusCode)
	}
	if body := mustReadBodyString(t, response.Body); !strings.Contains(body, `data-flash-key="daylog.save_conflict"`) {
		t.Fatalf("expected the conflict fragment to carry its stable key, got %q", body)
	}
}

func TestPatchDayRefusesAnUnreadableBase(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "patch-day-bad-base@example.com", "StrongPass1", true)
	cookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	for name, body := range map[string]string{
		"json": `{"mood":4,"base_mood":"four"}`,
		"form": url.Values{"mood": {"4"}, "base_mood": {"four"}}.Encode(),
	} {
		contentType := fiber.MIMEApplicationJSON
		if name == "form" {
			contentType = fiber.MIMEApplicationForm
		}
		if response := sendDayWriteForConflictTest(t, app, http.MethodPatch, cookie, contentType, body, nil); response.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: expected 400 for an unreadable base, got %d", name, response.StatusCode)
		}
	}
}

// The full write states the whole day and takes no base: a base it carries is
// not read, and PUT answers as it always has.
func TestPutDayIgnoresABase(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "put-day-ignores-base@example.com", "StrongPass1", true)
	cookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	if response := sendDayWriteForConflictTest(t, app, http.MethodPut, cookie, fiber.MIMEApplicationJSON, `{"notes":"remote"}`, nil); response.StatusCode != http.StatusOK {
		t.Fatalf("seed: expected 200, got %d", response.StatusCode)
	}
	response := sendDayWriteForConflictTest(t, app, http.MethodPut, cookie, fiber.MIMEApplicationJSON, `{"notes":"mine","base_notes":"stale","base_mood":"four"}`, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected PUT to ignore a base, got %d", response.StatusCode)
	}
	if notes, _ := storedDayNotesForConflictTest(t, database, user.ID); notes != "mine" {
		t.Fatalf("expected the full write to replace the note, got %q", notes)
	}
}
