package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestOnboardingStep2SanitizesOutOfRangeAndIncompatibleValues(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "step2-validation@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	invalidCycleForm := url.Values{
		"cycle_length":  {"14"},
		"period_length": {"5"},
	}
	invalidCycleRequest := httptest.NewRequest(http.MethodPost, "/onboarding/step2", strings.NewReader(invalidCycleForm.Encode()))
	invalidCycleRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	invalidCycleRequest.Header.Set("HX-Request", "true")
	invalidCycleRequest.Header.Set("Cookie", authCookie)

	invalidCycleResponse, err := app.Test(invalidCycleRequest, -1)
	if err != nil {
		t.Fatalf("invalid cycle request failed: %v", err)
	}
	defer invalidCycleResponse.Body.Close()
	if invalidCycleResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("expected invalid cycle status 204, got %d", invalidCycleResponse.StatusCode)
	}

	var updated models.User
	if err := database.First(&updated, user.ID).Error; err != nil {
		t.Fatalf("load updated user: %v", err)
	}
	if updated.CycleLength != 15 {
		t.Fatalf("expected clamped cycle length 15, got %d", updated.CycleLength)
	}
	if updated.PeriodLength != 5 {
		t.Fatalf("expected sanitized period length 5, got %d", updated.PeriodLength)
	}

	incompatibleForm := url.Values{
		"cycle_length":  {"21"},
		"period_length": {"14"},
	}
	incompatibleRequest := httptest.NewRequest(http.MethodPost, "/onboarding/step2", strings.NewReader(incompatibleForm.Encode()))
	incompatibleRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	incompatibleRequest.Header.Set("HX-Request", "true")
	incompatibleRequest.Header.Set("Cookie", authCookie)

	incompatibleResponse, err := app.Test(incompatibleRequest, -1)
	if err != nil {
		t.Fatalf("incompatible request failed: %v", err)
	}
	defer incompatibleResponse.Body.Close()
	if incompatibleResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("expected incompatible values status 204, got %d", incompatibleResponse.StatusCode)
	}

	if err := database.First(&updated, user.ID).Error; err != nil {
		t.Fatalf("reload updated user: %v", err)
	}
	if updated.CycleLength != 21 {
		t.Fatalf("expected persisted cycle length 21, got %d", updated.CycleLength)
	}
	if updated.PeriodLength != 11 {
		t.Fatalf("expected adjusted period length 11 for compatibility, got %d", updated.PeriodLength)
	}
}

func TestOnboardingStep2IgnoresUnexpectedPeriodEndInput(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "step2-extra-period-end@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	form := url.Values{
		"cycle_length":      {"28"},
		"period_length":     {"8"},
		"last_period_start": {"2026-02-10"},
		"period_end":        {"2026-02-15"},
	}
	request := httptest.NewRequest(http.MethodPost, "/onboarding/step2", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("unexpected period-end request failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", response.StatusCode)
	}

	var updated models.User
	if err := database.First(&updated, user.ID).Error; err != nil {
		t.Fatalf("load updated user: %v", err)
	}
	if updated.PeriodLength != 8 {
		t.Fatalf("expected period length to follow slider value 8, got %d", updated.PeriodLength)
	}
}

func TestOnboardingStep2SanitizesSliderValuesEvenWhenUnexpectedPeriodEndIsPresent(t *testing.T) {
	cases := []struct {
		name              string
		sliderPeriodValue string
		cycleLength       string
		wantPeriodLength  int
	}{
		{
			name:              "clamp to max period length",
			sliderPeriodValue: "20",
			cycleLength:       "28",
			wantPeriodLength:  14,
		},
		{
			name:              "compatibility floor adjusts period length",
			sliderPeriodValue: "10",
			cycleLength:       "15",
			wantPeriodLength:  5,
		},
		{
			name:              "invalid negative value clamps to min",
			sliderPeriodValue: "-4",
			cycleLength:       "28",
			wantPeriodLength:  1,
		},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			app, database := newOnboardingTestApp(t)
			user := createOnboardingTestUser(t, database, "step2-extra-input-sanitize-"+strings.ReplaceAll(testCase.name, " ", "-")+"@example.com", "StrongPass1", false)
			authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

			form := url.Values{
				"cycle_length":      {testCase.cycleLength},
				"period_length":     {testCase.sliderPeriodValue},
				"last_period_start": {"2026-02-10"},
				"period_end":        {"2026-02-25"},
			}
			request := httptest.NewRequest(http.MethodPost, "/onboarding/step2", strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("HX-Request", "true")
			request.Header.Set("Cookie", authCookie)

			response, err := app.Test(request, -1)
			if err != nil {
				t.Fatalf("sanitize request failed: %v", err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusNoContent {
				t.Fatalf("expected status 204, got %d", response.StatusCode)
			}

			var updated models.User
			if err := database.First(&updated, user.ID).Error; err != nil {
				t.Fatalf("load updated user: %v", err)
			}
			if updated.PeriodLength != testCase.wantPeriodLength {
				t.Fatalf("expected period length %d, got %d", testCase.wantPeriodLength, updated.PeriodLength)
			}
		})
	}
}
