package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLanguageSwitchSetsCookieAndRendersLocalizedLogin(t *testing.T) {
	tests := []struct {
		name               string
		switchPath         string
		expectedCookie     string
		expectedHTMLLang   string
		expectedTitle      string
		expectedHelperText string
	}{
		{
			name:               "english",
			switchPath:         "/lang/en?next=/login",
			expectedCookie:     "en",
			expectedHTMLLang:   "en",
			expectedTitle:      "Stay signed in for 30 days",
			expectedHelperText: "only until you close the browser",
		},
		{
			name:               "russian",
			switchPath:         "/lang/ru?next=/login",
			expectedCookie:     "ru",
			expectedHTMLLang:   "ru",
			expectedTitle:      "Оставаться в системе 30 дней",
			expectedHelperText: "только до закрытия браузера",
		},
		{
			name:               "spanish",
			switchPath:         "/lang/es?next=/login",
			expectedCookie:     "es",
			expectedHTMLLang:   "es",
			expectedTitle:      "Mantener la sesión iniciada durante 30 días",
			expectedHelperText: "solo hasta que cierres el navegador",
		},
		{
			name:               "french",
			switchPath:         "/lang/fr?next=/login",
			expectedCookie:     "fr",
			expectedHTMLLang:   "fr",
			expectedTitle:      "Rester connecté(e) pendant 30 jours",
			expectedHelperText: "fermeture du navigateur",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			app, _ := newOnboardingTestApp(t)

			switchRequest := httptest.NewRequest(http.MethodGet, testCase.switchPath, nil)
			switchRequest.Header.Set("Accept-Language", "en")
			switchResponse, err := app.Test(switchRequest, -1)
			if err != nil {
				t.Fatalf("switch language request failed: %v", err)
			}
			defer switchResponse.Body.Close()

			if switchResponse.StatusCode != http.StatusSeeOther {
				t.Fatalf("expected status 303, got %d", switchResponse.StatusCode)
			}
			if location := switchResponse.Header.Get("Location"); location != "/login" {
				t.Fatalf("expected redirect to /login, got %q", location)
			}

			languageCookie := responseCookieValue(switchResponse.Cookies(), "ovumcy_lang")
			if languageCookie != testCase.expectedCookie {
				t.Fatalf("expected ovumcy_lang cookie value %q, got %q", testCase.expectedCookie, languageCookie)
			}

			loginRequest := httptest.NewRequest(http.MethodGet, "/login", nil)
			loginRequest.Header.Set("Cookie", "ovumcy_lang="+languageCookie)
			loginResponse, err := app.Test(loginRequest, -1)
			if err != nil {
				t.Fatalf("localized login request failed: %v", err)
			}
			defer loginResponse.Body.Close()

			loginBody, err := io.ReadAll(loginResponse.Body)
			if err != nil {
				t.Fatalf("read localized login body: %v", err)
			}
			rendered := string(loginBody)
			if !strings.Contains(rendered, `<html lang="`+testCase.expectedHTMLLang+`"`) {
				t.Fatalf("expected login page html lang to be %q", testCase.expectedHTMLLang)
			}
			if !strings.Contains(rendered, testCase.expectedTitle) {
				t.Fatalf("expected localized remember-me control on login form, got %q", rendered)
			}
			if !strings.Contains(rendered, testCase.expectedHelperText) {
				t.Fatalf("expected localized remember-me helper text on login form, got %q", rendered)
			}
		})
	}
}
