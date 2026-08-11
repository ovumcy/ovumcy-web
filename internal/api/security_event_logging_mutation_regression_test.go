package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// captureEmittedSecurityEvent drives the given emit callback inside a real
// Fiber request context (so c.Method/Path/format resolve) with the audit
// stream forced on, and returns the single logged "security event: ..." line.
// It is the seam the direct-unit security-event tests use to assert exactly
// which key/value fields the emitter writes.
func captureEmittedSecurityEvent(t *testing.T, requestPath string, emit func(handler *Handler, c fiber.Ctx)) string {
	t.Helper()

	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)
	var output bytes.Buffer
	log.SetOutput(&output)

	handler := &Handler{auditLogEnabled: true}
	app := fiber.New()
	app.Get("/*", func(c fiber.Ctx) error {
		emit(handler, c)
		return c.SendStatus(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodGet, requestPath, nil)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("emit request failed: %v", err)
	}
	_ = response.Body.Close()
	return output.String()
}

// TestSecurityEventIncludesUserFieldsOnlyWhenPresent pins the currentUser guard
// in emitSecurityEvent: user_id and role are appended only when a user is on
// the request context (survivor: the `ok && user != nil` conditional). A
// regression that always/never emits identity would either leak on anonymous
// events or drop the actor on authenticated ones.
func TestSecurityEventIncludesUserFieldsOnlyWhenPresent(t *testing.T) {
	withUser := captureEmittedSecurityEvent(t, "/dashboard", func(_ *Handler, c fiber.Ctx) {
		c.Locals(contextUserKey, &models.User{ID: 7, Role: models.RoleOwner})
		emitSecurityEvent(c, "session.login", "success")
	})
	if !strings.Contains(withUser, `user_id="7"`) || !strings.Contains(withUser, `role="owner"`) {
		t.Fatalf("expected user_id and role when a user is present, got %q", withUser)
	}

	anonymous := captureEmittedSecurityEvent(t, "/dashboard", func(_ *Handler, c fiber.Ctx) {
		emitSecurityEvent(c, "session.login", "denied")
	})
	if strings.Contains(anonymous, "user_id=") || strings.Contains(anonymous, "role=") {
		t.Fatalf("did not expect identity fields for an anonymous event, got %q", anonymous)
	}
}

// TestSecurityEventSortsExtraFields pins the deterministic ordering of extra
// fields (survivor: the sort less-func comparison). Fields supplied out of
// order must appear alphabetically by key so log lines are stable and greppable.
func TestSecurityEventSortsExtraFields(t *testing.T) {
	line := captureEmittedSecurityEvent(t, "/dashboard", func(_ *Handler, c fiber.Ctx) {
		emitSecurityEvent(c, "settings.update", "success",
			securityEventField("zeta", "1"),
			securityEventField("alpha", "2"),
		)
	})
	alphaAt := strings.Index(line, `alpha="2"`)
	zetaAt := strings.Index(line, `zeta="1"`)
	if alphaAt < 0 || zetaAt < 0 {
		t.Fatalf("expected both extra fields in log line, got %q", line)
	}
	if alphaAt > zetaAt {
		t.Fatalf("expected extra fields sorted (alpha before zeta), got %q", line)
	}
}

// TestLogSecurityErrorAppendsReasonOnlyWithKey pins the reason-field guard in
// logSecurityError (survivor: the `TrimSpace(spec.Key) != ""` conditional): a
// spec with a stable key adds reason=<key>, an empty-key spec adds none.
func TestLogSecurityErrorAppendsReasonOnlyWithKey(t *testing.T) {
	withKey := captureEmittedSecurityEvent(t, "/dashboard", func(handler *Handler, c fiber.Ctx) {
		handler.logSecurityError(c, "session.refresh", APIErrorSpec{Status: http.StatusForbidden, Key: "role_denied"})
	})
	if !strings.Contains(withKey, `reason="role_denied"`) {
		t.Fatalf("expected reason field for a keyed spec, got %q", withKey)
	}

	withoutKey := captureEmittedSecurityEvent(t, "/dashboard", func(handler *Handler, c fiber.Ctx) {
		handler.logSecurityError(c, "session.refresh", APIErrorSpec{Status: http.StatusForbidden, Key: "   "})
	})
	if strings.Contains(withoutKey, "reason=") {
		t.Fatalf("did not expect a reason field for a blank-key spec, got %q", withoutKey)
	}
}

// TestLogHealthDataMutationErrorAppendsTargetOnlyWhenSet pins the target guard
// in logHealthDataMutationError (survivor: the `target != ""` conditional):
// a named target adds target=<name>, a blank one does not, while domain stays.
func TestLogHealthDataMutationErrorAppendsTargetOnlyWhenSet(t *testing.T) {
	withTarget := captureEmittedSecurityEvent(t, "/api/v1/days/2026-02-17", func(handler *Handler, c fiber.Ctx) {
		handler.logHealthDataMutationError(c, "health.day_upsert", APIErrorSpec{Status: http.StatusBadRequest, Key: "bad_date"}, "day")
	})
	if !strings.Contains(withTarget, `target="day"`) || !strings.Contains(withTarget, `domain="health_data"`) {
		t.Fatalf("expected target and domain fields for a named target, got %q", withTarget)
	}

	withoutTarget := captureEmittedSecurityEvent(t, "/api/v1/days/2026-02-17", func(handler *Handler, c fiber.Ctx) {
		handler.logHealthDataMutationError(c, "health.day_upsert", APIErrorSpec{Status: http.StatusBadRequest, Key: "bad_date"}, "  ")
	})
	if strings.Contains(withoutTarget, "target=") {
		t.Fatalf("did not expect a target field for a blank target, got %q", withoutTarget)
	}
	if !strings.Contains(withoutTarget, `domain="health_data"`) {
		t.Fatalf("expected domain field to remain for a blank target, got %q", withoutTarget)
	}
}

