package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"github.com/pquerna/otp/totp"
	"gorm.io/gorm"
)

// HTTP-integration tests closing mutation survivors in the settings handlers.
// They mirror the internal/api harness (Fiber + real migrated temp DB + real
// services) and assert observable behavior that a surviving mutation would break.
//
// Documented EQUIVALENT mutant (intentionally NOT killed): the
// CONDITIONALS_NEGATION mutant on handlers_settings_danger.go:75 negates the
// `hasJSONBody(c)` operand of
// `if err := c.Bind().Body(&input); err != nil && hasJSONBody(c)` in
// parsePasswordProtectedSettingsAction. passwordProtectedSettingsInput has a
// single string Password field, so a bind error is only reachable on malformed
// JSON and always leaves Password empty; the very next check
// (`if input.Password == ""`) emits the identical settingsMissingPasswordErrorSpec.
// There is therefore no request where the guard's value changes the observable
// spec/status — verified by running the full clear-data/delete/missing-password
// suite green with the operand negated.
//
// GENUINE GAP (now pinned): only the `hasJSONBody(c)` operand negation above is
// equivalent. The CONDITIONALS_NEGATION mutant on the OTHER operand of the same
// line — `err != nil` -> `err == nil` — is a real survivor. With a valid JSON body
// bind succeeds (err == nil), so the mutant enters the block and rejects a
// correct-password erasure as "missing password". The form/HTMX clear-data suites
// never send a real JSON body, so this branch went unexercised; it is now pinned by
// TestValidateClearDataPassword_JSONBodyWithCorrectPasswordSucceeds below.

// --- handlers_settings_danger.go ---

