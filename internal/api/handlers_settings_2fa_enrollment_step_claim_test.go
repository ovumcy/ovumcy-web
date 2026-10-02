package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/services"
	"github.com/pquerna/otp/totp"
)

// The code that confirms a 2FA enrollment has been shown to the owner and typed
// into a form; it is a spent one-time password. The sign-in challenge refuses a
// step it has already accepted (it claims the step against the account's last
// used one), so the enrollment confirmation has to leave that step claimed too:
// otherwise the very code that enabled the second factor opens the first
// sign-in after it, for whoever observed it, inside its ±1-step validity
// window.
//
// Both tests drive the real routes end to end — enrollment through
// PUT /api/v1/users/current/2fa, the password step through POST
// /api/v1/sessions, the second step through POST /api/v1/sessions/2fa-challenge
// — so the production handler, service and repository decide every answer.
//
// Clock: there is no clock seam on either path; both read time.Now(). The
// enrollment code C is generated from one instant at step S. The challenge
// accepts steps current-1..current+1, and the whole test runs well inside one
// 30-second step, so at the challenge the current step is S or S+1: C is still
// inside the window either way, so the refusal the repro demands can only come
// from a step claim, never from C having aged out. The control's code is for
// step S+1, which is inside the window whether the current step is S or S+1
// and is strictly greater than S, so a correct claim at enrollment must not
// refuse it.

type totpEnrollmentStepClaimFixture struct {
	ctx       settingsSecurityTestContext
	rawSecret string
	enrolled  time.Time
}

// enrollTOTPThroughSettings enrolls the context's owner with the code for the
// instant it returns, through the real enrollment route.
func enrollTOTPThroughSettings(t *testing.T, email string) totpEnrollmentStepClaimFixture {
	t.Helper()
	ctx := newTOTPSettingsContext(t, email)

	key, err := services.NewTOTPService(&dbUserRepoForTest{ctx.database}, []byte("test-secret-key"), nil).GenerateSetupKey("Ovumcy", ctx.user.Email)
	if err != nil {
		t.Fatalf("GenerateSetupKey: %v", err)
	}
	setupCookie := sealTOTPSetupCookieForTest(t, []byte("test-secret-key"), ctx.user.ID, key.Secret())

	enrolled := time.Now()
	code, err := totp.GenerateCode(key.Secret(), enrolled)
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	response := totpEnrollmentRequest(t, ctx, code, setupCookie)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusSeeOther {
		t.Fatalf("enrollment status = %d, want 200 or 303", response.StatusCode)
	}
	if !totpStateForUser(t, ctx, ctx.user.ID).TOTPEnabled {
		t.Fatal("enrollment did not enable TOTP — the premise of the sign-in check is broken")
	}
	return totpEnrollmentStepClaimFixture{ctx: ctx, rawSecret: key.Secret(), enrolled: enrolled}
}

// passwordStepPendingCookie runs the password step of a fresh sign-in and
// returns the Cookie header (pending second-factor cookie plus the CSRF cookie)
// and the CSRF token the challenge step needs.
func passwordStepPendingCookie(t *testing.T, fixture totpEnrollmentStepClaimFixture) (string, string) {
	t.Helper()
	app := fixture.ctx.app
	csrfToken, csrfCookieHeader := extractCSRFCookieAndToken(t, app)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions",
		strings.NewReader(`{"email":"`+fixture.ctx.user.Email+`","password":"StrongPass1"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrfToken)
	request.Header.Set("Cookie", csrfCookieHeader)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("password step failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if auth := responseCookie(response.Cookies(), authCookieName); auth != nil && auth.Value != "" {
		t.Fatal("the password step issued a session on a TOTP account")
	}
	pending := responseCookie(response.Cookies(), totpPendingCookieName)
	if pending == nil || pending.Value == "" {
		t.Fatalf("expected a pending second-factor cookie, status %d", response.StatusCode)
	}
	return joinCookieHeader(cookiePair(pending), csrfCookieHeader), csrfToken
}

func challengeTOTPCode(t *testing.T, fixture totpEnrollmentStepClaimFixture, pendingCookie string, csrfToken string, code string) (int, string, bool) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/2fa-challenge", strings.NewReader(`{"code":"`+code+`"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-CSRF-Token", csrfToken)
	request.Header.Set("Cookie", pendingCookie)
	response := mustAppResponse(t, fixture.ctx.app, request)
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read challenge response: %v", err)
	}
	auth := responseCookie(response.Cookies(), authCookieName)
	return response.StatusCode, string(body), auth != nil && auth.Value != ""
}

func TestTOTPEnrollmentCodeCannotPassFirstLoginChallenge(t *testing.T) {
	fixture := enrollTOTPThroughSettings(t, "totp-enroll-replay@example.com")
	enrollmentCode, err := totp.GenerateCode(fixture.rawSecret, fixture.enrolled)
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	// A code that collides with a neighbouring step's code would be accepted as
	// that step, not replayed; one in a million, but it would read as the defect.
	for _, offset := range []time.Duration{-30 * time.Second, 30 * time.Second} {
		neighbour, neighbourErr := totp.GenerateCode(fixture.rawSecret, fixture.enrolled.Add(offset))
		if neighbourErr != nil {
			t.Fatalf("GenerateCode: %v", neighbourErr)
		}
		if neighbour == enrollmentCode {
			t.Skip("enrollment code collides with a neighbouring step's code; the replay cannot be told apart")
		}
	}

	pendingCookie, csrfToken := passwordStepPendingCookie(t, fixture)
	status, body, sessionIssued := challengeTOTPCode(t, fixture, pendingCookie, csrfToken, enrollmentCode)
	t.Logf("replayed enrollment code: status=%d body=%s totp_last_used_step=%d",
		status, strings.TrimSpace(body), totpStateForUser(t, fixture.ctx, fixture.ctx.user.ID).TOTPLastUsedStep)
	if sessionIssued {
		t.Fatal("the code that confirmed enrollment passed the first sign-in challenge and minted a session")
	}
	assertTOTPChallengeAnswer(t, status, body, http.StatusUnauthorized, "totp invalid code", "replayed enrollment code")
}

func TestTOTPEnrollmentLaterStepCodePassesFirstLoginChallenge(t *testing.T) {
	fixture := enrollTOTPThroughSettings(t, "totp-enroll-next-step@example.com")
	nextStepCode, err := totp.GenerateCode(fixture.rawSecret, fixture.enrolled.Add(30*time.Second))
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}

	pendingCookie, csrfToken := passwordStepPendingCookie(t, fixture)
	status, body, sessionIssued := challengeTOTPCode(t, fixture, pendingCookie, csrfToken, nextStepCode)
	if !sessionIssued {
		t.Fatalf("a code for the step after enrollment was refused: status=%d body=%s", status, strings.TrimSpace(body))
	}
}
