package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/pquerna/otp/totp"
	"gorm.io/gorm"
)

// openAPIResponseSchemaGuardEntry drives one operation's JSON success path
// through the real router and returns the status and raw body it answered.
type openAPIResponseSchemaGuardEntry struct {
	operation string
	drive     func(t *testing.T) (int, []byte)
}

// schemaGuardOwner is a fresh app with one onboarded, signed-in owner. Each
// driver builds its own, so a destructive operation (sign-out, erasure) cannot
// leak into the next one.
type schemaGuardOwner struct {
	app      *fiber.App
	database *gorm.DB
	user     models.User
	email    string
	password string
	cookie   string
}

const (
	schemaGuardEmail    = "schema-guard@example.com"
	schemaGuardPassword = "StrongPass1"
)

func newSchemaGuardOwner(t *testing.T) schemaGuardOwner {
	t.Helper()
	return newSchemaGuardOwnerWithOptions(t, onboardingTestAppOptions{})
}

func newSchemaGuardOwnerWithOptions(t *testing.T, options onboardingTestAppOptions) schemaGuardOwner {
	t.Helper()
	app, database := newOnboardingTestAppWithOptions(t, options)
	user := createOnboardingTestUser(t, database, schemaGuardEmail, schemaGuardPassword, true)
	return schemaGuardOwner{
		app:      app,
		database: database,
		user:     user,
		email:    schemaGuardEmail,
		password: schemaGuardPassword,
		cookie:   loginAndExtractAuthCookie(t, app, schemaGuardEmail, schemaGuardPassword),
	}
}

// newSchemaGuardOnboardingOwner is a signed-in owner who has not finished
// onboarding, for the onboarding steps.
func newSchemaGuardOnboardingOwner(t *testing.T) schemaGuardOwner {
	t.Helper()
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, schemaGuardEmail, schemaGuardPassword, false)
	return schemaGuardOwner{
		app:      app,
		database: database,
		user:     user,
		email:    schemaGuardEmail,
		password: schemaGuardPassword,
		cookie:   loginAndExtractAuthCookie(t, app, schemaGuardEmail, schemaGuardPassword),
	}
}

// send issues a JSON request (a nil payload sends no body) carrying the
// owner's session and returns the status and body.
func (owner schemaGuardOwner) send(t *testing.T, method string, target string, payload any) (int, []byte) {
	t.Helper()
	return sendSchemaGuardJSON(t, owner.app, method, target, owner.cookie, payload)
}

func sendSchemaGuardJSON(t *testing.T, app *fiber.App, method string, target string, cookie string, payload any) (int, []byte) {
	t.Helper()
	response, body := sendSchemaGuardRequest(t, app, method, target, cookie, payload)
	return response.StatusCode, body
}

// sendSchemaGuardForm posts a form body asking for the JSON answer, for the
// operations whose declared request body is form-encoded only.
func sendSchemaGuardForm(t *testing.T, app *fiber.App, method string, target string, cookie string, form url.Values) (int, []byte) {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", fiber.MIMEApplicationForm)
	request.Header.Set("Accept", fiber.MIMEApplicationJSON)
	request.Header.Set("Cookie", cookie)
	return schemaGuardBody(t, mustAppResponse(t, app, request))
}

// schemaGuardBody reads a response a shared fixture helper built.
func schemaGuardBody(t *testing.T, response *http.Response) (int, []byte) {
	t.Helper()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return response.StatusCode, raw
}

func sendSchemaGuardRequest(t *testing.T, app *fiber.App, method string, target string, cookie string, payload any) (*http.Response, []byte) {
	t.Helper()

	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal %s %s payload: %v", method, target, err)
		}
		body = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, target, body)
	if payload != nil {
		request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	}
	request.Header.Set("Accept", fiber.MIMEApplicationJSON)
	if cookie != "" {
		request.Header.Set("Cookie", cookie)
	}
	response := mustAppResponse(t, app, request)
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s %s body: %v", method, target, err)
	}
	return response, raw
}

// requireSchemaGuardStatus fails a setup step that did not answer as expected.
func requireSchemaGuardStatus(t *testing.T, step string, status int, body []byte, want int) {
	t.Helper()
	if status != want {
		t.Fatalf("%s answered %d, want %d; body: %s", step, status, want, body)
	}
}

// openAPIResponseSchemaGuardExemptions names the JSON success operations the
// guard does not drive, each with the reason it cannot run in-process.
var openAPIResponseSchemaGuardExemptions = map[string]string{}