// TestSecurityEventOutcomeForSpec pins the failure/denied split at the 500
// boundary (survivor: `spec.Status >= StatusInternalServerError`): 5xx is a
// server failure, everything below is a denial. The boundary status (500)
// itself must classify as failure.
func TestSecurityEventOutcomeForSpec(t *testing.T) {
	t.Parallel()

	cases := []struct {
		status int
		want   string
	}{
		{http.StatusInternalServerError, "failure"},
		{http.StatusBadGateway, "failure"},
		{http.StatusForbidden, "denied"},
		{http.StatusTooManyRequests, "denied"},
		{http.StatusBadRequest, "denied"},
	}
	for _, tc := range cases {
		if got := securityEventOutcomeForSpec(APIErrorSpec{Status: tc.status}); got != tc.want {
			t.Fatalf("securityEventOutcomeForSpec(status=%d) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

// TestNormalizeSecurityEventKey pins the key normalizer used for extra fields:
// trim + lowercase + space-to-underscore, and an empty/whitespace key drops out
// (survivor: the empty-guard conditional), so a blank key never produces a
// stray `="..."` fragment.
func TestNormalizeSecurityEventKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{"Reason", "reason"},
		{"  Retry After  ", "retry_after"},
		{"", ""},
		{"   ", ""},
	}
	for _, tc := range cases {
		if got := normalizeSecurityEventKey(tc.in); got != tc.want {
			t.Fatalf("normalizeSecurityEventKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCreateSymptomLogsMutationWithoutLeakingUserInput(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)

	var output bytes.Buffer
	log.SetOutput(&output)

	ctx := newSettingsSecurityTestContextWithOptions(t, "settings-symptom-audit@example.com", onboardingTestAppOptions{enableCSRF: true, auditLogEnabled: true})
	form := url.Values{
		"name": {"=Cycle secret"},
		"icon": {"S"},
	}

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/symptoms", form, map[string]string{
		"Accept": "application/json",
	})
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusCreated {
		t.Fatalf("expected status 201, got %d", response.StatusCode)
	}

	logLine := output.String()
	if !strings.Contains(logLine, `security event: action="health.symptom_create" outcome="success"`) {
		t.Fatalf("expected health symptom create security event, got %q", logLine)
	}
	if !strings.Contains(logLine, `domain="health_data"`) {
		t.Fatalf("expected health_data domain in log line, got %q", logLine)
	}
	if !strings.Contains(logLine, `target="symptom"`) {
		t.Fatalf("expected symptom target in log line, got %q", logLine)
	}
	if strings.Contains(logLine, "=Cycle secret") {
		t.Fatalf("did not expect symptom name in mutation logs: %q", logLine)
	}
}

func TestUpsertDayLogsSanitizedPathWithoutConcreteDate(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)

	var output bytes.Buffer
	log.SetOutput(&output)

	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{auditLogEnabled: true})
	user := createOnboardingTestUser(t, database, "settings-day-audit@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/2026-02-17", strings.NewReader(url.Values{
		"is_period": {"true"},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("day upsert request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	logLine := output.String()
	if !strings.Contains(logLine, `security event: action="health.day_upsert" outcome="success"`) {
		t.Fatalf("expected health.day_upsert security event, got %q", logLine)
	}
	if !strings.Contains(logLine, `path="/api/v1/days/:date"`) {
		t.Fatalf("expected sanitized day route in log line, got %q", logLine)
	}
	if strings.Contains(logLine, "2026-02-17") {
		t.Fatalf("did not expect concrete health date in mutation logs: %q", logLine)
	}
}

// captureSettingsMutationAudit drives one authenticated settings-form request
// against an app with the audit stream on and returns the response together
// with the security-event output that request produced. The log writer is
// swapped around the request only, so the captured text holds the handler's own
// lines and not the sign-in that built the context.
func captureSettingsMutationAudit(t *testing.T, ctx settingsSecurityTestContext, method string, path string, form url.Values) (*http.Response, string) {
	t.Helper()

	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)
	var output bytes.Buffer
	log.SetOutput(&output)

	response := settingsFormRequestWithCSRF(t, ctx, method, path, form, map[string]string{
		"Accept": "application/json",
	})
	return response, output.String()
}

// assertSecurityEventNamesActor asserts that the one audit line carrying the
// given action/outcome header names the owner it acted for. It matches the
// line rather than the whole buffer on purpose: a request emits several
// events, and a buffer-wide search for user_id passes as long as any one of
// them is attributed.
func assertSecurityEventNamesActor(t *testing.T, logOutput string, action string, outcome string, userID uint) {
	t.Helper()

	header := fmt.Sprintf("security event: action=%q outcome=%q", action, outcome)
	for _, line := range strings.Split(logOutput, "\n") {
		if !strings.Contains(line, header) {
			continue
		}
		if !strings.Contains(line, fmt.Sprintf("user_id=%q", strconv.FormatUint(uint64(userID), 10))) {
			t.Fatalf("%s must name the owner it acted for, got %q", header, line)
		}
		if !strings.Contains(line, fmt.Sprintf("role=%q", models.RoleOwner)) {
			t.Fatalf("%s must carry the actor's role, got %q", header, line)
		}
		return
	}
	t.Fatalf("expected an audit line for %s, got %q", header, logOutput)
}

// captureStepupCallbackAudit drives an OIDC step-up callback with the audit
// stream captured, and returns the response together with the emitted lines.
func captureStepupCallbackAudit(t *testing.T, fixture *oidcStepupFixture, stepupCookie string, state string) (*http.Response, string) {
	t.Helper()

	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)
	var output bytes.Buffer
	log.SetOutput(&output)

	response := postOIDCStepupCallback(t, fixture, stepupCookie, state, "callback-code")
	return response, output.String()
}

// TestStepupCallbacksAuditTheOwnerTheyActFor covers the two completion
// handlers that resolve their session by calling authenticateRequest directly
// instead of sitting behind AuthRequired: /auth/oidc/callback cannot carry
// that middleware, because ordinary sign-in has to work for a visitor with no
// session. The actor has to be published anyway — an erasure that names no
// owner is unusable in a review of an instance hosting more than one, which is
// exactly the household case this product supports. Both purposes are covered
// because the gap belongs to the shared resolver, not to either flow.
func TestStepupCallbacksAuditTheOwnerTheyActFor(t *testing.T) {
	t.Parallel()

	t.Run("erasure", func(t *testing.T) {
		t.Parallel()

		fixture := newOIDCStepupFixtureWithAudit(t, "stepup-audit-erasure@example.com", true)
		fixture.oidcStub.reauthErr = nil
		seedStepupDayEntry(t, fixture)

		startResponse := postErasureStepupStart(t, fixture, "/api/v1/users/current/data-wipe/step-up")
		defer func() { _ = startResponse.Body.Close() }()
		if startResponse.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 from the clear-data step-up start, got %d", startResponse.StatusCode)
		}

		stepupCookie := readStepupCookie(t, startResponse)
		state := extractStepupCallbackState(t, fixture)

		callbackResponse, auditLog := captureStepupCallbackAudit(t, fixture, stepupCookie, state)
		defer func() { _ = callbackResponse.Body.Close() }()

		if got := countStepupDayEntries(t, fixture); got != 0 {
			t.Fatalf("expected the callback to erase the diary, day entries = %d", got)
		}
		assertHealthDataMutationAudited(t, auditLog, "settings.clear_data", "success", "account_data")
		assertSecurityEventNamesActor(t, auditLog, "settings.clear_data", "success", fixture.user.ID)
	})

	t.Run("local password setup", func(t *testing.T) {
		t.Parallel()

		fixture := newOIDCStepupFixtureWithAudit(t, "stepup-audit-password@example.com", true)
		fixture.oidcStub.reauthErr = nil

		startResponse := fixture.postStart(t, "EvenStronger2", "EvenStronger2")
		defer func() { _ = startResponse.Body.Close() }()
		if startResponse.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 from the password step-up start, got %d", startResponse.StatusCode)
		}

		stepupCookie := readStepupCookie(t, startResponse)
		state := extractStepupCallbackState(t, fixture)

		callbackResponse, auditLog := captureStepupCallbackAudit(t, fixture, stepupCookie, state)
		defer func() { _ = callbackResponse.Body.Close() }()

		assertSecurityEventNamesActor(t, auditLog, "auth.local_password_setup.callback", "success", fixture.user.ID)
	})
}

// securityEventLine resolves the ONE audit line an action/outcome pair produced
// and returns it, so every field assertion below is made against that line
// rather than against the whole capture buffer. Asserting on the buffer lets a
// `domain=` emitted by some other line of the same request satisfy a check the
// line under test fails, which is exactly the shape of the defect these tests
// exist to catch. The uniqueness check is the other half: one mutation is one
// audit line, so a handler that logs its success twice is a defect too.
func securityEventLine(t *testing.T, logOutput string, action string, outcome string) string {
	t.Helper()

	header := fmt.Sprintf("security event: action=%q outcome=%q", action, outcome)
	matches := []string{}
	for _, line := range strings.Split(logOutput, "\n") {
		if strings.Contains(line, header) {
			matches = append(matches, line)
		}
	}
	if len(matches) == 0 {
		t.Fatalf("expected one audit line matching [%s], got %q", header, logOutput)
	}
	if len(matches) > 1 {
		t.Fatalf("expected exactly one audit line matching [%s], got %d: %q", header, len(matches), logOutput)
	}
	return matches[0]
}

// assertHealthDataMutationAudited pins the three fields the typed mutation
// mechanism is responsible for: the wire-visible action name operators filter
// on, the health-data domain tag, and a non-empty target naming what was
// touched. A handler that logs through the plain security-event path emits the
// action alone, so the domain/target assertions are what catch the regression.
func assertHealthDataMutationAudited(t *testing.T, logOutput string, action string, outcome string, target string) {
	t.Helper()

	line := securityEventLine(t, logOutput, action, outcome)
	if !strings.Contains(line, `domain="health_data"`) {
		t.Fatalf("expected %s to be tagged domain=\"health_data\", got %q", action, line)
	}
	if target == "" {
		t.Fatalf("expected a non-empty target for %s", action)
	}
	if !strings.Contains(line, fmt.Sprintf("target=%q", target)) {
		t.Fatalf("expected %s to carry target=%q, got %q", action, target, line)
	}
}

// assertAccountMutationAudited is the negative-domain twin: an account-record
// mutation must be audited AND must stay out of the health-data domain, because
// `domain="health_data"` is the filter an incident review uses to select the
// actions that changed the cycle record. The positive anchor (a named domain
// and target on the same line) is what keeps this from passing when the audit
// line disappears entirely.
func assertAccountMutationAudited(t *testing.T, logOutput string, action string, outcome string, target string) {
	t.Helper()

	line := securityEventLine(t, logOutput, action, outcome)
	if strings.Contains(line, `domain="health_data"`) {
		t.Fatalf("expected %s to stay out of the health-data domain, got %q", action, line)
	}
	if !strings.Contains(line, `domain="account"`) {
		t.Fatalf("expected %s to be tagged domain=\"account\", got %q", action, line)
	}
	if !strings.Contains(line, fmt.Sprintf("target=%q", target)) {
		t.Fatalf("expected %s to carry target=%q, got %q", action, target, line)
	}
}

// TestClearAllDataIsAuditedAsHealthDataMutation covers the two branches that
// erase an owner's tracked data: the accepted wipe and the denial that stops
// one. Both are health-data mutations and must be greppable as such.
func TestClearAllDataIsAuditedAsHealthDataMutation(t *testing.T) {
	ctx := newSettingsSecurityTestContextWithOptions(t, "settings-clear-data-audit@example.com", onboardingTestAppOptions{enableCSRF: true, auditLogEnabled: true})

	denied, deniedLog := captureSettingsMutationAudit(t, ctx, http.MethodPost, "/api/v1/users/current/data-wipe", url.Values{
		"password": {"WrongPass1"},
	})
	defer func() { _ = denied.Body.Close() }()

	if denied.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401 for a wrong clear-data password, got %d", denied.StatusCode)
	}
	assertHealthDataMutationAudited(t, deniedLog, "settings.clear_data", "denied", "account_data")
	if !strings.Contains(deniedLog, `reason="invalid password"`) {
		t.Fatalf("expected the denial reason to survive the mutation path, got %q", deniedLog)
	}

	accepted, acceptedLog := captureSettingsMutationAudit(t, ctx, http.MethodPost, "/api/v1/users/current/data-wipe", url.Values{
		"password": {"StrongPass1"},
	})
	defer func() { _ = accepted.Body.Close() }()

	if accepted.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for an accepted clear-data request, got %d", accepted.StatusCode)
	}
	assertHealthDataMutationAudited(t, acceptedLog, "settings.clear_data", "success", "account_data")
	if strings.Contains(acceptedLog, ctx.user.Email) {
		t.Fatalf("did not expect the owner's email in an erasure audit line: %q", acceptedLog)
	}
}

