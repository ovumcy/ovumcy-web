package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The two request shapes v2.0.0 stopped accepting on the stable /api/v1 surface,
// pinned side by side against the shapes that replaced them (CONTRIBUTING.md →
// API Stability Contract). Each pair is deliberately divergent: the v1.9.2 body
// must be refused by a key that names the break, and the current body must still
// succeed, so neither half can be made green by undoing the other.

func forgotPasswordJSONRequest(body string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/v1/password-resets", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	return request
}

// TestRecoveryResetNamesTheV1BodyItNoLongerAccepts covers API-1. Until v2.0.0
// POST /api/v1/password-resets took (email, recovery_code); the account password
// joined it as a required operand, because a recovery code substitutes for the
// second factor and never for the first. A client still sending the old body was
// told its recovery code was invalid, which is both false and unactionable — the
// code is fine, the contract moved. It is now told which member the major
// version added, before any account is read.
func TestRecoveryResetNamesTheV1BodyItNoLongerAccepts(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "recovery-v1-body@example.com", "StrongPass1", true)
	recoveryCode := mustSetRecoveryCodeForUser(t, database, user.ID)

	t.Run("v1.9.2-body-is-refused-by-name", func(t *testing.T) {
		request := forgotPasswordJSONRequest(mustJSONBody(t, map[string]string{
			"email":         user.Email,
			"recovery_code": recoveryCode,
		}))

		response, err := app.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("forgot-password request failed: %v", err)
		}
		defer func() { _ = response.Body.Close() }()

		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected status 400 for the v1.9.2 body, got %d", response.StatusCode)
		}
		if key := errorKeyFromEnvelope(t, response.Body); key != "recovery reset requires the account password" {
			t.Fatalf("expected the break to be named, got error key %q", key)
		}
		if resetCookie := responseCookie(response.Cookies(), resetPasswordCookieName); resetCookie != nil && strings.TrimSpace(resetCookie.Value) != "" {
			t.Fatalf("the refused v1.9.2 body minted a reset token")
		}
		if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
			t.Fatalf("the refused v1.9.2 body issued an auth session")
		}
	})

	t.Run("current-body-still-succeeds", func(t *testing.T) {
		request := forgotPasswordJSONRequest(mustJSONBody(t, map[string]string{
			"email":         user.Email,
			"recovery_code": recoveryCode,
			"password":      "StrongPass1",
		}))

		response, err := app.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("forgot-password request failed: %v", err)
		}
		defer func() { _ = response.Body.Close() }()

		if response.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200 for the current body, got %d", response.StatusCode)
		}
		resetCookie := responseCookie(response.Cookies(), resetPasswordCookieName)
		if resetCookie == nil || strings.TrimSpace(resetCookie.Value) == "" {
			t.Fatalf("expected a reset-password cookie for the current body")
		}
	})

	// An empty password is a submitted credential, not a stale client, so it
	// stays inside the enumeration-safe collapse the constitution requires
	// (docs/SECURITY_INVARIANTS.md → Password recovery) rather than borrowing
	// the key above.
	t.Run("an-empty-password-stays-a-credential-refusal", func(t *testing.T) {
		request := forgotPasswordJSONRequest(mustJSONBody(t, map[string]string{
			"email":         user.Email,
			"recovery_code": recoveryCode,
			"password":      "",
		}))

		response, err := app.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("forgot-password request failed: %v", err)
		}
		defer func() { _ = response.Body.Close() }()

		if key := errorKeyFromEnvelope(t, response.Body); key == "recovery reset requires the account password" {
			t.Fatalf("an empty password was answered as a stale-client refusal")
		}
	})

	// Form encoding cannot tell an omitted field from an empty one, so the form
	// transport keeps the uniform credential refusal instead of guessing.
	t.Run("a-form-body-without-password-stays-a-credential-refusal", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/password-resets", strings.NewReader(url.Values{
			"email":         {user.Email},
			"recovery_code": {recoveryCode},
		}.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Accept", "application/json")

		response, err := app.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("forgot-password request failed: %v", err)
		}
		defer func() { _ = response.Body.Close() }()

		if key := errorKeyFromEnvelope(t, response.Body); key == "recovery reset requires the account password" {
			t.Fatalf("the form transport answered a question it cannot ask")
		}
	})
}