// schemaGuardDay is a recent past calendar day, safely inside every "not in
// the future" check whatever the wall clock says.
func schemaGuardDay(daysAgo int) string {
	return schemaGuardToday.AddDate(0, 0, -daysAgo).Format("2006-01-02")
}

// schemaGuardToday is read once, so a UTC midnight between a seed and the read
// that looks for it cannot move the day under the test.
var schemaGuardToday = time.Now().UTC()

// seedDay stores one period day through the real day write; it carries no
// BBT, so the day's omitempty field is absent from every body built from it.
func (owner schemaGuardOwner) seedDay(t *testing.T, day string) {
	t.Helper()
	status, body := owner.send(t, http.MethodPut, "/api/v1/days/"+day, map[string]any{
		"is_period": true,
		"flow":      "medium",
		"notes":     "schema guard",
	})
	requireSchemaGuardStatus(t, "seed PUT /api/v1/days/"+day, status, body, http.StatusOK)
}

// seedSymptom creates one custom symptom and returns its id.
func (owner schemaGuardOwner) seedSymptom(t *testing.T) uint {
	t.Helper()
	status, body := owner.send(t, http.MethodPost, "/api/v1/symptoms", map[string]any{
		"name":  "Schema guard",
		"icon":  "*",
		"color": "#AABBCC",
	})
	requireSchemaGuardStatus(t, "seed POST /api/v1/symptoms", status, body, http.StatusCreated)
	created := struct {
		ID uint `json:"id"`
	}{}
	if err := json.Unmarshal(body, &created); err != nil || created.ID == 0 {
		t.Fatalf("seed POST /api/v1/symptoms answered no id (%v): %s", err, body)
	}
	return created.ID
}