// TestDeleteAccountIsAuditedAsHealthDataMutation is the delete-account half of
// the same contract: the account and everything attached to it is health data,
// so its audit line carries the domain and a target naming the erased scope.
func TestDeleteAccountIsAuditedAsHealthDataMutation(t *testing.T) {
	ctx := newSettingsSecurityTestContextWithOptions(t, "settings-delete-account-audit@example.com", onboardingTestAppOptions{enableCSRF: true, auditLogEnabled: true})

	denied, deniedLog := captureSettingsMutationAudit(t, ctx, http.MethodDelete, "/api/v1/users/current", url.Values{})
	defer func() { _ = denied.Body.Close() }()

	if denied.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400 for a missing delete-account password, got %d", denied.StatusCode)
	}
	assertHealthDataMutationAudited(t, deniedLog, "settings.delete_account", "denied", "account")
	if !strings.Contains(deniedLog, `reason="invalid password"`) {
		t.Fatalf("expected the denial reason to survive the mutation path, got %q", deniedLog)
	}

	accepted, acceptedLog := captureSettingsMutationAudit(t, ctx, http.MethodDelete, "/api/v1/users/current", url.Values{
		"password": {"StrongPass1"},
	})
	defer func() { _ = accepted.Body.Close() }()

	if accepted.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for an accepted delete-account request, got %d", accepted.StatusCode)
	}
	assertHealthDataMutationAudited(t, acceptedLog, "settings.delete_account", "success", "account")
	if strings.Contains(acceptedLog, ctx.user.Email) {
		t.Fatalf("did not expect the owner's email in an erasure audit line: %q", acceptedLog)
	}
}

