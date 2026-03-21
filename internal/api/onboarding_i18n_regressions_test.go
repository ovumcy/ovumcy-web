package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

func TestOnboardingDateInputUsesCurrentLanguage(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "onboarding-lang@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/onboarding", nil)
	request.Header.Set("Cookie", authCookie+"; ovumcy_lang=en")
	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("onboarding request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected onboarding status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read onboarding body: %v", err)
	}

	pattern := regexp.MustCompile(`(?s)data-date-field-id="last-period-start".*?<input[^>]*id="last-period-start"[^>]*lang="en".*?aria-label="Day".*?aria-label="Month".*?aria-label="Year"`)
	if !pattern.Match(body) {
		t.Fatalf("expected onboarding date field #last-period-start to render english segmented accessibility labels")
	}
}

func TestOnboardingDateFieldUsesRussianLabels(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "onboarding-lang-ru@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/onboarding", nil)
	request.Header.Set("Cookie", authCookie+"; ovumcy_lang=ru")
	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("onboarding request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected onboarding status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read onboarding body: %v", err)
	}

	if !regexp.MustCompile(`(?s)data-date-field-id="last-period-start".*?aria-label="День".*?aria-label="Месяц".*?aria-label="Год"`).Match(body) {
		t.Fatalf("expected russian onboarding segmented date labels")
	}
	if !regexp.MustCompile(`data-yesterday-label="Вчера"`).Match(body) {
		t.Fatalf("expected russian onboarding quick-pick yesterday label")
	}
	if !regexp.MustCompile(`data-two-days-ago-label="2 дня назад"`).Match(body) {
		t.Fatalf("expected russian onboarding quick-pick two-days-ago label")
	}
}

func TestOnboardingDateFieldUsesSpanishLabels(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "onboarding-lang-es@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/onboarding", nil)
	request.Header.Set("Cookie", authCookie+"; ovumcy_lang=es")
	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("onboarding request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected onboarding status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read onboarding body: %v", err)
	}

	if !regexp.MustCompile(`(?s)data-date-field-id="last-period-start".*?aria-label="Día".*?aria-label="Mes".*?aria-label="Año"`).Match(body) {
		t.Fatalf("expected spanish onboarding segmented date labels")
	}
}

func TestOnboardingDateFieldUsesFrenchLabels(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "onboarding-lang-fr@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/onboarding", nil)
	request.Header.Set("Cookie", authCookie+"; ovumcy_lang=fr")
	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("onboarding request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected onboarding status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read onboarding body: %v", err)
	}

	if !regexp.MustCompile(`(?s)data-date-field-id="last-period-start".*?aria-label="Jour".*?aria-label="Mois".*?aria-label="Année"`).Match(body) {
		t.Fatalf("expected french onboarding segmented date labels")
	}
	if !regexp.MustCompile(`data-yesterday-label="Hier"`).Match(body) {
		t.Fatalf("expected french onboarding quick-pick yesterday label")
	}
	if !regexp.MustCompile(`data-two-days-ago-label="Il y a 2 jours"`).Match(body) {
		t.Fatalf("expected french onboarding quick-pick two-days-ago label")
	}
}
