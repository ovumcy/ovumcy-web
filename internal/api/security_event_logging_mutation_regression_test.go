package api

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
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