// TestErasureStorageFailuresAreAuditedAsHealthDataMutations covers the branch
// neither handler reaches on a healthy instance: the password was accepted and
// the erasure itself failed. Both erasure transactions start at daily_logs, so
// dropping that table fails them without disturbing the users table the auth
// middleware reads — and leaves the account in place, so one signed-in context
// exercises both handlers. A 5xx outcome is "failure", not "denied".
func TestErasureStorageFailuresAreAuditedAsHealthDataMutations(t *testing.T) {
	ctx := newSettingsSecurityTestContextWithOptions(t, "settings-erasure-failure-audit@example.com", onboardingTestAppOptions{enableCSRF: true, auditLogEnabled: true})
	if err := ctx.database.Exec("DROP TABLE daily_logs").Error; err != nil {
		t.Fatalf("drop daily_logs: %v", err)
	}

	clearData, clearDataLog := captureSettingsMutationAudit(t, ctx, http.MethodPost, "/api/v1/users/current/data-wipe", url.Values{
		"password": {"StrongPass1"},
	})
	defer func() { _ = clearData.Body.Close() }()

	if clearData.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected status 500 once clear-data storage is gone, got %d", clearData.StatusCode)
	}
	assertHealthDataMutationAudited(t, clearDataLog, "settings.clear_data", "failure", "account_data")
	if !strings.Contains(clearDataLog, `reason="failed to clear data"`) {
		t.Fatalf("expected the mapped clear-data failure reason, got %q", clearDataLog)
	}

	deleteAccount, deleteAccountLog := captureSettingsMutationAudit(t, ctx, http.MethodDelete, "/api/v1/users/current", url.Values{
		"password": {"StrongPass1"},
	})
	defer func() { _ = deleteAccount.Body.Close() }()

	if deleteAccount.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected status 500 once delete-account storage is gone, got %d", deleteAccount.StatusCode)
	}
	assertHealthDataMutationAudited(t, deleteAccountLog, "settings.delete_account", "failure", "account")
	if !strings.Contains(deleteAccountLog, `reason="failed to delete account"`) {
		t.Fatalf("expected the mapped delete-account failure reason, got %q", deleteAccountLog)
	}

	var usersCount int64
	if err := ctx.database.Model(&models.User{}).Where("id = ?", ctx.user.ID).Count(&usersCount).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	if usersCount != 1 {
		t.Fatalf("expected a failed erasure to leave the account in place, got count=%d", usersCount)
	}
}

