package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

func TestRecoveryCodePageRedirectsToDashboardWhenCookieMissing(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "recovery-route-missing-cookie@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/recovery-code", nil)
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("recovery-code request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/dashboard" {
		t.Fatalf("expected redirect to /dashboard, got %q", location)
	}
}

func TestRecoveryCodePageRejectsCookieFromDifferentUser(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	userB := createOnboardingTestUser(t, database, "recovery-cookie-user-b@example.com", "StrongPass1", true)
	authCookieUserB := loginAndExtractAuthCookie(t, app, userB.Email, "StrongPass1")
	_, recoveryCookieUserA := registerAndExtractRecoveryCookies(
		t,
		app,
		"recovery-cookie-user-a@example.com",
		"StrongPass1",
	)

	if recoveryCookieUserA == "" {
		t.Fatalf("expected recovery cookie for user A")
	}

	request := httptest.NewRequest(http.MethodGet, "/recovery-code", nil)
	request.Header.Set("Cookie", authCookieUserB+"; "+recoveryCodeCookieName+"="+recoveryCookieUserA)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("recovery-code request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/dashboard" {
		t.Fatalf("expected redirect to /dashboard, got %q", location)
	}

	cleared := responseCookie(response.Cookies(), recoveryCodeCookieName)
	if cleared == nil {
		t.Fatalf("expected invalid recovery cookie to be cleared")
	}
	if cleared.Value != "" {
		t.Fatalf("expected cleared recovery cookie value, got %q", cleared.Value)
	}
}

// TestRecoveryCodePageRefusesUnattributedRecoveryCookie is the recovery-code arm
// of the reveal contract that TestCalendarFeedRevealRefusesUnattributedCookie
// pins for the calendar feed: a sealed payload carrying `uid` 0 names no
// account, so the page must refuse it outright instead of skipping the owner
// comparison for want of an operand — otherwise the code renders for whichever
// session presents the cookie.
//
// Both payloads here are sealed under the app's own secret and open cleanly, so
// only the owner-scoping guard separates them. The attributed one is the
// positive anchor: it proves the page still reveals to the account it was minted
// for, so the refusal below is not just a page that shows nothing.
func TestRecoveryCodePageRefusesUnattributedRecoveryCookie(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	owner := createOnboardingTestUser(t, database, "recovery-unattributed-owner@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, owner.Email, "StrongPass1")

	const ownedCode = "OVUM-OWNED-CODE00"
	const unattributedCode = "OVUM-NOOWNER-CODE"

	// Positive anchor: a payload minted for this account reveals its code. Both
	// payloads carry the same unexpired bound, so the owner id is the only guard
	// that separates them — the expiry cannot rescue this test's refusal.
	attributed := recoveryCodePageCookieForTest(t, owner.ID, ownedCode, time.Now().Add(5*time.Minute))

	attributedRequest := httptest.NewRequest(http.MethodGet, "/recovery-code", nil)
	attributedRequest.Header.Set("Accept-Language", "en")
	attributedRequest.Header.Set("Cookie", authCookie+"; "+recoveryCodeCookieName+"="+attributed)
	attributedResponse, err := app.Test(attributedRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("attributed recovery-code request failed: %v", err)
	}
	defer func() { _ = attributedResponse.Body.Close() }()
	if attributedResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected the owner's own recovery code to render, got %d", attributedResponse.StatusCode)
	}
	if !strings.Contains(mustReadBodyString(t, attributedResponse.Body), ownedCode) {
		t.Fatal("the recovery-code page must still reveal the code minted for this account")
	}

	// An unattributed payload carrying a different code must reveal nothing.
	unattributed := recoveryCodePageCookieForTest(t, 0, unattributedCode, time.Now().Add(5*time.Minute))

	request := httptest.NewRequest(http.MethodGet, "/recovery-code", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie+"; "+recoveryCodeCookieName+"="+unattributed)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("unattributed recovery-code request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected an unattributed recovery payload to be refused with a redirect, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/dashboard" {
		t.Fatalf("expected redirect to /dashboard, got %q", location)
	}
	if strings.Contains(mustReadBodyString(t, response.Body), unattributedCode) {
		t.Fatal("an unattributed recovery payload must not surface its code")
	}
	cleared := responseCookie(response.Cookies(), recoveryCodeCookieName)
	if cleared == nil || cleared.Value != "" {
		t.Fatal("expected the refused recovery cookie to be cleared, not left presentable on a retry")
	}
}

// TestRecoveryCodePageRefusesAnExpiredRecoveryCookie pins the reveal's time
// bound where it has to hold: on the server. The 20-minute TTL reaches only the
// Set-Cookie `Expires` attribute, which is a browser hint — a client that keeps
// the sealed value can hand it back on its own session for as long as the code
// and SECRET_KEY live. The bound therefore rides inside the sealed payload and
// is verified on read, the way the TOTP enrollment cookie carries its own
// ExpiresAt beside its owner id.
//
// Both payloads here are sealed under the app's own secret and minted for the
// session presenting them, so only the expiry separates them. The fresh one is
// the positive anchor: it proves the page still renders the code and retracts
// the cookie as it does, so the refusal below is not a page that shows nothing
// to everybody.
//
// Each leg runs on its OWN account. A reveal now consumes the account's
// server-side mark, so anchoring and refusing on one account would let the mark
// refuse the second payload and leave the expiry itself untested — green about a
// control it never reached.
func TestRecoveryCodePageRefusesAnExpiredRecoveryCookie(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	anchor := createOnboardingTestUser(t, database, "recovery-expired-anchor@example.com", "StrongPass1", true)
	anchorCookie := loginAndExtractAuthCookie(t, app, anchor.Email, "StrongPass1")
	subject := createOnboardingTestUser(t, database, "recovery-expired-reveal@example.com", "StrongPass1", true)
	subjectCookie := loginAndExtractAuthCookie(t, app, subject.Email, "StrongPass1")

	const freshCode = "OVUM-FRESH-CODE00"
	const expiredCode = "OVUM-STALE-CODE00"

	assertRecoveryCodeRevealedAndCookieRetracted(t, app, anchorCookie,
		recoveryCodePageCookieForTest(t, anchor.ID, freshCode, time.Now().Add(5*time.Minute)), freshCode)
	assertRecoveryCodeRevealRefused(t, app, subjectCookie,
		recoveryCodePageCookieForTest(t, subject.ID, expiredCode, time.Now().Add(-time.Minute)), expiredCode)
}

// TestRecoveryCodePageRefusesARecoveryCookieCarryingNoExpiry covers the payload
// shape minted before the expiry field existed: it opens, it names the right
// account, and it says nothing about when it stops being honored. An absent
// bound is invalid input, not a licence to display, so the page refuses it and
// retracts the cookie — the same treatment a payload naming no owner gets. The
// account keeps its session and lands on its continue path; a code it never got
// to read is regenerated from Settings, which every flow that can issue this
// cookie leaves reachable.
//
// The two legs run on separate accounts for the reason stated on the expiry
// guard above: a reveal spends the account's consumption mark, and sharing one
// account would let the mark answer for the missing bound.
func TestRecoveryCodePageRefusesARecoveryCookieCarryingNoExpiry(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	anchor := createOnboardingTestUser(t, database, "recovery-unbounded-anchor@example.com", "StrongPass1", true)
	anchorCookie := loginAndExtractAuthCookie(t, app, anchor.Email, "StrongPass1")
	subject := createOnboardingTestUser(t, database, "recovery-unbounded-reveal@example.com", "StrongPass1", true)
	subjectCookie := loginAndExtractAuthCookie(t, app, subject.Email, "StrongPass1")

	const freshCode = "OVUM-BOUND-CODE00"
	const unboundedCode = "OVUM-LEGACY-CODE0"

	assertRecoveryCodeRevealedAndCookieRetracted(t, app, anchorCookie,
		recoveryCodePageCookieForTest(t, anchor.ID, freshCode, time.Now().Add(5*time.Minute)), freshCode)
	assertRecoveryCodeRevealRefused(t, app, subjectCookie,
		recoveryCodePageCookieForTest(t, subject.ID, unboundedCode, time.Time{}), unboundedCode)
}

// TestRecoveryCodeRevealRefusesAReplayedCookieAndRearmsOnRegenerate is the
// consumption-mark contract for the recovery code, driven through the real
// regeneration flow rather than a hand-sealed payload: what a client that kept
// the cookie holds is exactly the value the server handed it.
//
// The payload expiry bounds that client's window to 20 minutes and does not
// close it — inside the window the same sealed value opens and, until
// users.recovery_code_revealed_at existed, revealed the code again. Three legs,
// because a fix that simply refused forever would pass the first two:
//   - the first reveal still shows the code (positive anchor)
//   - the ORIGINAL sealed value, replayed on the same session, is refused, lands
//     on the continue path an absent cookie lands on, and shows no code
//   - regenerating mints a fresh code and re-arms the mark in the same write, so
//     the new reveal works
func TestRecoveryCodeRevealRefusesAReplayedCookieAndRearmsOnRegenerate(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "recovery-reveal-replay@example.com")

	firstAuth, firstSealed := regenerateRecoveryCodeForTest(t, &ctx)
	firstCode := assertRecoveryCodeRevealShowsCode(t, ctx.app, firstAuth, firstSealed)
	// A refusal lands on the account's post-login path (/dashboard here), not on
	// the /settings continue path the refused payload named: once the payload is
	// refused, nothing in it is acted on.
	assertRecoveryCodeRevealRefused(t, ctx.app, firstAuth, firstSealed, firstCode)

	secondAuth, secondSealed := regenerateRecoveryCodeForTest(t, &ctx)
	secondCode := assertRecoveryCodeRevealShowsCode(t, ctx.app, secondAuth, secondSealed)
	if secondCode == firstCode {
		t.Fatal("expected regeneration to mint a different recovery code")
	}
	// The re-armed mark belongs to the new reveal only: the second cookie is
	// spent too, and the first stays refused rather than riding the re-arm.
	assertRecoveryCodeRevealRefused(t, ctx.app, secondAuth, secondSealed, secondCode)
	assertRecoveryCodeRevealRefused(t, ctx.app, secondAuth, firstSealed, firstCode)
}

// regenerateRecoveryCodeForTest drives POST /api/v1/users/current/recovery-code
// and returns the refreshed auth cookie (regeneration bumps the session version)
// together with the sealed reveal cookie the response handed the client. It
// writes the refreshed cookie back into ctx so a second regeneration on the same
// account is not refused by the version its own predecessor bumped.
func regenerateRecoveryCodeForTest(t *testing.T, ctx *settingsSecurityTestContext) (string, string) {
	t.Helper()

	response := settingsFormRequestWithCSRF(t, *ctx, http.MethodPost, "/api/v1/users/current/recovery-code", url.Values{
		"password": {"StrongPass1"},
	}, nil)
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 on recovery-code regeneration, got %d", response.StatusCode)
	}
	sealed := responseCookieValue(response.Cookies(), recoveryCodeCookieName)
	if sealed == "" {
		t.Fatal("expected a sealed recovery-code cookie after regeneration")
	}
	refreshedAuth := responseCookieValue(response.Cookies(), authCookieName)
	if refreshedAuth == "" {
		t.Fatal("expected a refreshed auth cookie after regeneration (the session version was bumped)")
	}
	ctx.authCookie = authCookieName + "=" + refreshedAuth
	return ctx.authCookie, sealed
}

