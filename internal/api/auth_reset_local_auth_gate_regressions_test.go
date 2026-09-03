package api

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
)

// The reset flow mints THREE kinds of token, distinguished by the token's own
// SIGNED Purpose claim (PRIV-4) — never by a cookie-carried bool, which is not
// part of the signed token and cannot be trusted for this decision:
//
//   - PasswordResetTokenPurposeRecovery — the email + recovery-code flow.
//     ForgotPassword refuses to START one once local public auth is off, so
//     its redeem must stop just as sharply.
//   - PasswordResetTokenPurposeForcedLocal — minted after a LOCAL password
//     authenticates against an account carrying MustChangePassword: the plain
//     login route, and OIDC link-confirm's own password challenge (A1 — both
//     go through LoginService.Authenticate, which never checks an OIDC gate).
//     This is gated exactly like recovery: the factor that produced it is the
//     one the operator disabled.
//   - PasswordResetTokenPurposeForcedOIDC — minted by CompleteOIDCLogin
//     without ever checking a local password. An oidc_only instance
//     legitimately mints and must keep redeeming these with local sign-in
//     off — refusing them would strand an owner whose account carries
//     MustChangePassword with no way to clear it.
//
// A token that fails to parse for its own reasons — expired, malformed,
// unrecognised/legacy purpose — must NOT be answered with the local-disabled
// refusal: that collapses routine expiry (the default outcome of every
// forced-from-OIDC reset a user takes longer than 30 minutes to complete)
// into an operator-configuration message and floods the security log with an
// event that never happened. Those cases get the ordinary invalid-token
// answer instead, from CompleteReset (redeem) or buildResetPasswordPageData
// (page) independently re-parsing the same token.
//
// Every redeem test below mints its cookie through the real production paths
// while local public auth is still ON, then flips the switch afterwards,
// which is the exact sequence PRIV-4 describes.

func TestRecoveryResetRedeemRefusesOnceLocalPublicAuthIsOff(t *testing.T) {
	app, database, stub := newLocalAuthGateTestApp(t)
	user := createOnboardingTestUser(t, database, "reset-gate-recovery@example.com", "StrongPass1", true)

	recoveryCode := mustSetRecoveryCodeForUser(t, database, user.ID)
	resetCookieValue := requestResetCookieByRecoveryCode(t, app, user.Email, recoveryCode, "StrongPass1")

	stub.localPublicAuthEnabled = false

	response := redeemResetCookie(t, app, resetCookieValue, "EvenStronger2")
	assertStatusCode(t, response, http.StatusForbidden)
	if got := readAPIError(t, response.Body); got != "local recovery unavailable" {
		t.Fatalf("expected %q, got %q", "local recovery unavailable", got)
	}
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
		t.Fatalf("expected a refused redeem to mint no session, got %#v", authCookie)
	}

	// The refusal is a refusal, not a delay: the account's posture is untouched.
	var stored models.User
	if err := database.First(&stored, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if stored.PasswordHash != user.PasswordHash {
		t.Fatal("expected the refused redeem to leave the password hash unchanged")
	}
	if stored.AuthSessionVersion != user.AuthSessionVersion {
		t.Fatalf("expected auth_session_version to stay %d, got %d", user.AuthSessionVersion, stored.AuthSessionVersion)
	}
}