// TestSettingsMutationsWithPersistenceAreAudited is the settings half of the
// zero-audit class: three endpoints that each write a users column and emitted
// no security event at all, so an operator reviewing an incident saw the
// cycle-settings form's changes and none of theirs.
//
// The three rows are not interchangeable, which is the point of running them
// through one table: the reminder window and the timezone are health-data
// mutations (they decide when a cycle prediction is announced and which
// calendar day it lands on), while the display name is account identity that no
// cycle math reads — so it is audited under domain="account" and asserted to
// stay OUT of the health-data domain. The two deliberate non-members of the
// class are named here rather than left to inference: PATCH /interface persists
// nothing to users (language is a cookie, theme is client storage) and POST
// /lang only writes the language cookie, so neither has a persisted mutation to
// record.
func TestSettingsMutationsWithPersistenceAreAudited(t *testing.T) {
	cases := []struct {
		name    string
		method  string
		path    string
		accept  url.Values
		refuse  url.Values
		action  string
		target  string
		leak    string
		account bool
	}{
		{
			name:   "timezone",
			method: http.MethodPost,
			path:   "/api/v1/users/current/timezone",
			accept: url.Values{"timezone": {"Europe/Berlin"}},
			refuse: url.Values{"timezone": {"Local"}},
			action: "settings.timezone_update",
			target: "timezone",
			leak:   "Europe/Berlin",
		},
		{
			name:   "reminders",
			method: http.MethodPatch,
			path:   "/api/v1/users/current/reminders",
			accept: url.Values{"reminder_lead_days": {"3"}},
			refuse: url.Values{"reminder_lead_days": {"soon"}},
			action: "settings.reminders_update",
			target: "reminder_settings",
		},
		{
			name:    "profile",
			method:  http.MethodPatch,
			path:    "/api/v1/users/current/profile",
			accept:  url.Values{"display_name": {"Renamed"}},
			refuse:  url.Values{"display_name": {strings.Repeat("x", 512)}},
			action:  "settings.profile_update",
			target:  "profile",
			leak:    "Renamed",
			account: true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := newSettingsSecurityTestContextWithOptions(t, "settings-"+testCase.name+"-audit@example.com", onboardingTestAppOptions{enableCSRF: true, auditLogEnabled: true})

			refused, refusedLog := captureSettingsMutationAudit(t, ctx, testCase.method, testCase.path, testCase.refuse)
			defer func() { _ = refused.Body.Close() }()
			if refused.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected status 400 for a refused %s update, got %d", testCase.name, refused.StatusCode)
			}

			accepted, acceptedLog := captureSettingsMutationAudit(t, ctx, testCase.method, testCase.path, testCase.accept)
			defer func() { _ = accepted.Body.Close() }()
			if accepted.StatusCode != http.StatusOK {
				t.Fatalf("expected status 200 for an accepted %s update, got %d", testCase.name, accepted.StatusCode)
			}

			assertAudited := assertHealthDataMutationAudited
			if testCase.account {
				assertAudited = assertAccountMutationAudited
			}
			assertAudited(t, refusedLog, testCase.action, "denied", testCase.target)
			assertAudited(t, acceptedLog, testCase.action, "success", testCase.target)

			// The submitted value is what a fixed target exists to keep out of the
			// stream: a zone is a location, a display name is account identity.
			if testCase.leak != "" && strings.Contains(acceptedLog+refusedLog, testCase.leak) {
				t.Fatalf("did not expect the submitted %s value in the audit stream: %q", testCase.name, acceptedLog+refusedLog)
			}
		})
	}
}