// assertRecoveryCodeRevealShowsCode drives one reveal that must succeed and
// returns the code the page displayed, read through its stable hook so the
// caller compares against what actually reached the owner.
func assertRecoveryCodeRevealShowsCode(t *testing.T, app *fiber.App, authCookie string, sealed string) string {
	t.Helper()

	response := recoveryCodePageWithCookie(t, app, authCookie, sealed)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected the armed reveal to render, got %d", response.StatusCode)
	}
	return recoveryCodeFromRevealPage(t, mustReadBodyString(t, response.Body))
}

// TestRecoveryCodeRevealRefusesAReplayedInlineCookie is the same contract on the
// OTHER surface that shows a recovery code: the block the register page renders
// straight after sign-up. Both surfaces read one mark, so a fix applied to the
// dedicated page alone would leave this one replayable — the shape of an
// enumerable class closed at N of N+1.
func TestRecoveryCodeRevealRefusesAReplayedInlineCookie(t *testing.T) {
	app, _ := newOnboardingTestApp(t)
	authCookie, sealed := registerAndExtractRecoveryCookies(t, app, "recovery-inline-replay@example.com", "StrongPass1")
	if authCookie == "" || sealed == "" {
		t.Fatal("expected auth and recovery cookies from the register pickup")
	}
	session := authCookieName + "=" + authCookie

	first := registerPageWithRecoveryCookie(t, app, session, sealed)
	if first.StatusCode != http.StatusOK {
		t.Fatalf("expected the inline reveal to render, got %d", first.StatusCode)
	}
	revealedCode := recoveryCodeFromRevealPage(t, mustReadBodyString(t, first.Body))

	replay := registerPageWithRecoveryCookie(t, app, session, sealed)
	if replay.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected a replayed inline reveal to be refused with a redirect, got %d", replay.StatusCode)
	}
	if location := replay.Header.Get("Location"); location != "/onboarding" {
		t.Fatalf("expected the refusal to land on the post-login path /onboarding, got %q", location)
	}
	if strings.Contains(mustReadBodyString(t, replay.Body), revealedCode) {
		t.Fatal("a replayed inline reveal must not surface the recovery code")
	}
	assertRevealCookieCleared(t, replay, recoveryCodeCookieName)
}

