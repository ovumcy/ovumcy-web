package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestRegisterStoresTheRegistrationLanguageOnTheAccount(t *testing.T) {
	cases := []struct {
		name   string
		cookie string
		want   string
	}{
		{name: "supported language cookie", cookie: "ru", want: "ru"},
		{name: "unsupported language cookie", cookie: "xx", want: ""},
		{name: "no language named", cookie: "", want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app, database := newOnboardingTestApp(t)
			email := "register-language@example.com"

			request := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(url.Values{
				"email":            {email},
				"password":         {"StrongPass1"},
				"confirm_password": {"StrongPass1"},
				"consent":          {"true"},
			}.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("Accept", "application/json")
			if tc.cookie != "" {
				request.Header.Set("Cookie", languageCookieName+"="+tc.cookie)
			}

			response := mustAppResponse(t, app, request)
			assertStatusCode(t, response, http.StatusCreated)

			var stored models.User
			if err := database.Where("email = ?", email).First(&stored).Error; err != nil {
				t.Fatalf("load registered user: %v", err)
			}
			if stored.InterfaceLanguage != tc.want {
				t.Fatalf("expected interface_language %q right after registration, got %q", tc.want, stored.InterfaceLanguage)
			}
		})
	}
}