// TestForcedResetFromLocalLoginRedeemRefusedWhenLocalPublicAuthIsOff is the
// PRIV-4 regression itself. Before the Purpose split, ANY token carrying the
// cookie's "forced" bool survived this gate — including one minted by the
// plain LOCAL login route, whose own factor is exactly what the operator just
// disabled. This test used to assert the opposite (a minted session); it now
// pins the fix: the redeem must refuse, mint no session, and leave the
// account's password hash and session version untouched.
func TestForcedResetFromLocalLoginRedeemRefusedWhenLocalPublicAuthIsOff(t *testing.T) {
	app, database, stub := newLocalAuthGateTestApp(t)
	user := createOnboardingTestUser(t, database, "reset-gate-forced-local@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("must_change_password", true).Error; err != nil {
		t.Fatalf("mark user must_change_password: %v", err)
	}

	resetCookieValue := forcedLocalResetCookieFromLogin(t, app, user.Email, "StrongPass1")

	// The login above is itself a successful local authentication, and
	// AuthService.rehashPasswordIfStale opportunistically upgrades a
	// below-cost bcrypt hash on ANY successful login — legitimate, unrelated
	// to the reset flow under test. Snapshot the hash the mint left behind
	// (never touching AuthSessionVersion, which rehashing never bumps) so the
	// comparison below isolates what the REFUSED redeem itself did, not what
	// minting the token already did.
	var afterMint models.User
	if err := database.First(&afterMint, user.ID).Error; err != nil {
		t.Fatalf("reload user after mint: %v", err)
	}

	stub.localPublicAuthEnabled = false

	response := redeemResetCookie(t, app, resetCookieValue, "EvenStronger2")
	assertStatusCode(t, response, http.StatusForbidden)
	if got := readAPIError(t, response.Body); got != "local recovery unavailable" {
		t.Fatalf("expected %q, got %q", "local recovery unavailable", got)
	}
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
		t.Fatalf("expected a refused redeem to mint no session, got %#v", authCookie)
	}

	var stored models.User
	if err := database.First(&stored, user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if stored.PasswordHash != afterMint.PasswordHash {
		t.Fatal("expected the refused redeem to leave the password hash unchanged")
	}
	if stored.AuthSessionVersion != afterMint.AuthSessionVersion {
		t.Fatalf("expected auth_session_version to stay %d, got %d", afterMint.AuthSessionVersion, stored.AuthSessionVersion)
	}
}

// TestForcedResetFromLocalLoginRedeemSucceedsWhenLocalPublicAuthIsOn is the
// sibling sanity check: the forced-from-LOCAL purpose is gated on the
// instance toggle, not refused outright. With local sign-in left on (the
// ordinary configuration), the same mint must still redeem and mint a
// session — this is the account's own operator-forced rotation, not a
// security incident.
func TestForcedResetFromLocalLoginRedeemSucceedsWhenLocalPublicAuthIsOn(t *testing.T) {
	app, database, _ := newLocalAuthGateTestApp(t)
	user := createOnboardingTestUser(t, database, "reset-gate-forced-local-on@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("must_change_password", true).Error; err != nil {
		t.Fatalf("mark user must_change_password: %v", err)
	}

	resetCookieValue := forcedLocalResetCookieFromLogin(t, app, user.Email, "StrongPass1")

	response := redeemResetCookie(t, app, resetCookieValue, "EvenStronger2")
	assertStatusCode(t, response, http.StatusOK)
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie == nil || strings.TrimSpace(authCookie.Value) == "" {
		t.Fatalf("expected the forced-from-local redeem to mint a session while local sign-in stays on, got %#v", authCookie)
	}
}

// TestForcedResetFromOIDCRedeemSurvivesLocalPublicAuthBeingOff locks the
// half of PRIV-4 that must NOT change: a token minted by CompleteOIDCLogin —
// which authenticates purely through the OIDC exchange, never a local
// password — has to keep redeeming on an oidc_only instance, or an owner
// carrying MustChangePassword would have no way to clear it.
func TestForcedResetFromOIDCRedeemSurvivesLocalPublicAuthBeingOff(t *testing.T) {
	app, database, stub := newLocalAuthGateTestApp(t)
	user := createOnboardingTestUser(t, database, "reset-gate-forced-oidc@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("must_change_password", true).Error; err != nil {
		t.Fatalf("mark user must_change_password: %v", err)
	}
	user.MustChangePassword = true

	resetCookieValue := forcedOIDCResetCookieFromCallback(t, app, stub, user)

	stub.localPublicAuthEnabled = false

	response := redeemResetCookie(t, app, resetCookieValue, "EvenStronger2")
	assertStatusCode(t, response, http.StatusOK)
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie == nil || strings.TrimSpace(authCookie.Value) == "" {
		t.Fatalf("expected the forced-from-OIDC redeem to mint a session even with local sign-in off, got %#v", authCookie)
	}
}