func registerPageWithRecoveryCookie(t *testing.T, app *fiber.App, authCookie string, sealed string) *http.Response {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/register", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie+"; "+recoveryCodeCookieName+"="+sealed)
	return mustAppResponse(t, app, request)
}

// TestHeadOnTheInlineRegisterRevealDoesNotSpendTheRecoveryCode covers the reveal
// surface no route can refuse HEAD for: the inline block lives on GET /register,
// which is also the anonymous signup page, so the refusal sits on
// claimRecoveryCodeReveal — the thing that is actually spent — exactly where the
// first-party rule sits, for the same reason.
//
// Without it the HEAD twin of /register would claim the mark for a signed-in
// owner holding the issuance cookie and answer with a body the protocol strips:
// the code burned, never displayed, and reachable again only by regenerating it.
func TestHeadOnTheInlineRegisterRevealDoesNotSpendTheRecoveryCode(t *testing.T) {
	app, _ := newOnboardingTestApp(t)
	authCookie, sealed := registerAndExtractRecoveryCookies(t, app, "recovery-inline-head@example.com", "StrongPass1")
	if authCookie == "" || sealed == "" {
		t.Fatal("expected auth and recovery cookies from the register pickup")
	}
	session := authCookieName + "=" + authCookie

	headRequest := httptest.NewRequest(http.MethodHead, "/register", nil)
	headRequest.Header.Set("Accept-Language", "en")
	headRequest.Header.Set("Cookie", session+"; "+recoveryCodeCookieName+"="+sealed)
	headResponse := mustAppResponse(t, app, headRequest)
	if headResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected HEAD on the inline reveal to take the refused-claim exit, got %d", headResponse.StatusCode)
	}

	// The anchor is the owner's own GET, on the same app and the same cookie:
	// the reveal the HEAD must not have spent is still hers to spend.
	revealed := registerPageWithRecoveryCookie(t, app, session, sealed)
	if revealed.StatusCode != http.StatusOK {
		t.Fatalf("expected the owner's own visit to render the inline reveal after the refused HEAD, got %d", revealed.StatusCode)
	}
	if code := recoveryCodeFromRevealPage(t, mustReadBodyString(t, revealed.Body)); strings.TrimSpace(code) == "" {
		t.Fatal("expected the inline reveal to carry the recovery code after the refused HEAD")
	}
}