func openAPIResponseSchemaGuardTable() []openAPIResponseSchemaGuardEntry {
	return []openAPIResponseSchemaGuardEntry{
		{operation: "GET /healthz", drive: func(t *testing.T) (int, []byte) {
			app, _ := newOnboardingTestApp(t)
			return sendSchemaGuardJSON(t, app, http.MethodGet, "/healthz", "", nil)
		}},
		{operation: "GET /readyz", drive: func(t *testing.T) (int, []byte) {
			app, _ := newOnboardingTestApp(t)
			return sendSchemaGuardJSON(t, app, http.MethodGet, "/readyz", "", nil)
		}},
		{operation: "GET /api/v1/days", drive: func(t *testing.T) (int, []byte) {
			owner := newSchemaGuardOwner(t)
			owner.seedDay(t, schemaGuardDay(3))
			owner.seedDay(t, schemaGuardDay(2))
			return owner.send(t, http.MethodGet, "/api/v1/days?from="+schemaGuardDay(10)+"&to="+schemaGuardDay(0), nil)
		}},
		{operation: "GET /api/v1/days/{date}", drive: func(t *testing.T) (int, []byte) {
			owner := newSchemaGuardOwner(t)
			owner.seedDay(t, schemaGuardDay(2))
			return owner.send(t, http.MethodGet, "/api/v1/days/"+schemaGuardDay(2), nil)
		}},
		{operation: "PUT /api/v1/days/{date}", drive: func(t *testing.T) (int, []byte) {
			owner := newSchemaGuardOwner(t)
			symptomID := owner.seedSymptom(t)
			return owner.send(t, http.MethodPut, "/api/v1/days/"+schemaGuardDay(2), map[string]any{
				"is_period":   true,
				"flow":        "light",
				"notes":       "schema guard",
				"symptom_ids": []uint{symptomID},
			})
		}},
		{operation: "DELETE /api/v1/days/{date}", drive: func(t *testing.T) (int, []byte) {
			// Without `source` a programmatic delete answers 204 and no body;
			// the JSON arm is the browser selector's.
			owner := newSchemaGuardOwner(t)
			owner.seedDay(t, schemaGuardDay(2))
			return owner.send(t, http.MethodDelete, "/api/v1/days/"+schemaGuardDay(2)+"?source=dashboard", nil)
		}},
		{operation: "POST /api/v1/users", drive: func(t *testing.T) (int, []byte) {
			app, _ := newOnboardingTestApp(t)
			return sendSchemaGuardJSON(t, app, http.MethodPost, "/api/v1/users", "", map[string]any{
				"email": schemaGuardEmail, "password": schemaGuardPassword, "confirm_password": schemaGuardPassword, "consent": "true",
			})
		}},
		{operation: "POST /api/v1/sessions", drive: func(t *testing.T) (int, []byte) {
			app, database := newOnboardingTestApp(t)
			createOnboardingTestUser(t, database, schemaGuardEmail, schemaGuardPassword, true)
			return sendSchemaGuardJSON(t, app, http.MethodPost, "/api/v1/sessions", "", map[string]any{
				"email": schemaGuardEmail, "password": schemaGuardPassword,
			})
		}},
		{operation: "POST /api/v1/sessions/2fa-challenge", drive: func(t *testing.T) (int, []byte) {
			app, database := newOnboardingTestApp(t)
			user := createOnboardingTestUser(t, database, schemaGuardEmail, schemaGuardPassword, true)
			secret := setupTOTPForUser(t, database, user.ID, []byte(testAppSecretKey))
			code, err := totp.GenerateCode(secret, time.Now())
			if err != nil {
				t.Fatalf("generate totp code: %v", err)
			}
			pending := sealTOTPPendingCookieForTest(t, []byte(testAppSecretKey), user.ID, false)
			return sendSchemaGuardJSON(t, app, http.MethodPost, "/api/v1/sessions/2fa-challenge", pending, map[string]any{"code": code})
		}},
		{operation: "DELETE /api/v1/sessions/current", drive: func(t *testing.T) (int, []byte) {
			return newSchemaGuardOwner(t).send(t, http.MethodDelete, "/api/v1/sessions/current", nil)
		}},
		{operation: "POST /api/v1/password-resets", drive: func(t *testing.T) (int, []byte) {
			app, database := newOnboardingTestApp(t)
			user := createOnboardingTestUser(t, database, schemaGuardEmail, schemaGuardPassword, true)
			return sendSchemaGuardJSON(t, app, http.MethodPost, "/api/v1/password-resets", "", map[string]any{
				"email": schemaGuardEmail, "recovery_code": mustSetRecoveryCodeForUser(t, database, user.ID), "password": schemaGuardPassword,
			})
		}},
		{operation: "POST /api/v1/password-resets/redeem", drive: func(t *testing.T) (int, []byte) {
			app, database := newOnboardingTestApp(t)
			user := createOnboardingTestUser(t, database, schemaGuardEmail, schemaGuardPassword, true)
			response, body := sendSchemaGuardRequest(t, app, http.MethodPost, "/api/v1/password-resets", "", map[string]any{
				"email": schemaGuardEmail, "recovery_code": mustSetRecoveryCodeForUser(t, database, user.ID), "password": schemaGuardPassword,
			})
			requireSchemaGuardStatus(t, "start POST /api/v1/password-resets", response.StatusCode, body, http.StatusOK)
			resetCookie := responseCookieValue(response.Cookies(), resetPasswordCookieName)
			if resetCookie == "" {
				t.Fatal("start POST /api/v1/password-resets set no reset cookie")
			}
			return sendSchemaGuardJSON(t, app, http.MethodPost, "/api/v1/password-resets/redeem", resetPasswordCookieName+"="+resetCookie, map[string]any{
				"password": "EvenStronger2", "confirm_password": "EvenStronger2",
			})
		}},
		{operation: "POST /api/v1/onboarding/steps/1", drive: func(t *testing.T) (int, []byte) {
			owner := newSchemaGuardOnboardingOwner(t)
			return owner.send(t, http.MethodPost, "/api/v1/onboarding/steps/1", map[string]any{"last_period_start": schemaGuardDay(4)})
		}},
		{operation: "POST /api/v1/onboarding/steps/2", drive: func(t *testing.T) (int, []byte) {
			owner := newSchemaGuardOnboardingOwner(t)
			return owner.send(t, http.MethodPost, "/api/v1/onboarding/steps/2", map[string]any{
				"cycle_length": 28, "period_length": 5, "usage_goal": models.UsageGoalAvoid,
			})
		}},
		{operation: "POST /api/v1/onboarding/complete", drive: func(t *testing.T) (int, []byte) {
			owner := newSchemaGuardOnboardingOwner(t)
			lastPeriodStart := time.Now().UTC().AddDate(0, 0, -4).Truncate(24 * time.Hour)
			if err := owner.database.Model(&models.User{}).Where("id = ?", owner.user.ID).Update("last_period_start", lastPeriodStart).Error; err != nil {
				t.Fatalf("seed last_period_start: %v", err)
			}
			return owner.send(t, http.MethodPost, "/api/v1/onboarding/complete", nil)
		}},
		{operation: "DELETE /api/v1/users/current", drive: func(t *testing.T) (int, []byte) {
			return newSchemaGuardOwner(t).send(t, http.MethodDelete, "/api/v1/users/current", map[string]any{"password": schemaGuardPassword})
		}},
		{operation: "PUT /api/v1/users/current/password", drive: func(t *testing.T) (int, []byte) {
			return newSchemaGuardOwner(t).send(t, http.MethodPut, "/api/v1/users/current/password", map[string]any{
				"current_password": schemaGuardPassword, "new_password": "EvenStronger2", "confirm_password": "EvenStronger2",
			})
		}},
		{operation: "POST /api/v1/users/current/recovery-code", drive: func(t *testing.T) (int, []byte) {
			return newSchemaGuardOwner(t).send(t, http.MethodPost, "/api/v1/users/current/recovery-code", map[string]any{"password": schemaGuardPassword})
		}},
		{operation: "PUT /api/v1/users/current/2fa", drive: func(t *testing.T) (int, []byte) {
			owner := newSchemaGuardOwner(t)
			key, err := getTOTPServiceForTest(owner.database).GenerateSetupKey("Ovumcy", owner.email)
			if err != nil {
				t.Fatalf("generate totp setup key: %v", err)
			}
			code, err := totp.GenerateCode(key.Secret(), time.Now())
			if err != nil {
				t.Fatalf("generate totp code: %v", err)
			}
			setupCookie := sealTOTPSetupCookieForTest(t, []byte(testAppSecretKey), owner.user.ID, key.Secret())
			// The spec declares a form body for both 2fa operations, and the
			// handlers read only form values.
			return sendSchemaGuardForm(t, owner.app, http.MethodPut, "/api/v1/users/current/2fa", owner.cookie+"; "+setupCookie, url.Values{
				"code": {code}, "password": {schemaGuardPassword},
			})
		}},
		{operation: "DELETE /api/v1/users/current/2fa", drive: func(t *testing.T) (int, []byte) {
			owner := newSchemaGuardOwner(t)
			if err := getTOTPServiceForTest(owner.database).EnableTOTP(context.Background(), owner.user.ID, owner.user.AuthSessionVersion, "JBSWY3DPEHPK3PXP"); err != nil {
				t.Fatalf("enable totp: %v", err)
			}
			var enrolled models.User
			if err := owner.database.First(&enrolled, owner.user.ID).Error; err != nil {
				t.Fatalf("reload enrolled owner: %v", err)
			}
			return sendSchemaGuardForm(t, owner.app, http.MethodDelete, "/api/v1/users/current/2fa", issueAuthCookieForUser(t, enrolled), url.Values{"password": {schemaGuardPassword}})
		}},
		{operation: "POST /api/v1/users/current/data-wipe/validate", drive: func(t *testing.T) (int, []byte) {
			owner := newSchemaGuardOwner(t)
			owner.seedDay(t, schemaGuardDay(2))
			return owner.send(t, http.MethodPost, "/api/v1/users/current/data-wipe/validate", map[string]any{"password": schemaGuardPassword})
		}},
		{operation: "POST /api/v1/users/current/data-wipe", drive: func(t *testing.T) (int, []byte) {
			owner := newSchemaGuardOwner(t)
			owner.seedDay(t, schemaGuardDay(2))
			return owner.send(t, http.MethodPost, "/api/v1/users/current/data-wipe", map[string]any{"password": schemaGuardPassword})
		}},
		{operation: "POST /api/v1/users/current/password/step-up", drive: func(t *testing.T) (int, []byte) {
			fixture := newOIDCStepupFixture(t, "schema-guard-password-stepup@example.com")
			return schemaGuardBody(t, fixture.postStart(t, "EvenStronger2", "EvenStronger2"))
		}},
		{operation: "POST /api/v1/users/current/data-wipe/step-up", drive: func(t *testing.T) (int, []byte) {
			fixture := newOIDCStepupFixture(t, "schema-guard-wipe-stepup@example.com")
			fixture.oidcStub.reauthErr = nil
			return schemaGuardBody(t, postErasureStepupStart(t, fixture, "/api/v1/users/current/data-wipe/step-up"))
		}},
		{operation: "POST /api/v1/users/current/deletion/step-up", drive: func(t *testing.T) (int, []byte) {
			fixture := newOIDCStepupFixture(t, "schema-guard-deletion-stepup@example.com")
			fixture.oidcStub.reauthErr = nil
			return schemaGuardBody(t, postErasureStepupStart(t, fixture, "/api/v1/users/current/deletion/step-up"))
		}},
		{operation: "POST /api/v1/users/current/oidc/link/step-up", drive: func(t *testing.T) (int, []byte) {
			fixture := newOIDCStepupFixture(t, "schema-guard-link-stepup@example.com")
			return schemaGuardBody(t, postOIDCIdentityLinkStepupStart(t, fixture))
		}},
		{operation: "DELETE /api/v1/users/current/oidc/identities/{id}", drive: func(t *testing.T) (int, []byte) {
			fixture := newOIDCStepupFixture(t, "schema-guard-unlink@example.com")
			giveLinkFixtureAPassword(t, fixture)
			identityID := strconv.FormatUint(uint64(fixture.identity.ID), 10)
			return schemaGuardBody(t, deleteOIDCIdentity(t, fixture, identityID, linkFixturePassword, true))
		}},
		{operation: "POST /api/v1/days/{date}/cycle-start", drive: func(t *testing.T) (int, []byte) {
			owner := newSchemaGuardOwner(t)
			return owner.send(t, http.MethodPost, "/api/v1/days/"+schemaGuardDay(2)+"/cycle-start", nil)
		}},
		{operation: "GET /api/v1/symptoms", drive: func(t *testing.T) (int, []byte) {
			owner := newSchemaGuardOwner(t)
			owner.seedSymptom(t)
			return owner.send(t, http.MethodGet, "/api/v1/symptoms", nil)
		}},
		{operation: "POST /api/v1/symptoms", drive: func(t *testing.T) (int, []byte) {
			return newSchemaGuardOwner(t).send(t, http.MethodPost, "/api/v1/symptoms", map[string]any{
				"name": "Schema guard", "icon": "*", "color": "#AABBCC",
			})
		}},
		{operation: "PATCH /api/v1/symptoms/{id}", drive: func(t *testing.T) (int, []byte) {
			owner := newSchemaGuardOwner(t)
			id := owner.seedSymptom(t)
			return owner.send(t, http.MethodPatch, "/api/v1/symptoms/"+strconv.FormatUint(uint64(id), 10), map[string]any{
				"name": "Schema guard renamed", "icon": "+", "color": "#112233",
			})
		}},
		{operation: "DELETE /api/v1/symptoms/{id}", drive: func(t *testing.T) (int, []byte) {
			owner := newSchemaGuardOwner(t)
			id := owner.seedSymptom(t)
			return owner.send(t, http.MethodDelete, "/api/v1/symptoms/"+strconv.FormatUint(uint64(id), 10), nil)
		}},
		{operation: "POST /api/v1/symptoms/{id}/restore", drive: func(t *testing.T) (int, []byte) {
			owner := newSchemaGuardOwner(t)
			target := "/api/v1/symptoms/" + strconv.FormatUint(uint64(owner.seedSymptom(t)), 10)
			status, body := owner.send(t, http.MethodDelete, target, nil)
			requireSchemaGuardStatus(t, "archive DELETE "+target, status, body, http.StatusOK)
			return owner.send(t, http.MethodPost, target+"/restore", nil)
		}},
		{operation: "GET /api/v1/stats/overview", drive: func(t *testing.T) (int, []byte) {
			owner := newSchemaGuardOwner(t)
			owner.seedDay(t, schemaGuardDay(2))
			return owner.send(t, http.MethodGet, "/api/v1/stats/overview", nil)
		}},
		{operation: "GET /api/v1/exports/summary", drive: func(t *testing.T) (int, []byte) {
			owner := newSchemaGuardOwner(t)
			owner.seedDay(t, schemaGuardDay(2))
			return owner.send(t, http.MethodGet, "/api/v1/exports/summary", nil)
		}},
		{operation: "GET /api/v1/exports/json", drive: func(t *testing.T) (int, []byte) {
			owner := newSchemaGuardOwner(t)
			owner.seedDay(t, schemaGuardDay(2))
			return owner.send(t, http.MethodGet, "/api/v1/exports/json", nil)
		}},
		{operation: "POST /api/v1/imports/json", drive: func(t *testing.T) (int, []byte) {
			return newSchemaGuardOwner(t).send(t, http.MethodPost, "/api/v1/imports/json", map[string]any{
				"exported_at": time.Now().UTC().Format(time.RFC3339),
				"entries": []map[string]any{{
					"date": schemaGuardDay(4), "period": true, "flow": "light", "mood_rating": 0,
					"sex_activity": "none", "cervical_mucus": "none", "cycle_factors": []string{},
					"symptoms": map[string]bool{}, "other_symptoms": []string{}, "notes": "",
				}},
			})
		}},
		{operation: "GET /api/v1/users/current", drive: func(t *testing.T) (int, []byte) {
			return newSchemaGuardOwner(t).send(t, http.MethodGet, "/api/v1/users/current", nil)
		}},
		{operation: "PATCH /api/v1/users/current/profile", drive: func(t *testing.T) (int, []byte) {
			return newSchemaGuardOwner(t).send(t, http.MethodPatch, "/api/v1/users/current/profile", map[string]any{"display_name": "Guard"})
		}},
		{operation: "PATCH /api/v1/users/current/interface", drive: func(t *testing.T) (int, []byte) {
			return newSchemaGuardOwner(t).send(t, http.MethodPatch, "/api/v1/users/current/interface", map[string]any{"language": "de", "theme": "dark"})
		}},
		{operation: "PATCH /api/v1/users/current/tracking", drive: func(t *testing.T) (int, []byte) {
			return newSchemaGuardOwner(t).send(t, http.MethodPatch, "/api/v1/users/current/tracking", map[string]any{
				"track_bbt": true, "temperature_unit": "f", "track_cervical_mucus": true,
				"hide_sex_chip": true, "hide_cycle_factors": false, "hide_notes_field": false,
				"show_historical_phases": true, "week_starts_on": "monday",
			})
		}},
		{operation: "PATCH /api/v1/users/current/cycle", drive: func(t *testing.T) (int, []byte) {
			return newSchemaGuardOwner(t).send(t, http.MethodPatch, "/api/v1/users/current/cycle", map[string]any{"cycle_length": 30, "period_length": 5})
		}},
		{operation: "PATCH /api/v1/users/current/reminders", drive: func(t *testing.T) (int, []byte) {
			return newSchemaGuardOwner(t).send(t, http.MethodPatch, "/api/v1/users/current/reminders", map[string]any{"reminder_lead_days": 3})
		}},
		{operation: "POST /api/v1/users/current/timezone", drive: func(t *testing.T) (int, []byte) {
			return newSchemaGuardOwner(t).send(t, http.MethodPost, "/api/v1/users/current/timezone", map[string]any{"timezone": "Europe/Berlin"})
		}},
		{operation: "POST /api/v1/users/current/webhook", drive: func(t *testing.T) (int, []byte) {
			return newSchemaGuardOwner(t).send(t, http.MethodPost, "/api/v1/users/current/webhook", schemaGuardWebhookSave)
		}},
		{operation: "DELETE /api/v1/users/current/webhook", drive: func(t *testing.T) (int, []byte) {
			owner := newSchemaGuardOwner(t)
			status, body := owner.send(t, http.MethodPost, "/api/v1/users/current/webhook", schemaGuardWebhookSave)
			requireSchemaGuardStatus(t, "seed POST /api/v1/users/current/webhook", status, body, http.StatusOK)
			return owner.send(t, http.MethodDelete, "/api/v1/users/current/webhook", nil)
		}},
		{operation: "POST /api/v1/users/current/calendar-feed", drive: func(t *testing.T) (int, []byte) {
			return newSchemaGuardOwner(t).send(t, http.MethodPost, "/api/v1/users/current/calendar-feed", nil)
		}},
		{operation: "POST /api/v1/users/current/calendar-feed/rotate", drive: func(t *testing.T) (int, []byte) {
			owner := newSchemaGuardOwner(t)
			status, body := owner.send(t, http.MethodPost, "/api/v1/users/current/calendar-feed", nil)
			requireSchemaGuardStatus(t, "seed POST /api/v1/users/current/calendar-feed", status, body, http.StatusOK)
			return owner.send(t, http.MethodPost, "/api/v1/users/current/calendar-feed/rotate", nil)
		}},
		{operation: "DELETE /api/v1/users/current/calendar-feed", drive: func(t *testing.T) (int, []byte) {
			owner := newSchemaGuardOwner(t)
			status, body := owner.send(t, http.MethodPost, "/api/v1/users/current/calendar-feed", nil)
			requireSchemaGuardStatus(t, "seed POST /api/v1/users/current/calendar-feed", status, body, http.StatusOK)
			return owner.send(t, http.MethodDelete, "/api/v1/users/current/calendar-feed", nil)
		}},
	}
}