// TestForcedResetFromOIDCExpiredTokenGetsInvalidTokenNotLocalAuthDisabled
// pins the fix for the finding raised against the first version of this
// change: PasswordResetTokenRefusedByLocalAuthGate must not collapse EVERY
// parse failure into the local-disabled refusal. A forced-from-OIDC token
// that simply outlives its 30-minute TTL is the DEFAULT outcome of a slow
// forced-reset flow on an oidc_only instance — it must get the ordinary
// invalid-token answer (the one CompleteReset gives downstream), not a
// message blaming the instance's local-sign-in toggle, and it must not log a
// local-recovery-disabled security event for what is a routine expiry.
func TestForcedResetFromOIDCExpiredTokenGetsInvalidTokenNotLocalAuthDisabled(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)
	var output bytes.Buffer
	log.SetOutput(&output)

	stub := newStubOIDCWorkflowService(true)
	stub.localPublicAuthEnabled = false
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure:    true,
		oidcService:     stub,
		auditLogEnabled: true,
	})
	user := createOnboardingTestUser(t, database, "reset-gate-forced-oidc-expired@example.com", "StrongPass1", true)

	expiredToken, err := services.BuildPasswordResetToken(
		[]byte(testAppSecretKey), user.ID, user.PasswordHash,
		services.PasswordResetTokenPurposeForcedOIDC, time.Minute, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("BuildPasswordResetToken: %v", err)
	}
	resetCookieValue := mustSealResetCookieValueForTest(t, []byte(testAppSecretKey), expiredToken)

	response := redeemResetCookie(t, app, resetCookieValue, "EvenStronger2")
	assertStatusCode(t, response, http.StatusBadRequest)
	if got := readAPIError(t, response.Body); got != "invalid reset token" {
		t.Fatalf("expected %q, got %q", "invalid reset token", got)
	}
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
		t.Fatalf("expected a refused redeem to mint no session, got %#v", authCookie)
	}

	logged := output.String()
	if strings.Contains(logged, "reason=\"local recovery unavailable\"") {
		t.Fatalf("expected no local-recovery-disabled security event for a routine token expiry, got %q", logged)
	}
	if !strings.Contains(logged, "reason=\"invalid reset token\"") {
		t.Fatalf("expected the security log to record the accurate invalid-reset-token reason, got %q", logged)
	}
}

// TestLegacyOrUnrecognisedPurposeResetTokenRedeemRefusedAsInvalid covers the
// documented migration behaviour: every reset token minted before this split
// carried the single now-unrecognised "password_reset" Purpose. The
// redeem-time allow-list refuses it — same as any other unparsable token —
// and CompleteReset's own collapse of every parse error to
// ErrInvalidResetToken is what the caller sees, regardless of the instance
// toggle (this app leaves it at the default ON).
func TestLegacyOrUnrecognisedPurposeResetTokenRedeemRefusedAsInvalid(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "reset-gate-legacy-purpose@example.com", "StrongPass1", true)

	legacyToken := mustSignResetTokenForTest(t, user.ID, user.PasswordHash, time.Now().Add(10*time.Minute), time.Now())
	resetCookieValue := mustSealResetCookieValueForTest(t, []byte(testAppSecretKey), legacyToken)

	response := redeemResetCookie(t, app, resetCookieValue, "EvenStronger2")
	assertStatusCode(t, response, http.StatusBadRequest)
	if got := readAPIError(t, response.Body); got != "invalid reset token" {
		t.Fatalf("expected %q, got %q", "invalid reset token", got)
	}
}

// TestOIDCLinkConfirmForcedResetMintsLocalPurposeRefusedWhenLocalPublicAuthIsOff
// is the direct regression for the security review's amendment A1:
// handlers_auth_oidc_link_confirm.go's own forced-reset mint (:189) must
// carry PasswordResetTokenPurposeForcedLocal, not ...ForcedOIDC, because the
// handler authenticates the target with a LOCAL password
// (LoginService.Authenticate) and never checks an instance-wide OIDC gate.
// Had it been mislabelled OIDC, this token would have bypassed the
// local-sign-in gate exactly like PRIV-4's original local-login hole; this
// test would then see 200 instead of the refusal asserted below.
func TestOIDCLinkConfirmForcedResetMintsLocalPurposeRefusedWhenLocalPublicAuthIsOff(t *testing.T) {
	app, database, stub := newLocalAuthGateTestApp(t)
	user := createOnboardingTestUser(t, database, "reset-gate-link-confirm@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("must_change_password", true).Error; err != nil {
		t.Fatalf("mark user must_change_password: %v", err)
	}

	resetCookieValue := forcedLocalResetCookieFromLinkConfirm(t, app, user, "StrongPass1")

	stub.localPublicAuthEnabled = false

	response := redeemResetCookie(t, app, resetCookieValue, "EvenStronger2")
	assertStatusCode(t, response, http.StatusForbidden)
	if got := readAPIError(t, response.Body); got != "local recovery unavailable" {
		t.Fatalf("expected %q, got %q", "local recovery unavailable", got)
	}
	if authCookie := responseCookie(response.Cookies(), authCookieName); authCookie != nil && strings.TrimSpace(authCookie.Value) != "" {
		t.Fatalf("expected a refused redeem to mint no session, got %#v", authCookie)
	}
}

