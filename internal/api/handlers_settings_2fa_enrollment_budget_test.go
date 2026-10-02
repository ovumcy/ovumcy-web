package api

import (
	"context"
	"net/http"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

// enrollmentBudgetTestWindow is the totp.enroll window the wait-it-out
// regression runs under: wide enough that spending the budget and the two
// refused requests after it land well inside it (each request pays a
// default-cost bcrypt for the password), short enough to wait out.
const enrollmentBudgetTestWindow = 10 * time.Second

// sendEnrollment sends PUT /api/v1/users/current/2fa with the correct password
// and code. It reads ctx through the pointer, so a refreshed auth cookie is the
// one sent.
func sendEnrollment(t *testing.T, ctx *settingsSecurityTestContext, setupCookie string, code string) *http.Response {
	t.Helper()
	return send2FARequest(t, *ctx, twoFARequest{
		method:      http.MethodPut,
		contentType: "application/x-www-form-urlencoded",
		body:        url.Values{"password": {"StrongPass1"}, "code": {code}, "csrf_token": {ctx.csrfToken}}.Encode(),
		setupCookie: setupCookie,
		withSession: true,
	})
}

// TestVerifyTOTP2FAEnrollmentBooksAWrongCodeAgainstTheTOTPEnrollBudget pins
// that the enrollment code draws its own totp.enroll budget: with the correct
// password and a wrong code, the limit's worth of attempts is refused one by
// one, the next request is refused as rate limited, and then the correct code
// is refused too and enrolls nothing. The password check passes every time, so
// the only thing that can book these attempts is the wrong code. Once the
// window has passed, the correct code enrolls.
func TestVerifyTOTP2FAEnrollmentBooksAWrongCodeAgainstTheTOTPEnrollBudget(t *testing.T) {
	ctx := newSettingsSecurityTestContextWithOptions(t, "totp-enroll-code-budget@example.com",
		onboardingTestAppOptions{enableCSRF: true, totpEnrollWindow: enrollmentBudgetTestWindow})
	setupCookie, secret := enrollmentSecretFixture(t, ctx)
	wrongCode := invalidTOTPCodeForSkewWindow(t, secret)

	// Every assertion before the wait must run inside the window, or a refusal
	// it expects could be missing only because the first failures had aged out.
	spentFrom := time.Now()
	insideWindow := func(step string) {
		t.Helper()
		if elapsed := time.Since(spentFrom); elapsed >= enrollmentBudgetTestWindow {
			t.Fatalf("%s ran %s after the first booked failure, past the %s window: it measures nothing", step, elapsed, enrollmentBudgetTestWindow)
		}
	}

	for attempt := range services.DefaultTOTPEnrollAttemptsLimit {
		resp := sendEnrollment(t, &ctx, setupCookie, wrongCode)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("wrong code, attempt %d: status = %d, want 401", attempt+1, resp.StatusCode)
		}
	}
	lastFailure := time.Now()

	resp := sendEnrollment(t, &ctx, setupCookie, wrongCode)
	insideWindow("the wrong code past the limit")
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("wrong code after %d attempts: status = %d, want 429", services.DefaultTOTPEnrollAttemptsLimit, resp.StatusCode)
	}

	resp = sendEnrollment(t, &ctx, setupCookie, currentTOTPCode(t, secret))
	insideWindow("the correct code past the limit")
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("correct code after the budget: status = %d, want 429", resp.StatusCode)
	}
	if totpEnabledInDatabase(t, ctx) {
		t.Fatal("a request past the budget enrolled 2FA")
	}

	// A refused request books nothing, so the last failure is the one to outlast.
	time.Sleep(time.Until(lastFailure.Add(enrollmentBudgetTestWindow + 250*time.Millisecond)))

	resp = sendEnrollment(t, &ctx, setupCookie, currentTOTPCode(t, secret))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("correct code after the window: status = %d, want 200", resp.StatusCode)
	}
	if !totpEnabledInDatabase(t, ctx) {
		t.Fatal("the correct code after the window did not enroll 2FA")
	}
}

// TestVerifyTOTP2FAEnrollmentMalformedCodeDrawsNothingFromTheTOTPEnrollBudget
// pins that a missing or wrong-length code is refused before the budget is read
// and is not booked: after several of them the full limit's worth of wrong
// six-digit codes is still answered 401, and the 429 arrives exactly at
// limit+1.
func TestVerifyTOTP2FAEnrollmentMalformedCodeDrawsNothingFromTheTOTPEnrollBudget(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-enroll-malformed-code@example.com")
	setupCookie, secret := enrollmentSecretFixture(t, ctx)
	wrongCode := invalidTOTPCodeForSkewWindow(t, secret)

	for _, malformed := range []string{"", "12345", "1234567", "12345678", "", "1"} {
		resp := sendEnrollment(t, &ctx, setupCookie, malformed)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("malformed code %q: status = %d, want 401", malformed, resp.StatusCode)
		}
	}
	for attempt := range services.DefaultTOTPEnrollAttemptsLimit {
		resp := sendEnrollment(t, &ctx, setupCookie, wrongCode)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("wrong code, attempt %d after the malformed ones: status = %d, want 401 (a malformed code was booked)", attempt+1, resp.StatusCode)
		}
	}
	resp := sendEnrollment(t, &ctx, setupCookie, wrongCode)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("wrong code at limit+1: status = %d, want 429", resp.StatusCode)
	}
}