// TestOnboardingStep2NamesTheAgeGroupItNoLongerAccepts covers API-2. Until
// v2.0.0 step 2 collected `age_group`; the step stopped asking for it, and the
// column is written by PATCH /api/v1/users/current/cycle alone. A client still
// submitting it received 200 with the field silently dropped — a removal that
// reads as a successful save is indistinguishable from one that worked.
func TestOnboardingStep2NamesTheAgeGroupItNoLongerAccepts(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "step2-v1-body@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	t.Run("v1.9.2-body-is-refused-by-name", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/steps/2", strings.NewReader(mustJSONBody(t, map[string]any{
			"cycle_length":  31,
			"period_length": 6,
			"age_group":     models.AgeGroup40To45,
		})))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Cookie", authCookie)

		response, err := app.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("step2 request failed: %v", err)
		}
		defer func() { _ = response.Body.Close() }()

		if response.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected status 400 for the v1.9.2 body, got %d", response.StatusCode)
		}
		if key := errorKeyFromEnvelope(t, response.Body); key != "onboarding does not accept an age group" {
			t.Fatalf("expected the break to be named, got error key %q", key)
		}

		var stored models.User
		if err := database.First(&stored, user.ID).Error; err != nil {
			t.Fatalf("load user: %v", err)
		}
		if stored.AgeGroup == models.AgeGroup40To45 {
			t.Fatalf("the refused body wrote the age group it carried")
		}
		// 31/6 is chosen away from the account's seeded defaults, so a stored
		// value equal to it can only have come from the refused body.
		if stored.CycleLength == 31 || stored.PeriodLength == 6 {
			t.Fatalf("the refused body saved the step it was refused for")
		}
	})

	// The form transport can ask the same question, and must answer it the same
	// way: a class refused on one transport and accepted on the other is the
	// silent save again, one spelling over.
	t.Run("the-form-transport-refuses-it-too", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/steps/2", strings.NewReader(url.Values{
			"cycle_length":  {"28"},
			"period_length": {"5"},
			"age_group":     {models.AgeGroup40To45},
		}.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Cookie", authCookie)

		response, err := app.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("step2 request failed: %v", err)
		}
		defer func() { _ = response.Body.Close() }()

		if key := errorKeyFromEnvelope(t, response.Body); key != "onboarding does not accept an age group" {
			t.Fatalf("expected the break to be named on the form transport, got error key %q", key)
		}
	})

	t.Run("current-body-still-succeeds", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/steps/2", strings.NewReader(mustJSONBody(t, map[string]any{
			"cycle_length":  28,
			"period_length": 5,
			"usage_goal":    models.UsageGoalAvoid,
		})))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json")
		request.Header.Set("Cookie", authCookie)

		response, err := app.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("step2 request failed: %v", err)
		}
		defer func() { _ = response.Body.Close() }()

		if response.StatusCode != http.StatusOK {
			t.Fatalf("expected status 200 for the current body, got %d", response.StatusCode)
		}

		var stored models.User
		if err := database.First(&stored, user.ID).Error; err != nil {
			t.Fatalf("load user: %v", err)
		}
		if stored.CycleLength != 28 || stored.PeriodLength != 5 {
			t.Fatalf("expected the current body to save, got cycle %d period %d", stored.CycleLength, stored.PeriodLength)
		}
		if stored.UsageGoal != models.UsageGoalAvoid {
			t.Fatalf("expected the submitted usage goal, got %q", stored.UsageGoal)
		}
	})
}

func mustJSONBody(t *testing.T, payload any) string {
	t.Helper()

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode request body: %v", err)
	}
	return string(encoded)
}

func errorKeyFromEnvelope(t *testing.T, body io.Reader) string {
	t.Helper()

	raw, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	envelope := struct {
		Error string `json:"error"`
	}{}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decode error envelope %q: %v", string(raw), err)
	}
	return envelope.Error
}
