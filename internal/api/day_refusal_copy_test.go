package api

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
)

// The "invalid date" refusal is not only a save refusal: the calendar day panel
// and GET /api/v1/days/{date} answer it for a read, and DELETE answers it for a
// delete. The copy behind it therefore names the day and its values, never the
// save.

// TestDayPanelAndReadRouteShowTheInvalidDayCopyForAMalformedDate drives the two
// htmx read paths that answer the date refusal and reads the fragment the way
// the page shows it.
func TestDayPanelAndReadRouteShowTheInvalidDayCopyForAMalformedDate(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "day-invalid-copy-read@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	want := template.HTMLEscapeString(englishCopy(t, "dashboard.error.invalid_day_entry"))

	for _, path := range []string{"/calendar/day/2026-13-45", "/api/v1/days/2026-13-45"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set("Cookie", authCookie)
			request.Header.Set("HX-Request", "true")

			response, err := app.Test(request, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("GET %s failed: %v", path, err)
			}
			defer func() { _ = response.Body.Close() }()

			assertStatusCode(t, response, http.StatusBadRequest)
			body := mustReadBodyString(t, response.Body)
			if !strings.Contains(body, `class="status-error"`) {
				t.Fatalf("GET %s: expected the shared status fragment, got %s", path, body)
			}
			if !strings.Contains(body, want) {
				t.Fatalf("GET %s: fragment lacks the localized invalid-day copy %q: %s", path, want, body)
			}
		})
	}
}

// TestInvalidDayCopyMakesNoSaveClaimInAnyLocale holds the copy of the shared
// invalid-day key off the locale's own save wording. The wording is not listed
// here: each locale's label for the day's Save button supplies it, and its first
// four letters are the stem every inflection of that verb shares (save and
// saved, speichern and gespeichert, guardar, enregistrer and enregistrée).
func TestInvalidDayCopyMakesNoSaveClaimInAnyLocale(t *testing.T) {
	t.Parallel()

	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("init i18n manager: %v", err)
	}
	languages := manager.SupportedLanguages()
	if len(languages) < 6 {
		t.Fatalf("expected at least the six shipped locales, got %v", languages)
	}
	for _, language := range languages {
		t.Run(language, func(t *testing.T) {
			messages := manager.Messages(language)
			copyText := strings.ToLower(strings.TrimSpace(messages["dashboard.error.invalid_day_entry"]))
			if copyText == "" {
				t.Fatalf("locale %q defines no dashboard.error.invalid_day_entry", language)
			}
			label := []rune(strings.ToLower(strings.TrimSpace(messages["dashboard.save_day"])))
			if len(label) < 4 {
				t.Fatalf("locale %q save label %q is too short to derive its save stem", language, string(label))
			}
			if stem := string(label[:4]); strings.Contains(copyText, stem) {
				t.Errorf("locale %q: the invalid-day copy %q uses the save wording %q; a read or a delete shows it too", language, copyText, stem)
			}
		})
	}
}
