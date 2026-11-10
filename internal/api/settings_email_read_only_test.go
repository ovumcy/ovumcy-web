package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestSettingsWritesNeverChangeTheAccountEmail sends an email field alongside a
// legitimate change to every signed-in settings write that updates the account
// row, in both body encodings. The legitimate field landing is the anchor that
// the request was accepted and bound; the stored email staying put, and the old
// address still signing in, is the read-only claim.
func TestSettingsWritesNeverChangeTheAccountEmail(t *testing.T) {
	const injectedEmail = "taken-over@example.com"

	cases := []struct {
		name    string
		method  string
		path    string
		fields  map[string]any
		changed func(models.User) bool
	}{
		{"profile", http.MethodPatch, "/api/v1/users/current/profile",
			map[string]any{"display_name": "Maya"},
			func(u models.User) bool { return u.DisplayName == "Maya" }},
		{"interface", http.MethodPatch, "/api/v1/users/current/interface",
			map[string]any{"language": "de", "theme": "dark"},
			func(u models.User) bool { return u.InterfaceLanguage == "de" }},
		{"timezone", http.MethodPost, "/api/v1/users/current/timezone",
			map[string]any{"timezone": "Europe/Berlin"},
			func(u models.User) bool { return u.Timezone == "Europe/Berlin" }},
		{"reminders", http.MethodPatch, "/api/v1/users/current/reminders",
			map[string]any{"reminder_lead_days": 5},
			func(u models.User) bool { return u.ReminderLeadDays == 5 }},
	}

	for _, tc := range cases {
		for _, encoding := range []string{"form", "json"} {
			t.Run(tc.name+" "+encoding, func(t *testing.T) {
				app, database := newOnboardingTestApp(t)
				email := tc.name + "-" + encoding + "-email-owner@example.com"
				user := createOnboardingTestUser(t, database, email, "StrongPass1", true)
				authCookie := loginAndExtractAuthCookie(t, app, email, "StrongPass1")

				var request *http.Request
				if encoding == "json" {
					body := map[string]any{"email": injectedEmail}
					for key, value := range tc.fields {
						body[key] = value
					}
					encoded, err := json.Marshal(body)
					if err != nil {
						t.Fatalf("marshal body: %v", err)
					}
					request = httptest.NewRequest(tc.method, tc.path, strings.NewReader(string(encoded)))
					request.Header.Set("Content-Type", "application/json")
					request.Header.Set("Accept", "application/json")
				} else {
					form := url.Values{"email": {injectedEmail}}
					for key, value := range tc.fields {
						form.Set(key, fmt.Sprint(value))
					}
					request = httptest.NewRequest(tc.method, tc.path, strings.NewReader(form.Encode()))
					request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				}
				request.Header.Set("Cookie", authCookie)

				response := mustAppResponse(t, app, request)
				if response.StatusCode >= http.StatusBadRequest {
					t.Fatalf("expected the settings write to be accepted, got %d", response.StatusCode)
				}

				stored := models.User{}
				if err := database.First(&stored, user.ID).Error; err != nil {
					t.Fatalf("reload user: %v", err)
				}
				if !tc.changed(stored) {
					t.Fatalf("expected the legitimate %s field to be persisted, got %+v", tc.name, tc.fields)
				}
				if stored.Email != email {
					t.Fatalf("a settings write changed the account email: want %q, got %q", email, stored.Email)
				}
				var injected int64
				if err := database.Model(&models.User{}).Where("email = ?", injectedEmail).Count(&injected).Error; err != nil {
					t.Fatalf("count injected email: %v", err)
				}
				if injected != 0 {
					t.Fatalf("expected no account to carry the submitted email, found %d", injected)
				}

				loginAndExtractAuthCookie(t, app, email, "StrongPass1")
			})
		}
	}
}