// recoveryCodePageCookieForTest seals a recovery-code page payload written as
// raw JSON, so a test can present a shape the production writer will not mint:
// an expiry already in the past, or — for the zero time — a payload from before
// the field existed, carrying no expiry at all.
func recoveryCodePageCookieForTest(t *testing.T, ownerID uint, recoveryCode string, expiresAt time.Time) string {
	t.Helper()

	fields := []string{
		`"uid":` + strconv.FormatUint(uint64(ownerID), 10),
		`"recovery_code":"` + recoveryCode + `"`,
		`"continue_path":"/dashboard"`,
		`"continue_target":"dashboard"`,
		`"surface":"dedicated"`,
	}
	if !expiresAt.IsZero() {
		fields = append(fields, `"expires_at":"`+expiresAt.UTC().Format(time.RFC3339Nano)+`"`)
	}
	return sealCookieForTestApp(t, recoveryCodeCookieName, []byte("{"+strings.Join(fields, ",")+"}"))
}

// assertRecoveryCodeRevealedAndCookieRetracted is the positive anchor the
// expiry guards share: the same page and the same session render a payload that
// differs from the refused one only in its expiry, and retract the cookie in
// that same response.
//
// Deliberately NOT named "once": retraction is what the response can be seen to
// do, and it is not by itself single use — within the payload's lifetime a
// client that kept the sealed value can still present it again, which is what
// the expiry bounds rather than removes. What makes the reveal single use is the
// account's consumption mark, pinned by
// TestRecoveryCodeRevealRefusesAReplayedCookieAndRearmsOnRegenerate. Each helper
// here therefore uses a FRESH account, so one case's claim cannot decide
// another's.
func assertRecoveryCodeRevealedAndCookieRetracted(t *testing.T, app *fiber.App, authCookie string, sealed string, expectedCode string) {
	t.Helper()

	response := recoveryCodePageWithCookie(t, app, authCookie, sealed)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected an unexpired payload minted for this account to render, got %d", response.StatusCode)
	}
	if !strings.Contains(mustReadBodyString(t, response.Body), expectedCode) {
		t.Fatal("the recovery-code page must still reveal an unexpired code minted for this account")
	}
	assertRevealCookieCleared(t, response, recoveryCodeCookieName)
}