// captureOnboardingAudit drives one onboarding request against an app with the
// audit stream on and returns the response together with the security-event
// output it produced. Onboarding runs before the settings pages exist, so it
// uses the plain onboarding app rather than the CSRF-bearing settings context.
func captureOnboardingAudit(t *testing.T, app *fiber.App, authCookie string, path string, form url.Values) (*http.Response, string) {
	t.Helper()

	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)
	var output bytes.Buffer
	log.SetOutput(&output)

	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("onboarding request %s failed: %v", path, err)
	}
	return response, output.String()
}

// TestOnboardingStepsAreAuditedLikeTheSettingsCycleForm is the parity assertion
// the fix exists for. Onboarding writes the same users columns the settings
// cycle form writes — step 1 sets last_period_start, step 2 sets the cycle and
// period lengths — so "were the cycle settings changed?" must be answerable
// from the audit stream whichever surface the owner used. Before this change
// the settings form answered it and onboarding answered nothing, so the filter
// was right exactly when the change came from the surface it knew about.
//
// Completion is the third member: with auto-period-fill on it seeds one
// is_period day row per day of the declared first period, which is a day-entry
// mutation rather than a settings one, so it carries the day_entry target.
func TestOnboardingStepsAreAuditedLikeTheSettingsCycleForm(t *testing.T) {
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{auditLogEnabled: true})
	startDate := services.DateAtLocation(time.Now().In(time.UTC), time.UTC).AddDate(0, 0, -2)

	// First owner: the standalone completion endpoint is only reachable while
	// onboarding is still open and a start date is already stored, which is
	// exactly the state step 1 alone leaves behind.
	firstOwner := createOnboardingTestUser(t, database, "onboarding-audit@example.com", "StrongPass1", false)
	firstCookie := loginAndExtractAuthCookie(t, app, firstOwner.Email, "StrongPass1")

	// A start date outside the accepted window is refused before anything is
	// persisted, and that refusal is itself an audited attempt to change the
	// cycle baseline.
	refusedStep1, refusedStep1Log := captureOnboardingAudit(t, app, firstCookie, "/api/v1/onboarding/steps/1", url.Values{
		"last_period_start": {startDate.AddDate(0, 0, -400).Format("2006-01-02")},
	})
	defer func() { _ = refusedStep1.Body.Close() }()
	if refusedStep1.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400 for an out-of-range onboarding start date, got %d", refusedStep1.StatusCode)
	}
	assertHealthDataMutationAudited(t, refusedStep1Log, "onboarding.cycle_start_update", "denied", "cycle_settings")

	step1, step1Log := captureOnboardingAudit(t, app, firstCookie, "/api/v1/onboarding/steps/1", url.Values{
		"last_period_start": {startDate.Format("2006-01-02")},
	})
	defer func() { _ = step1.Body.Close() }()
	if step1.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for onboarding step 1, got %d", step1.StatusCode)
	}
	assertHealthDataMutationAudited(t, step1Log, "onboarding.cycle_start_update", "success", "cycle_settings")
	if strings.Contains(step1Log, startDate.Format("2006-01-02")) {
		t.Fatalf("did not expect the declared cycle start date in the audit stream: %q", step1Log)
	}

	complete, completeLog := captureOnboardingAudit(t, app, firstCookie, "/api/v1/onboarding/complete", url.Values{})
	defer func() { _ = complete.Body.Close() }()
	if complete.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for onboarding completion, got %d", complete.StatusCode)
	}
	assertHealthDataMutationAudited(t, completeLog, "onboarding.complete", "success", "day_entry")

	// Second owner: step 2 short-circuits to the dashboard once onboarding is
	// closed, so its own legs need an account that has not finished yet.
	secondOwner := createOnboardingTestUser(t, database, "onboarding-audit-step2@example.com", "StrongPass1", false)
	secondCookie := loginAndExtractAuthCookie(t, app, secondOwner.Email, "StrongPass1")

	refusedStep2, refusedStep2Log := captureOnboardingAudit(t, app, secondCookie, "/api/v1/onboarding/steps/2", url.Values{
		"cycle_length":  {"not-a-number"},
		"period_length": {"5"},
	})
	defer func() { _ = refusedStep2.Body.Close() }()
	if refusedStep2.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400 for an unparseable cycle length, got %d", refusedStep2.StatusCode)
	}
	assertHealthDataMutationAudited(t, refusedStep2Log, "onboarding.cycle_update", "denied", "cycle_settings")

	firstStep1, _ := captureOnboardingAudit(t, app, secondCookie, "/api/v1/onboarding/steps/1", url.Values{
		"last_period_start": {startDate.Format("2006-01-02")},
	})
	defer func() { _ = firstStep1.Body.Close() }()
	if firstStep1.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for the second owner's step 1, got %d", firstStep1.StatusCode)
	}

	step2, step2Log := captureOnboardingAudit(t, app, secondCookie, "/api/v1/onboarding/steps/2", url.Values{
		"cycle_length":     {"30"},
		"period_length":    {"5"},
		"auto_period_fill": {"true"},
	})
	defer func() { _ = step2.Body.Close() }()
	if step2.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for onboarding step 2, got %d", step2.StatusCode)
	}
	assertHealthDataMutationAudited(t, step2Log, "onboarding.cycle_update", "success", "cycle_settings")
}