// TestResetPasswordPageFollowsTheRedeemGate is the N+1 coverage A2 mandates:
// ShowResetPasswordPage must reach the SAME decision as ResetPassword for
// every token kind, including the expired-forced-OIDC case the finding
// above pins for the redeem gate — the page must not redirect to /login
// (blaming the instance toggle) for a token whose only problem is its own
// expiry.
func TestResetPasswordPageFollowsTheRedeemGate(t *testing.T) {
	app, database, stub := newLocalAuthGateTestApp(t)
	user := createOnboardingTestUser(t, database, "reset-gate-page@example.com", "StrongPass1", true)

	recoveryCode := mustSetRecoveryCodeForUser(t, database, user.ID)
	recoveryCookie := requestResetCookieByRecoveryCode(t, app, user.Email, recoveryCode, "StrongPass1")

	forcedLocalUser := createOnboardingTestUser(t, database, "reset-gate-page-forced-local@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", forcedLocalUser.ID).Update("must_change_password", true).Error; err != nil {
		t.Fatalf("mark user must_change_password: %v", err)
	}
	forcedLocalCookie := forcedLocalResetCookieFromLogin(t, app, forcedLocalUser.Email, "StrongPass1")

	forcedOIDCUser := createOnboardingTestUser(t, database, "reset-gate-page-forced-oidc@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", forcedOIDCUser.ID).Update("must_change_password", true).Error; err != nil {
		t.Fatalf("mark user must_change_password: %v", err)
	}
	forcedOIDCUser.MustChangePassword = true
	forcedOIDCCookie := forcedOIDCResetCookieFromCallback(t, app, stub, forcedOIDCUser)

	expiredForcedOIDCToken, err := services.BuildPasswordResetToken(
		[]byte(testAppSecretKey), forcedOIDCUser.ID, forcedOIDCUser.PasswordHash,
		services.PasswordResetTokenPurposeForcedOIDC, time.Minute, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("BuildPasswordResetToken: %v", err)
	}
	expiredForcedOIDCCookie := mustSealResetCookieValueForTest(t, []byte(testAppSecretKey), expiredForcedOIDCToken)

	stub.localPublicAuthEnabled = false

	testCases := []struct {
		name       string
		cookie     string
		wantStatus int
	}{
		{name: "recovery token", cookie: recoveryCookie, wantStatus: http.StatusSeeOther},
		{name: "no token", cookie: "", wantStatus: http.StatusSeeOther},
		{name: "forced-local token", cookie: forcedLocalCookie, wantStatus: http.StatusSeeOther},
		{name: "forced-oidc token", cookie: forcedOIDCCookie, wantStatus: http.StatusOK},
		{name: "expired forced-oidc token", cookie: expiredForcedOIDCCookie, wantStatus: http.StatusOK},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/reset-password", nil)
			if testCase.cookie != "" {
				request.Header.Set("Cookie", resetPasswordCookieName+"="+testCase.cookie)
			}
			response := mustAppResponse(t, app, request)
			assertStatusCode(t, response, testCase.wantStatus)
			if testCase.wantStatus == http.StatusSeeOther {
				if location := response.Header.Get("Location"); location != "/login" {
					t.Fatalf("expected redirect to /login, got %q", location)
				}
			}
			// A cookie the request never presented is never retracted: a bare
			// visit must not be answered with a Set-Cookie for a value nobody
			// sent, which is the rule the TOTP readers already follow.
			if testCase.cookie == "" {
				if retracted := responseCookie(response.Cookies(), resetPasswordCookieName); retracted != nil {
					t.Fatalf("expected no reset-password cookie header on a request that carried none, got %#v", retracted)
				}
			}
		})
	}

	// The expired-forced-OIDC case renders the ordinary invalid-token page —
	// the same answer the redeem gate gives — not a silent /login bounce that
	// would misattribute the refusal to the instance toggle.
	expiredRequest := httptest.NewRequest(http.MethodGet, "/reset-password", nil)
	expiredRequest.Header.Set("Cookie", resetPasswordCookieName+"="+expiredForcedOIDCCookie)
	expiredResponse := mustAppResponse(t, app, expiredRequest)
	body := mustReadBodyString(t, expiredResponse.Body)
	assertBodyContainsAll(t, body,
		bodyStringMatch{fragment: `data-reset-notice="invalid-token"`, message: "expected invalid-token notice for an expired forced-OIDC token"},
	)
}