// TestValidateClearDataPassword_JSONBodyWithCorrectPasswordSucceeds pins the
// GENUINE CONDITIONALS_NEGATION survivor on the `err != nil` operand of
// handlers_settings_danger.go:75
// (`if err := c.Bind().Body(&input); err != nil && hasJSONBody(c)`). The JSON REST
// endpoints (POST /data-wipe/validate, DELETE /users/current) are the only callers
// that send a real application/json body; the form/HTMX suites never do, so this
// branch was uncovered. With a valid JSON body carrying the CORRECT current
// password (+ owner session + CSRF header), bind succeeds (err == nil), so the
// mutant (`err == nil`) enters the block and rejects the request as "missing
// password". Driving the non-destructive validate endpoint and asserting SUCCESS
// makes the mutant fail while still requiring a valid password + CSRF + owner
// session — the re-auth-for-erasure invariant is upheld, not bypassed.
func TestValidateClearDataPassword_JSONBodyWithCorrectPasswordSucceeds(t *testing.T) {
	scenario := setupClearDataScenario(t)

	// A JSON body has no csrf_token form field, so the CSRF token rides the
	// X-CSRF-Token header (the source the form-then-header extractor falls back to).
	body := `{"password":"StrongPass1"}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/current/data-wipe/validate", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("X-CSRF-Token", scenario.csrfToken)
	request.Header.Set("Cookie", settingsCookieHeader(scenario.authCookie, scenario.csrfCookie))

	response, err := scenario.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("json validate request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected JSON-body validate with correct password to succeed (200), got %d (body %q)", response.StatusCode, mustReadBodyString(t, response.Body))
	}
	// Validate is non-destructive: an accepted validation must not wipe the seeded
	// logs, custom symptoms, or cycle settings.
	assertClearDataPreconditionsRemain(t, scenario.database, scenario.user)
}

// --- handlers_settings_2fa.go ---

// authCookieAuthenticates returns whether the given auth cookie is accepted on a
// protected page (200). Used to prove a re-issued session cookie carries the
// correct auth_session_version.
func authCookieAuthenticates(t *testing.T, app *fiber.App, authCookieValue string) bool {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("Cookie", authCookieName+"="+authCookieValue)
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("protected probe failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode == http.StatusOK
}

// TestVerifyTOTP2FAEnrollment_ReissuedCookieStaysValidAndReturnsSuccess pins two
// enrollment mutants:
//   - handlers_settings_2fa.go:107 (`NormalizeAuthSessionVersion(...) + 1`): the
//     in-memory bump must match the atomically bumped DB row so the re-issued
//     ovumcy_auth cookie authenticates. `- 1` mints a cookie with the wrong
//     version, which the next request rejects.
//   - handlers_settings_2fa.go:109 (`refreshCurrentSession(...); err != nil`):
//     negating to `== nil` returns early before the HTMX success markup, so the
//     success body disappears.
func TestVerifyTOTP2FAEnrollment_ReissuedCookieStaysValidAndReturnsSuccess(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-enroll-reissue@example.com")

	key, err := getTOTPServiceForTest(ctx.database).GenerateSetupKey("Ovumcy", ctx.user.Email)
	if err != nil {
		t.Fatalf("GenerateSetupKey: %v", err)
	}
	setupCookie := sealTOTPSetupCookieForTest(t, []byte("test-secret-key"), key.Secret())
	code, err := totp.GenerateCode(key.Secret(), time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	form := url.Values{"code": {code}, "password": {"StrongPass1"}, "csrf_token": {ctx.csrfToken}}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/users/current/2fa", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("Cookie", joinCookieHeader(ctx.authCookie, cookiePair(ctx.csrfCookie), setupCookie))
	resp, err := ctx.app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("verify enroll: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HTMX enroll status = %d, want 200", resp.StatusCode)
	}

	// L109: the HTMX success body must be present (skipped if refresh-branch flips).
	body := mustReadBodyString(t, resp.Body)
	if !strings.Contains(body, "toast-close") {
		t.Fatalf("expected dismissible success markup after enrollment, got %q", body)
	}

	// L107: the freshly re-issued cookie must authenticate the originating device.
	refreshed := responseCookie(resp.Cookies(), authCookieName)
	if refreshed == nil || strings.TrimSpace(refreshed.Value) == "" {
		t.Fatal("enrollment must re-issue ovumcy_auth")
	}
	if !authCookieAuthenticates(t, ctx.app, refreshed.Value) {
		t.Fatal("re-issued auth cookie must authenticate; wrong session-version bump would reject it")
	}
}

// TestDisableTOTP2FA_ReissuedCookieStaysValidAndReturnsSuccess pins the disable-side
// twins of the above: handlers_settings_2fa.go:161 (`... + 1`) and :164
// (`refreshCurrentSession(...); err != nil`).
func TestDisableTOTP2FA_ReissuedCookieStaysValidAndReturnsSuccess(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-disable-reissue@example.com")
	if err := getTOTPServiceForTest(ctx.database).EnableTOTP(context.Background(), ctx.user.ID, "JBSWY3DPEHPK3PXP"); err != nil {
		t.Fatalf("EnableTOTP setup: %v", err)
	}
	ctx.refreshAuthCookie(t)

	form := url.Values{"password": {"StrongPass1"}}
	resp := settingsFormRequestWithCSRF(t, ctx, http.MethodDelete, "/api/v1/users/current/2fa", form, map[string]string{
		"Accept-Language": "en",
		"HX-Request":      "true",
	})
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HTMX disable status = %d, want 200", resp.StatusCode)
	}

	// L164: HTMX success body present.
	body := mustReadBodyString(t, resp.Body)
	if !strings.Contains(body, "toast-close") {
		t.Fatalf("expected dismissible success markup after disable, got %q", body)
	}

	// L161: re-issued cookie authenticates.
	refreshed := responseCookie(resp.Cookies(), authCookieName)
	if refreshed == nil || strings.TrimSpace(refreshed.Value) == "" {
		t.Fatal("disable must re-issue ovumcy_auth")
	}
	if !authCookieAuthenticates(t, ctx.app, refreshed.Value) {
		t.Fatal("re-issued auth cookie must authenticate after disable; wrong session-version bump would reject it")
	}
}

// --- handlers_settings_symptoms.go ---

// TestDefaultSymptomDraftIcon_EmptyFallsBackToDefault pins the `icon == ""`
// decision in defaultSymptomDraftIcon (handlers_settings_symptoms.go:120): an
// empty/whitespace icon yields the default sparkle; a provided icon is kept.
// Negating to `!=` swaps the two branches.
func TestDefaultSymptomDraftIcon_EmptyFallsBackToDefault(t *testing.T) {
	if got := defaultSymptomDraftIcon(""); got != "✨" {
		t.Fatalf("empty icon must default to ✨, got %q", got)
	}
	if got := defaultSymptomDraftIcon("   "); got != "✨" {
		t.Fatalf("whitespace icon must default to ✨, got %q", got)
	}
	if got := defaultSymptomDraftIcon("🔥"); got != "🔥" {
		t.Fatalf("provided icon must be kept, got %q", got)
	}
}

// TestLocalizedSettingsSymptomStatus pins the two decisions in
// localizedSettingsSymptomStatus (handlers_settings_symptoms.go:110 and :111):
//   - :110 `SettingsStatusTranslationKey(status) != ""`: a mappable status is
//     localized; an unmappable one is echoed verbatim.
//   - :111 `translateMessage(...) != key`: when the catalog resolves the key to a
//     real localized string it is used; when the key is missing the raw status is
//     echoed instead of the bare key.
func TestLocalizedSettingsSymptomStatus(t *testing.T) {
	// "symptom_created" -> "settings.symptoms.success.created" -> localized copy.
	localized := localizedSettingsSymptomStatusForTest(t, "symptom_created", map[string]string{
		"settings.symptoms.success.created": "Created!",
	})
	if localized != "Created!" {
		t.Fatalf("mappable+translated status must localize, got %q", localized)
	}

	// Mappable key but NO translation available -> echoes the raw status (:111).
	echoed := localizedSettingsSymptomStatusForTest(t, "symptom_created", map[string]string{})
	if echoed != "symptom_created" {
		t.Fatalf("mappable-but-untranslated status must echo raw status, got %q", echoed)
	}

	// Unmappable status -> echoed verbatim (:110 false branch).
	unmapped := localizedSettingsSymptomStatusForTest(t, "definitely_not_a_status", map[string]string{})
	if unmapped != "definitely_not_a_status" {
		t.Fatalf("unmappable status must echo verbatim, got %q", unmapped)
	}
}

// TestSettingsSymptomsHTMXCreateTooLongClearsDraftName pins the
// `spec.Key == "symptom name is too long"` decision
// (handlers_settings_symptoms.go:43): on a too-long create, the re-rendered create
// form must NOT echo the rejected (over-length) name back into the input.
// Negating the equality keeps the over-length value in the form.
func TestSettingsSymptomsHTMXCreateTooLongClearsDraftName(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "settings-symptoms-htmx-draftclear@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")
	csrfCookie, csrfToken := loadSettingsCSRFContext(t, app, authCookie)

	const tooLong = "12345678901234567890123456789012345678901" // 41 chars
	createForm := url.Values{
		"csrf_token": {csrfToken},
		"name":       {tooLong},
		"icon":       {"✨"},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/symptoms", strings.NewReader(createForm.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req.Header.Set("Cookie", joinCookieHeader(authCookie, cookiePair(csrfCookie)))
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("too-long create request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("too-long create status = %d, want 200", resp.StatusCode)
	}

	body := mustReadBodyString(t, resp.Body)
	nameInput := htmlFindElement(mustParseHTMLDocument(t, body), func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "input" && htmlHasAttr(node, "data-symptom-name-input")
	})
	if nameInput == nil {
		t.Fatal("expected the create-form name input to be re-rendered")
	}
	if value := htmlAttr(nameInput, "value"); strings.TrimSpace(value) != "" {
		t.Fatalf("too-long name must be cleared from the re-rendered create form, got value %q", value)
	}
	// Sanity: the rejected value really is absent from the whole rendered form.
	if strings.Contains(body, tooLong) {
		t.Fatalf("rejected over-length name must not be echoed back into the form")
	}
}

// --- handlers_settings_password.go ---

// TestStepupReauthMaxAgeConstant pins stepupReauthMaxAge
// (handlers_settings_password.go:18) against 5 minutes; the ARITHMETIC_BASE mutant
// swaps '*' for '/', collapsing the OIDC step-up replay window to ~0.
func TestStepupReauthMaxAgeConstant(t *testing.T) {
	if stepupReauthMaxAge != 5*time.Minute {
		t.Fatalf("stepupReauthMaxAge = %v, want 5m", stepupReauthMaxAge)
	}
	if stepupReauthMaxAge <= 0 {
		t.Fatalf("stepupReauthMaxAge must be a positive replay window, got %v", stepupReauthMaxAge)
	}
}

// --- handlers_settings_cycle_helpers.go ---

// TestUpdateCycleSettings_JSONBodyPersistsLastPeriodStart pins the JSON-body
// `input.LastPeriodStart != ""` decision (handlers_settings_cycle_helpers.go:22):
// a non-empty last_period_start supplied via a JSON body must mark the field as
// set so it is validated and persisted. Negating to `== ""` drops the flag for a
// real date, so the start date is silently ignored. Uses a no-CSRF app to isolate
// JSON body parsing/negotiation (per the testing rules).
func TestUpdateCycleSettings_JSONBodyPersistsLastPeriodStart(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "settings-cycle-json@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	const startDate = "2026-02-10"
	jsonBody := `{"cycle_length":28,"period_length":5,"auto_period_fill":true,"last_period_start":"` + startDate + `"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/cycle", strings.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en")
	req.Header.Set("Cookie", authCookie)
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("json cycle update failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("json cycle update status = %d, want 200 (body %q)", resp.StatusCode, mustReadBodyString(t, resp.Body))
	}

	var persisted models.User
	if err := database.Select("cycle_length", "last_period_start").First(&persisted, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if persisted.LastPeriodStart == nil {
		t.Fatal("JSON-body last_period_start must be persisted (LastPeriodStartSet must be true for a non-empty date)")
	}
	if got := services.CalendarDayKey(*persisted.LastPeriodStart); got != startDate {
		t.Fatalf("persisted last_period_start = %q, want %q", got, startDate)
	}
}

// TestCompleteLocalPasswordSetupReauth_SuccessIssuesRecoveryCode drives the OIDC
// step-up password-setup callback all the way through its happy path to pin
// handlers_settings_password.go:193 (`refreshPasswordChangeSession(...); err !=
// nil`). On success the handler must set the recovery-code issuance cookie and
// redirect to /recovery-code. Negating the guard to `== nil` returns early right
// after the (successful) session refresh, so neither the redirect nor the
// recovery cookie is produced. Existing step-up coverage only exercises the error
// arms (no session / already-local), leaving this success arm uncovered.
func TestCompleteLocalPasswordSetupReauth_SuccessIssuesRecoveryCode(t *testing.T) {
	stub := &stubOIDCWorkflowService{enabled: true, localPublicAuthEnabled: true} // ValidateReauthExchange -> nil
	app, database, handler := newSettingsMutationStepupApp(t, stub)

	// OIDC-only user: no local password yet, so the setup flow can complete.
	oidcUser := models.User{
		Email:               "settings-stepup-success@example.com",
		LocalAuthEnabled:    false,
		Role:                models.RoleOwner,
		OnboardingCompleted: true,
		AuthSessionVersion:  1,
		CycleLength:         28,
		PeriodLength:        5,
		AutoPeriodFill:      true,
		CreatedAt:           time.Now().UTC(),
	}
	if err := database.Create(&oidcUser).Error; err != nil {
		t.Fatalf("create oidc-only user: %v", err)
	}
	authCookie := issueAuthCookieForUser(t, oidcUser)

	// Seed a step-up cookie for this user and learn its sealed State value so the
	// callback's state check passes.
	stepupCookie, stateValue := seedStepupCookie(t, app, handler, oidcUser.ID)

	form := url.Values{"state": {stateValue}, "code": {"stub-code"}}
	req := httptest.NewRequest(http.MethodPost, "/auth/oidc/callback", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", joinCookieHeader(authCookie, stepupCookie))
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("oidc stepup callback failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Success path: 303 -> /recovery-code with the recovery-code issuance cookie.
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("stepup success status = %d, want 303 (body %q)", resp.StatusCode, mustReadBodyString(t, resp.Body))
	}
	if loc := resp.Header.Get("Location"); loc != "/recovery-code" {
		t.Fatalf("stepup success redirect = %q, want /recovery-code", loc)
	}
	if cookie := responseCookie(resp.Cookies(), recoveryCodeCookieName); cookie == nil || strings.TrimSpace(cookie.Value) == "" {
		t.Fatalf("stepup success must set the %s issuance cookie", recoveryCodeCookieName)
	}

	// The password setup must have committed (local auth now enabled).
	var reloaded models.User
	if err := database.First(&reloaded, oidcUser.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if !reloaded.LocalAuthEnabled {
		t.Fatal("expected local auth to be enabled after successful password setup reauth")
	}
}

// newSettingsMutationStepupApp builds a self-contained app (real DB + real
// services + stub OIDC) with cookie-secure transport and a seed route that mints a
// step-up cookie and echoes its State. It intentionally does not reuse the shared
// fullpage-fallback harness to avoid editing another area's aggregator test file.
func newSettingsMutationStepupApp(t *testing.T, stub *stubOIDCWorkflowService) (*fiber.App, *gorm.DB, *Handler) {
	t.Helper()

	databasePath := filepath.Join(t.TempDir(), "ovumcy-settings-stepup-test.db")
	database, err := db.OpenSQLite(databasePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	i18nManager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("init i18n: %v", err)
	}

	handler, err := NewHandler("test-secret-key", time.UTC, i18nManager, true, newTestHandlerDependencies(database, i18nManager, onboardingTestAppOptions{oidcService: stub, cookieSecure: true}))
	if err != nil {
		t.Fatalf("init handler: %v", err)
	}

	app := fiber.New()
	app.Use(handler.LanguageMiddleware)
	app.Get("/__seed/stepup", func(c fiber.Ctx) error {
		userID := uint(fiber.Query[int](c, "user_id", 0))
		state, stateErr := newOIDCStepupState(time.Now(), oidcStepupPurposeLocalPasswordSetup, userID, "prepared-hash")
		if stateErr != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(stateErr.Error())
		}
		if err := handler.setOIDCStepupCookie(c, state); err != nil {
			return c.Status(fiber.StatusInternalServerError).SendString(err.Error())
		}
		return c.JSON(fiber.Map{"state": state.State})
	})
	RegisterRoutes(app, handler)
	app.Use(handler.NotFound)
	return app, database, handler
}

// seedStepupCookie hits the seed route and returns the step-up cookie header plus
// the sealed State value that the callback must echo.
func seedStepupCookie(t *testing.T, app *fiber.App, _ *Handler, userID uint) (string, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/__seed/stepup?user_id="+utoa(userID), nil)
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("seed stepup failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("seed stepup status = %d, want 200", resp.StatusCode)
	}

	var seeded struct {
		State string `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&seeded); err != nil {
		t.Fatalf("decode seeded stepup state: %v", err)
	}

	cookie := responseCookie(resp.Cookies(), oidcStepupCookieName)
	if cookie == nil {
		t.Fatal("seed route did not set the stepup cookie")
	}
	return cookiePair(cookie), seeded.State
}

// localizedSettingsSymptomStatusForTest drives localizedSettingsSymptomStatus with
// an injected message catalog via a mounted route (the function reads
// currentMessages(c) from the request Locals).
func localizedSettingsSymptomStatusForTest(t *testing.T, status string, messages map[string]string) string {
	t.Helper()
	app := fiber.New()
	app.Get("/localize", func(c fiber.Ctx) error {
		c.Locals(contextMessagesKey, messages)
		return c.SendString(localizedSettingsSymptomStatus(c, status))
	})
	req := httptest.NewRequest(http.MethodGet, "/localize", nil)
	resp, err := app.Test(req, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("localizer probe failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return mustReadBodyString(t, resp.Body)
}