var errAuditedMutationStorage = errors.New("storage refused")

// stubOnboardingRepository refuses every call unless the test supplies a
// replacement, so an onboarding storage-failure arm can be reached without
// disturbing the users table the auth middleware reads.
type stubOnboardingRepository struct {
	findByID  func(ctx context.Context, userID uint) (models.User, error)
	saveStep1 func(ctx context.Context, userID uint, start time.Time) error
	saveStep2 func(ctx context.Context, userID uint, cycleLength int, periodLength int, autoPeriodFill bool, irregularCycle bool, ageGroup string, usageGoal string) error
	complete  func(ctx context.Context, userID uint, startDay time.Time, periodLength int, autoPeriodFill bool) error
}

func (repo stubOnboardingRepository) FindByID(ctx context.Context, userID uint) (models.User, error) {
	if repo.findByID == nil {
		return models.User{}, errAuditedMutationStorage
	}
	return repo.findByID(ctx, userID)
}

func (repo stubOnboardingRepository) SaveOnboardingStep1(ctx context.Context, userID uint, start time.Time) error {
	if repo.saveStep1 == nil {
		return errAuditedMutationStorage
	}
	return repo.saveStep1(ctx, userID, start)
}

func (repo stubOnboardingRepository) SaveOnboardingStep2(ctx context.Context, userID uint, cycleLength int, periodLength int, autoPeriodFill bool, irregularCycle bool, ageGroup string, usageGoal string) error {
	if repo.saveStep2 == nil {
		return errAuditedMutationStorage
	}
	return repo.saveStep2(ctx, userID, cycleLength, periodLength, autoPeriodFill, irregularCycle, ageGroup, usageGoal)
}

func (repo stubOnboardingRepository) CompleteOnboarding(ctx context.Context, userID uint, startDay time.Time, periodLength int, autoPeriodFill bool) error {
	if repo.complete == nil {
		return errAuditedMutationStorage
	}
	return repo.complete(ctx, userID, startDay, periodLength, autoPeriodFill)
}

// stubSettingsRepository implements only the one write the profile handler
// makes; the embedded interface is nil, so any other call would panic loudly
// rather than pass silently.
type stubSettingsRepository struct {
	services.SettingsUserRepository
}

func (stubSettingsRepository) UpdateDisplayName(context.Context, uint, string) error {
	return errAuditedMutationStorage
}

// auditedMutationProbe drives one handler function directly, without the auth
// or language middleware, and returns the response with the audit output it
// produced. It exists for the arms a routed request cannot reach: the
// session-less guard, which AuthRequired refuses before the handler runs, and
// the storage failures, which need a repository that refuses without breaking
// the users table the auth middleware reads.
type auditedMutationProbe struct {
	user        *models.User
	method      string
	path        string
	body        string
	contentType string
}

func (probe auditedMutationProbe) run(t *testing.T, route fiber.Handler) (*http.Response, string) {
	t.Helper()

	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)
	var output bytes.Buffer
	log.SetOutput(&output)

	wrapped := func(c fiber.Ctx) error {
		if probe.user != nil {
			c.Locals(contextUserKey, probe.user)
		}
		return route(c)
	}

	app := fiber.New()
	switch probe.method {
	case http.MethodPost:
		app.Post(probe.path, wrapped)
	case http.MethodPatch:
		app.Patch(probe.path, wrapped)
	default:
		t.Fatalf("unsupported probe method %q", probe.method)
	}

	contentType := probe.contentType
	if contentType == "" {
		contentType = "application/x-www-form-urlencoded"
	}
	request := httptest.NewRequest(probe.method, probe.path, strings.NewReader(probe.body))
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Accept", "application/json")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("probe %s %s failed: %v", probe.method, probe.path, err)
	}
	return response, output.String()
}

func newAuditedMutationTestHandler(onboarding stubOnboardingRepository) *Handler {
	return &Handler{
		auditLogEnabled: true,
		location:        time.UTC,
		onboardingSvc:   services.NewOnboardingService(onboarding),
		settingsService: services.NewSettingsService(stubSettingsRepository{}),
	}
}