// The schema guard compares keys and value keywords; the spec text that came with the settings
// echo schemas also makes claims about VALUES, each pinned here against the
// server that answers them.
func TestOpenAPIResponseSchemaDescriptionsMatchTheValuesTheServerAnswers(t *testing.T) {
	schemas, declared := loadOpenAPISchemaGuardDocument(t)
	decodeStatus := func(t *testing.T, status int, body []byte, want int) map[string]any {
		t.Helper()
		requireSchemaGuardStatus(t, "request", status, body, want)
		decoded := map[string]any{}
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Fatalf("decode %q: %v", body, err)
		}
		return decoded
	}
	decode := func(t *testing.T) func(int, []byte) map[string]any {
		return func(status int, body []byte) map[string]any {
			t.Helper()
			return decodeStatus(t, status, body, http.StatusOK)
		}
	}

	t.Run("ExportSummary dates are empty strings without data", func(t *testing.T) {
		owner := newSchemaGuardOwner(t)
		summary := decode(t)(owner.send(t, http.MethodGet, "/api/v1/exports/summary", nil))
		if summary["has_data"] != false || summary["date_from"] != "" || summary["date_to"] != "" {
			t.Fatalf("want has_data false and both dates \"\", got %v", summary)
		}
	})

	t.Run("ExportSummary dates are calendar days with data", func(t *testing.T) {
		owner := newSchemaGuardOwner(t)
		// A non-period day: a period day would be extended by auto period fill,
		// and the summary would span the filled days rather than the seeded one.
		seeded := schemaGuardDay(3)
		status, body := owner.send(t, http.MethodPut, "/api/v1/days/"+seeded, map[string]any{"is_period": false, "notes": "schema guard"})
		requireSchemaGuardStatus(t, "seed PUT /api/v1/days/"+seeded, status, body, http.StatusOK)
		summary := decode(t)(owner.send(t, http.MethodGet, "/api/v1/exports/summary", nil))
		if summary["has_data"] != true || summary["date_from"] != seeded || summary["date_to"] != seeded {
			t.Fatalf("want has_data true and both dates %s (the one seeded day), got %v", seeded, summary)
		}
	})

	t.Run("TrackingSettingsUpdated echoes the stored normalisation", func(t *testing.T) {
		owner := newSchemaGuardOwner(t)
		unrecognised := decode(t)(owner.send(t, http.MethodPatch, "/api/v1/users/current/tracking", map[string]any{
			"temperature_unit": "kelvin", "week_starts_on": "tuesday",
		}))
		if unrecognised["temperature_unit"] != "c" || unrecognised["week_starts_on"] != "sunday" {
			t.Fatalf("want unrecognised values echoed as c / sunday, got %v", unrecognised)
		}
		monday := decode(t)(owner.send(t, http.MethodPatch, "/api/v1/users/current/tracking", map[string]any{"week_starts_on": "monday"}))
		if monday["week_starts_on"] != "monday" {
			t.Fatalf("want the posted monday echoed, got %v", monday)
		}
		omitted := decode(t)(owner.send(t, http.MethodPatch, "/api/v1/users/current/tracking", map[string]any{"temperature_unit": "f"}))
		if omitted["week_starts_on"] != "sunday" || omitted["temperature_unit"] != "f" {
			t.Fatalf("want an omitted week_starts_on read as sunday after a stored monday, got %v", omitted)
		}
	})

	// WEB-99: the webhook save echoes the posted `webhook_enabled` even when
	// `webhook_remove_url` turned delivery off. WebhookSettingsUpdated documents
	// that echo as it is; this pins the documented behaviour, not a desired one,
	// and is expected to change when WEB-99 is fixed.
	t.Run("WebhookSettingsUpdated echoes the request, not the stored switch", func(t *testing.T) {
		owner := newSchemaGuardOwnerWithOptions(t, onboardingTestAppOptions{outboundDeliveryEnabled: true})
		reload := func(t *testing.T) models.User {
			t.Helper()
			var stored models.User
			if err := owner.database.First(&stored, owner.user.ID).Error; err != nil {
				t.Fatalf("reload owner: %v", err)
			}
			return stored
		}
		status, body := owner.send(t, http.MethodPost, "/api/v1/users/current/webhook", schemaGuardWebhookSave)
		requireSchemaGuardStatus(t, "seed POST /api/v1/users/current/webhook", status, body, http.StatusOK)
		if seeded := reload(t); !seeded.WebhookEnabled || seeded.WebhookURL == "" || !seeded.WebhookNotifyPeriod || seeded.WebhookNotifyOvulation {
			t.Fatalf("precondition: the seed save must store delivery on, an endpoint, notify_period on and notify_ovulation off; stored enabled=%v url set=%v period=%v ovulation=%v",
				seeded.WebhookEnabled, seeded.WebhookURL != "", seeded.WebhookNotifyPeriod, seeded.WebhookNotifyOvulation)
		}

		// notify_period is omitted (stored true) and notify_ovulation posted true
		// (stored false), so an echo of the stored flags cannot pass for one of
		// the request.
		echo := decode(t)(owner.send(t, http.MethodPost, "/api/v1/users/current/webhook", map[string]any{
			"webhook_enabled": true, "webhook_remove_url": true, "webhook_notify_ovulation": true,
		}))
		if echo["webhook_enabled"] != true || echo["notify_period"] != false || echo["notify_ovulation"] != true {
			t.Fatalf("want the request echoed (enabled true, notify_period false, notify_ovulation true), got %v", echo)
		}
		if stored := reload(t); stored.WebhookEnabled || stored.WebhookURL != "" {
			t.Fatalf("webhook_remove_url must clear the endpoint and disable delivery; stored enabled=%v url set=%v", stored.WebhookEnabled, stored.WebhookURL != "")
		}
	})

	t.Run("ProfileUpdated reports profile_name_cleared when a name is cleared", func(t *testing.T) {
		owner := newSchemaGuardOwner(t)
		set := decode(t)(owner.send(t, http.MethodPatch, "/api/v1/users/current/profile", map[string]any{"display_name": "  Guard  "}))
		if set["status"] != "profile_updated" || set["display_name"] != "Guard" {
			t.Fatalf("want profile_updated with the trimmed name, got %v", set)
		}
		cleared := decode(t)(owner.send(t, http.MethodPatch, "/api/v1/users/current/profile", map[string]any{"display_name": ""}))
		if cleared["status"] != "profile_name_cleared" || cleared["display_name"] != "" {
			t.Fatalf("want profile_name_cleared with an empty name, got %v", cleared)
		}
	})

	t.Run("TimezoneUpdated reports changed false for the stored zone", func(t *testing.T) {
		owner := newSchemaGuardOwner(t)
		first := decode(t)(owner.send(t, http.MethodPost, "/api/v1/users/current/timezone", map[string]any{"timezone": "Europe/Berlin"}))
		if first["changed"] != true {
			t.Fatalf("a new zone must report changed true, got %v", first)
		}
		again := decode(t)(owner.send(t, http.MethodPost, "/api/v1/users/current/timezone", map[string]any{"timezone": "Europe/Berlin"}))
		if again["changed"] != false {
			t.Fatalf("re-posting the stored zone must report changed false, got %v", again)
		}
	})

	t.Run("readyz answers ProbeUnavailable once storage is gone", func(t *testing.T) {
		owner := newSchemaGuardOwner(t)
		closeTestDatabase(t, owner.database)
		status, body := sendSchemaGuardJSON(t, owner.app, http.MethodGet, "/readyz", "", nil)
		decoded := decodeStatus(t, status, body, http.StatusServiceUnavailable)
		if decoded["status"] != "unavailable" {
			t.Fatalf("want status unavailable, got %v", decoded)
		}
		if found := schemas.violations(decoded, readyUnavailableSchema(t), "body", 0); len(found) > 0 {
			t.Fatalf("the 503 body departs from ProbeUnavailable: %v", found)
		}
	})

	t.Run("a login that needs a second factor matches the oneOf's other alternative", func(t *testing.T) {
		app, database := newOnboardingTestApp(t)
		user := createOnboardingTestUser(t, database, schemaGuardEmail, schemaGuardPassword, true)
		setupTOTPForUser(t, database, user.ID, []byte(testAppSecretKey))
		status, body := sendSchemaGuardJSON(t, app, http.MethodPost, "/api/v1/sessions", "", map[string]any{
			"email": schemaGuardEmail, "password": schemaGuardPassword,
		})
		decoded := decode(t)(status, body)
		if decoded["requires_totp"] != true {
			t.Fatalf("want requires_totp true, got %v", decoded)
		}
		// Validated against the operation's own oneOf: the closed OkResponse
		// refuses this body, so only a declared second-factor alternative passes it.
		if found := schemas.violations(decoded, declared["POST /api/v1/sessions"].schema, "body", 0); len(found) > 0 {
			t.Fatalf("the second-factor login body matches no alternative POST /api/v1/sessions declares: %v", found)
		}
	})
}

// schemaGuardWebhookSave enables delivery to a reserved `.example` host, which
// never resolves, so nothing in the test can reach a real endpoint.
var schemaGuardWebhookSave = map[string]any{
	"webhook_enabled":          true,
	"webhook_url":              "https://ntfy.example/schema-guard",
	"webhook_notify_period":    true,
	"webhook_notify_ovulation": false,
}

// readyUnavailableSchema is the schema the spec declares for the /readyz 503,
// read from the operation itself so a changed or removed declaration fails.
func readyUnavailableSchema(t *testing.T) *openAPINode {
	t.Helper()
	document := parseOpenAPIDocument(t, filepath.Join("..", "..", "docs", "openapi.yaml"))
	schema := document.get("paths").get("/readyz").get("get").get("responses").get("503").get("content").get("application/json").get("schema")
	if schema == nil {
		t.Fatal("docs/openapi.yaml declares no JSON body for the /readyz 503")
	}
	return schema
}