// TestVerifyTOTP2FAEnrollmentEnrollsOnAFreshBudget is the positive control for
// the budget regression above: the correct password and the correct code on an
// unspent totp.enroll budget enroll 2FA.
func TestVerifyTOTP2FAEnrollmentEnrollsOnAFreshBudget(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-enroll-fresh-budget@example.com")
	setupCookie, secret := enrollmentSecretFixture(t, ctx)

	resp := sendEnrollment(t, &ctx, setupCookie, currentTOTPCode(t, secret))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("correct code on a fresh budget: status = %d, want 200", resp.StatusCode)
	}
	if !totpEnabledInDatabase(t, ctx) {
		t.Fatal("the correct code on a fresh budget did not enroll 2FA")
	}
}

// TestVerifyTOTP2FAEnrollmentCommittedEnrollmentResetsTheTOTPEnrollBudget pins
// the reset: one failure short of the limit, a committed enrollment clears the
// count, so after 2FA is turned off again a new enrollment gets the whole
// budget. Without the reset the second wrong code would already be refused 429.
func TestVerifyTOTP2FAEnrollmentCommittedEnrollmentResetsTheTOTPEnrollBudget(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-enroll-budget-reset@example.com")
	setupCookie, secret := enrollmentSecretFixture(t, ctx)
	wrongCode := invalidTOTPCodeForSkewWindow(t, secret)

	for attempt := range services.DefaultTOTPEnrollAttemptsLimit - 1 {
		resp := sendEnrollment(t, &ctx, setupCookie, wrongCode)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("wrong code, attempt %d: status = %d, want 401", attempt+1, resp.StatusCode)
		}
	}
	resp := sendEnrollment(t, &ctx, setupCookie, currentTOTPCode(t, secret))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("correct code one short of the limit: status = %d, want 200", resp.StatusCode)
	}

	// Turn 2FA off out of band (its own route draws totp.disable, not this
	// budget) and carry the session past the revocation that came with it.
	ctx.refreshAuthCookie(t)
	if err := getTOTPServiceForTest(ctx.database).DisableTOTP(context.Background(), ctx.user.ID, ctx.user.AuthSessionVersion); err != nil {
		t.Fatalf("DisableTOTP: %v", err)
	}
	ctx.refreshAuthCookie(t)

	setupCookie, secret = enrollmentSecretFixture(t, ctx)
	wrongCode = invalidTOTPCodeForSkewWindow(t, secret)
	for attempt := range services.DefaultTOTPEnrollAttemptsLimit {
		resp := sendEnrollment(t, &ctx, setupCookie, wrongCode)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("wrong code after a committed enrollment, attempt %d: status = %d, want 401 (the enrollment did not reset totp.enroll)", attempt+1, resp.StatusCode)
		}
	}
	resp = sendEnrollment(t, &ctx, setupCookie, wrongCode)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("wrong code past the fresh limit: status = %d, want 429", resp.StatusCode)
	}
}

// TestVerifyTOTP2FAEnrollmentRefusedWriteDoesNotResetTheTOTPEnrollBudget is the
// other half of the reset: a correct code whose enrollment a revocation refused
// mid-request proved nothing lasting, so it keeps the count it found.
func TestVerifyTOTP2FAEnrollmentRefusedWriteDoesNotResetTheTOTPEnrollBudget(t *testing.T) {
	ctx := newTOTPSettingsContext(t, "totp-enroll-refused-write@example.com")
	setupCookie, secret := enrollmentSecretFixture(t, ctx)
	wrongCode := invalidTOTPCodeForSkewWindow(t, secret)

	for attempt := range services.DefaultTOTPEnrollAttemptsLimit - 1 {
		resp := sendEnrollment(t, &ctx, setupCookie, wrongCode)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("wrong code, attempt %d: status = %d, want 401", attempt+1, resp.StatusCode)
		}
	}

	const revokeBeforeEnable = "test:totp-enroll-revoke-before-cas"
	var revoked atomic.Bool
	if err := ctx.database.Callback().Update().Before("gorm:update").Register(revokeBeforeEnable, func(tx *gorm.DB) {
		updates, ok := tx.Statement.Dest.(map[string]any)
		if !ok {
			return
		}
		if _, enablesTOTP := updates["totp_enabled"]; !enablesTOTP || !revoked.CompareAndSwap(false, true) {
			return
		}
		if err := tx.Session(&gorm.Session{NewDB: true}).Exec("UPDATE users SET auth_session_version = auth_session_version + 1 WHERE id = ?", ctx.user.ID).Error; err != nil {
			t.Errorf("simulate the mid-request revocation: %v", err)
		}
	}); err != nil {
		t.Fatalf("register the revocation callback: %v", err)
	}
	t.Cleanup(func() { _ = ctx.database.Callback().Update().Remove(revokeBeforeEnable) })

	resp := sendEnrollment(t, &ctx, setupCookie, currentTOTPCode(t, secret))
	if !revoked.Load() {
		t.Fatal("anchor: the correct-code request never reached EnableTOTP's write")
	}
	if resp.StatusCode == http.StatusOK {
		t.Fatal("an enrollment whose compare-and-set failed answered 200")
	}
	if totpEnabledInDatabase(t, ctx) {
		t.Fatal("an enrollment whose compare-and-set failed still turned 2FA on")
	}

	// The refusal signed this device out; sign the same owner back in at the
	// version it kept, from the same client address.
	ctx.refreshAuthCookie(t)

	// One failure short of the limit was spent before the refused write. If that
	// write reset the count, the correct code below would enroll.
	resp = sendEnrollment(t, &ctx, setupCookie, wrongCode)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("the failure that reaches the limit: status = %d, want 401", resp.StatusCode)
	}
	resp = sendEnrollment(t, &ctx, setupCookie, currentTOTPCode(t, secret))
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("correct code after the refused write: status = %d, want 429 (the refused write reset totp.enroll)", resp.StatusCode)
	}
}