// TestAuditedMutationsRecordRefusalsAndStorageFailures covers the arms the
// happy-path regressions above cannot reach: a request that arrives without a
// session, and a write the storage layer refuses. Both are exactly the moments
// an incident review cares about — a mutation attempt that produced no change —
// so neither may be the branch that quietly skips the audit line.
//
// The outcome is asserted per case: a refusal below 500 is "denied", a storage
// failure is "failure", and the profile rows additionally prove the account
// domain holds on the failure paths too.
func TestAuditedMutationsRecordRefusalsAndStorageFailures(t *testing.T) {
	onboardingOwner := &models.User{ID: 3, Role: models.RoleOwner, OnboardingCompleted: false}
	startDate := services.DateAtLocation(time.Now().In(time.UTC), time.UTC).AddDate(0, 0, -2)
	completionOwner := &models.User{ID: 4, Role: models.RoleOwner, OnboardingCompleted: false, LastPeriodStart: &startDate}

	step2Body := "cycle_length=30&period_length=5"
	step1Body := "last_period_start=" + startDate.Format("2006-01-02")

	cases := []struct {
		name       string
		repository stubOnboardingRepository
		user       *models.User
		method     string
		path       string
		body       string
		mediaType  string
		route      func(handler *Handler) fiber.Handler
		action     string
		target     string
		outcome    string
		status     int
		account    bool
	}{
		{
			name:    "onboarding step 1 without a session",
			method:  http.MethodPost,
			path:    "/api/v1/onboarding/steps/1",
			route:   func(handler *Handler) fiber.Handler { return handler.OnboardingStep1 },
			action:  "onboarding.cycle_start_update",
			target:  "cycle_settings",
			outcome: "denied",
			status:  http.StatusUnauthorized,
		},
		{
			name:    "onboarding step 2 without a session",
			method:  http.MethodPost,
			path:    "/api/v1/onboarding/steps/2",
			route:   func(handler *Handler) fiber.Handler { return handler.OnboardingStep2 },
			action:  "onboarding.cycle_update",
			target:  "cycle_settings",
			outcome: "denied",
			status:  http.StatusUnauthorized,
		},
		{
			name:    "onboarding completion without a session",
			method:  http.MethodPost,
			path:    "/api/v1/onboarding/complete",
			route:   func(handler *Handler) fiber.Handler { return handler.OnboardingComplete },
			action:  "onboarding.complete",
			target:  "day_entry",
			outcome: "denied",
			status:  http.StatusUnauthorized,
		},
		{
			name:    "profile update without a session",
			method:  http.MethodPatch,
			path:    "/api/v1/users/current/profile",
			route:   func(handler *Handler) fiber.Handler { return handler.UpdateProfile },
			action:  "settings.profile_update",
			target:  "profile",
			outcome: "denied",
			status:  http.StatusUnauthorized,
			account: true,
		},
		{
			name:      "profile update with an undecodable body",
			user:      onboardingOwner,
			method:    http.MethodPatch,
			path:      "/api/v1/users/current/profile",
			body:      "{",
			mediaType: fiber.MIMEApplicationJSON,
			route:     func(handler *Handler) fiber.Handler { return handler.UpdateProfile },
			action:    "settings.profile_update",
			target:    "profile",
			outcome:   "denied",
			status:    http.StatusBadRequest,
			account:   true,
		},
		{
			name:    "profile update the storage refuses",
			user:    onboardingOwner,
			method:  http.MethodPatch,
			path:    "/api/v1/users/current/profile",
			body:    "display_name=Renamed",
			route:   func(handler *Handler) fiber.Handler { return handler.UpdateProfile },
			action:  "settings.profile_update",
			target:  "profile",
			outcome: "failure",
			status:  http.StatusInternalServerError,
			account: true,
		},
		{
			name:    "onboarding step 1 the storage refuses",
			user:    onboardingOwner,
			method:  http.MethodPost,
			path:    "/api/v1/onboarding/steps/1",
			body:    step1Body,
			route:   func(handler *Handler) fiber.Handler { return handler.OnboardingStep1 },
			action:  "onboarding.cycle_start_update",
			target:  "cycle_settings",
			outcome: "failure",
			status:  http.StatusInternalServerError,
		},
		{
			name:    "onboarding step 2 the storage refuses",
			user:    onboardingOwner,
			method:  http.MethodPost,
			path:    "/api/v1/onboarding/steps/2",
			body:    step2Body,
			route:   func(handler *Handler) fiber.Handler { return handler.OnboardingStep2 },
			action:  "onboarding.cycle_update",
			target:  "cycle_settings",
			outcome: "failure",
			status:  http.StatusInternalServerError,
		},
		{
			// The step-2 columns land and the completion that follows them fails
			// for a reason that is not "steps required", so the mutation is audited
			// as a failure rather than the success its persisted half would suggest.
			name: "onboarding step 2 whose completion fails",
			repository: stubOnboardingRepository{
				saveStep2: func(context.Context, uint, int, int, bool, bool, string, string) error { return nil },
			},
			user:    onboardingOwner,
			method:  http.MethodPost,
			path:    "/api/v1/onboarding/steps/2",
			body:    step2Body,
			route:   func(handler *Handler) fiber.Handler { return handler.OnboardingStep2 },
			action:  "onboarding.cycle_update",
			target:  "cycle_settings",
			outcome: "failure",
			status:  http.StatusInternalServerError,
		},
		{
			name:    "onboarding completion the storage refuses",
			user:    completionOwner,
			method:  http.MethodPost,
			path:    "/api/v1/onboarding/complete",
			route:   func(handler *Handler) fiber.Handler { return handler.OnboardingComplete },
			action:  "onboarding.complete",
			target:  "day_entry",
			outcome: "failure",
			status:  http.StatusInternalServerError,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			handler := newAuditedMutationTestHandler(testCase.repository)
			probe := auditedMutationProbe{
				user:        testCase.user,
				method:      testCase.method,
				path:        testCase.path,
				body:        testCase.body,
				contentType: testCase.mediaType,
			}

			response, logOutput := probe.run(t, testCase.route(handler))
			defer func() { _ = response.Body.Close() }()

			if response.StatusCode != testCase.status {
				t.Fatalf("status = %d, want %d", response.StatusCode, testCase.status)
			}
			if testCase.account {
				assertAccountMutationAudited(t, logOutput, testCase.action, testCase.outcome, testCase.target)
				return
			}
			assertHealthDataMutationAudited(t, logOutput, testCase.action, testCase.outcome, testCase.target)
		})
	}
}