func newLocalAuthGateTestApp(t *testing.T) (*fiber.App, *gorm.DB, *stubOIDCWorkflowService) {
	t.Helper()

	stub := newStubOIDCWorkflowService(true)
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  stub,
	})
	return app, database, stub
}

func redeemResetCookie(t *testing.T, app *fiber.App, resetCookieValue string, password string) *http.Response {
	t.Helper()

	form := url.Values{
		"password":         {password},
		"confirm_password": {password},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/password-resets/redeem", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", resetPasswordCookieName+"="+resetCookieValue)
	return mustAppResponse(t, app, request)
}

// forcedLocalResetCookieFromLogin drives the real forced-reset path rather
// than sealing a cookie by hand, so the payload under test is the one
// production writes: the plain login route authenticates with a LOCAL
// password, so LoginService mints PasswordResetTokenPurposeForcedLocal.
func forcedLocalResetCookieFromLogin(t *testing.T, app *fiber.App, email string, password string) string {
	t.Helper()

	form := url.Values{"email": {email}, "password": {password}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusSeeOther)

	cookieValue := responseCookieValue(response.Cookies(), resetPasswordCookieName)
	if cookieValue == "" {
		t.Fatal("expected the forced-reset login to seal a reset-password cookie")
	}
	return cookieValue
}

// forcedLocalResetCookieFromLinkConfirm drives OIDC link-confirm's own LOCAL
// password challenge for a user carrying MustChangePassword (A1). The
// resulting mint must carry PasswordResetTokenPurposeForcedLocal even though
// the surrounding flow is nominally "OIDC": link-confirm authenticates the
// target through LoginService.Authenticate, the same local-password check the
// plain login route makes, and never consults an OIDC-specific gate before
// minting.
func forcedLocalResetCookieFromLinkConfirm(t *testing.T, app *fiber.App, user models.User, password string) string {
	t.Helper()

	pendingPayload, err := newOIDCLinkPendingPayload(time.Now().UTC(), user.ID, "https://idp.example", "subject-gate-test", user.Email)
	if err != nil {
		t.Fatalf("newOIDCLinkPendingPayload: %v", err)
	}
	cookie := sealLinkPendingCookieForTest(t, pendingPayload)

	request := httptest.NewRequest(http.MethodPost, oidcLinkConfirmPath, strings.NewReader(url.Values{
		"password": {password},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", oidcLinkPendingCookieName+"="+cookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/reset-password" {
		t.Fatalf("expected link-confirm to route the forced-reset target to /reset-password, got %q", location)
	}

	cookieValue := responseCookieValue(response.Cookies(), resetPasswordCookieName)
	if cookieValue == "" {
		t.Fatal("expected link-confirm to seal a reset-password cookie for a forced-reset target")
	}
	return cookieValue
}

// forcedOIDCResetCookieFromCallback drives the OIDC callback for a REAL
// persisted user carrying MustChangePassword, so the resulting token can be
// independently redeemed afterward (ResolveUserByResetToken needs a live
// row). The callback authenticates purely through the OIDC exchange the stub
// stands in for — no local password is ever checked — so the mint must carry
// PasswordResetTokenPurposeForcedOIDC.
func forcedOIDCResetCookieFromCallback(t *testing.T, app *fiber.App, stub *stubOIDCWorkflowService, user models.User) string {
	t.Helper()

	startResponse := mustAppResponse(t, app, httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil))
	stateCookie := responseCookie(startResponse.Cookies(), oidcStateCookieName)
	if stateCookie == nil {
		t.Fatal("expected OIDC state cookie from start flow")
	}

	stub.result = services.OIDCLoginResult{User: user}

	callbackRequest := httptest.NewRequest(http.MethodPost, security.OIDCCallbackPath, strings.NewReader(url.Values{
		"state": {stub.lastStartState},
		"code":  {"provider-code"},
	}.Encode()))
	callbackRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	callbackRequest.Header.Set("Cookie", stateCookie.String())

	callbackResponse := mustAppResponse(t, app, callbackRequest)
	assertStatusCode(t, callbackResponse, http.StatusSeeOther)
	if location := callbackResponse.Header.Get("Location"); location != "/reset-password" {
		t.Fatalf("expected the OIDC callback to route the forced-reset user to /reset-password, got %q", location)
	}

	cookieValue := responseCookieValue(callbackResponse.Cookies(), resetPasswordCookieName)
	if cookieValue == "" {
		t.Fatal("expected the OIDC callback to seal a reset-password cookie for a forced-reset user")
	}
	return cookieValue
}