// assertRecoveryCodeRevealRefused states both halves of a refusal: no code in
// the response, and the cookie retracted in that same response rather than left
// presentable on a retry.
func assertRecoveryCodeRevealRefused(t *testing.T, app *fiber.App, authCookie string, sealed string, refusedCode string) {
	t.Helper()

	response := recoveryCodePageWithCookie(t, app, authCookie, sealed)
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected the recovery payload to be refused with a redirect, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/dashboard" {
		t.Fatalf("expected the refusal to land on the continue path /dashboard, got %q", location)
	}
	if strings.Contains(mustReadBodyString(t, response.Body), refusedCode) {
		t.Fatal("a refused recovery payload must not surface its code")
	}
	assertRevealCookieCleared(t, response, recoveryCodeCookieName)
}

func TestRecoveryCodePageRejectsTamperedRecoveryCookie(t *testing.T) {
	app, _ := newOnboardingTestApp(t)
	authCookie, recoveryCookie := registerAndExtractRecoveryCookies(
		t,
		app,
		"recovery-cookie-tampered@example.com",
		"StrongPass1",
	)

	if authCookie == "" || recoveryCookie == "" {
		t.Fatalf("expected auth and recovery cookies in register response")
	}

	separatorIndex := strings.Index(recoveryCookie, ".")
	if separatorIndex < 0 || separatorIndex+6 >= len(recoveryCookie) {
		t.Fatalf("expected versioned recovery cookie payload, got %q", recoveryCookie)
	}

	tampered := recoveryCookie[:separatorIndex+5] + "A" + recoveryCookie[separatorIndex+6:]
	if recoveryCookie[separatorIndex+5] == 'A' {
		tampered = recoveryCookie[:separatorIndex+5] + "B" + recoveryCookie[separatorIndex+6:]
	}

	request := httptest.NewRequest(http.MethodGet, "/recovery-code", nil)
	request.Header.Set("Cookie", authCookieName+"="+authCookie+"; "+recoveryCodeCookieName+"="+tampered)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("recovery-code request with tampered cookie failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/onboarding" {
		t.Fatalf("expected redirect to /onboarding, got %q", location)
	}

	cleared := responseCookie(response.Cookies(), recoveryCodeCookieName)
	if cleared == nil {
		t.Fatalf("expected tampered recovery cookie to be cleared")
	}
	if cleared.Value != "" {
		t.Fatalf("expected cleared recovery cookie value, got %q", cleared.Value)
	}
}

func registerAndExtractRecoveryCookies(t *testing.T, app *fiber.App, email string, password string) (string, string) {
	t.Helper()

	form := url.Values{
		"email":            {email},
		"password":         {password},
		"confirm_password": {password},
		"consent":          {"true"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	registerResponse, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("register request failed: %v", err)
	}
	defer func() { _ = registerResponse.Body.Close() }()

	if registerResponse.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected register status 303, got %d", registerResponse.StatusCode)
	}

	pickup := responseCookieValue(registerResponse.Cookies(), registerPickupCookieName)
	if pickup == "" {
		t.Fatalf("expected pickup cookie after register")
	}

	pickupRequest := httptest.NewRequest(http.MethodGet, "/register/welcome", nil)
	pickupRequest.Header.Set("Cookie", registerPickupCookieName+"="+pickup)
	pickupResponse, err := app.Test(pickupRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("register/welcome request failed: %v", err)
	}
	defer func() { _ = pickupResponse.Body.Close() }()

	authCookie := responseCookieValue(pickupResponse.Cookies(), authCookieName)
	recoveryCookie := responseCookieValue(pickupResponse.Cookies(), recoveryCodeCookieName)
	return authCookie, recoveryCookie
}
