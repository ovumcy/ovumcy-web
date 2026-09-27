package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// WEB-40: a request reachable without a CSRF token must never overwrite or
// erase a pending same-origin flash. Before this channel existed, every write
// below sealed straight into flashCookieName — the same slot an ordinary page
// redirect (e.g. a settings save) had just written — so the token-less
// request clobbered it. The fix is a second sealed cookie
// (exemptFlashCookieName) those writers use instead; these tests are red on
// the single-slot code these writers used to share.

// sealPendingPageFlash seals a FlashPayload for the ordinary page slot, the
// way a same-origin redirect (e.g. a settings save) would have left it
// sitting in the browser a moment before a token-less request arrives.
func sealPendingPageFlash(t *testing.T, payload FlashPayload) string {
	t.Helper()
	payload.ExpiresAt = time.Now().Add(flashCookieTTL)
	serialized, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal pending page flash: %v", err)
	}
	return sealCookieForTestApp(t, flashCookieName, serialized)
}

// flashProbeHandler builds a Handler whose secret matches sealCookieForTestApp,
// so cookies sealed by that helper genuinely open against it.
func flashProbeHandler() *Handler {
	return &Handler{secretKey: []byte(testAppSecretKey), cookieSecure: true}
}

// TestOIDCCallbackStateMismatchDoesNotClobberAPendingPageFlash is the direct
// regression for the issue: POST /auth/oidc/callback is the sole CSRF
// exemption, and a garbage/attacker-chosen state never matches the sealed one
// regardless of who sends it. On unpatched code this redirect wrote
// FlashPayload{AuthError: ...} straight into flashCookieName, discarding
// whatever the browser already carried there.
func TestOIDCCallbackStateMismatchDoesNotClobberAPendingPageFlash(t *testing.T) {
	t.Parallel()

	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  newStubOIDCWorkflowService(true),
	})

	pendingFlash := sealPendingPageFlash(t, FlashPayload{SettingsSuccess: "password_changed"})

	request := httptest.NewRequest(http.MethodPost, security.OIDCCallbackPath, strings.NewReader(url.Values{
		"state": {"attacker-supplied-garbage"},
		"code":  {"whatever"},
	}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", flashCookieName+"="+pendingFlash)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/login" {
		t.Fatalf("expected redirect to /login, got %q", location)
	}

	// The invariant: this response carries no Set-Cookie for the page slot at
	// all — neither an overwrite nor a clear — so the value the browser
	// already holds survives untouched.
	if touched := responseCookie(response.Cookies(), flashCookieName); touched != nil {
		t.Fatalf("a token-less callback refusal must not touch the page flash cookie at all, got %#v", touched)
	}
	exempt := responseCookie(response.Cookies(), exemptFlashCookieName)
	if exempt == nil || strings.TrimSpace(exempt.Value) == "" {
		t.Fatal("expected the refusal on the exempt channel")
	}
	if payload := decodeExemptFlashCookieForTest(t, exempt.Value); payload.AuthError == "" {
		t.Fatalf("expected an auth_error on the exempt channel, got %+v", payload)
	}
}

// TestPopFlashCookiePageSlotWinsWhenBothChannelsArePending pins the read-side
// precedence the decision names: when a same-origin page flash is pending
// alongside an exempt-channel one, the page slot wins — a token-less writer
// must never outrank the trusted channel, even after both cookies reach the
// same response.
func TestPopFlashCookiePageSlotWinsWhenBothChannelsArePending(t *testing.T) {
	pageFlash := sealPendingPageFlash(t, FlashPayload{SettingsSuccess: "password_changed"})
	exemptPayload, err := json.Marshal(FlashPayload{AuthError: "auth.invalid_credentials", ExpiresAt: time.Now().Add(flashCookieTTL)})
	if err != nil {
		t.Fatalf("marshal exempt flash payload: %v", err)
	}
	exemptFlash := sealCookieForTestApp(t, exemptFlashCookieName, exemptPayload)

	handler := flashProbeHandler()
	app := fiber.New()
	app.Get("/probe", func(c fiber.Ctx) error {
		return c.JSON(handler.popFlashCookie(c))
	})

	request := httptest.NewRequest(http.MethodGet, "/probe", nil)
	request.Header.Set("Cookie", joinCookieHeader(flashCookieName+"="+pageFlash, exemptFlashCookieName+"="+exemptFlash))
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	var payload FlashPayload
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode probe response: %v", err)
	}
	if payload.SettingsSuccess != "password_changed" || payload.AuthError != "" {
		t.Fatalf("expected the page slot to win over the exempt channel, got %+v", payload)
	}

	// The page slot is always single-use, cleared on this pop.
	if cleared := responseCookie(response.Cookies(), flashCookieName); cleared == nil || cleared.Value != "" {
		t.Fatalf("expected the page flash cookie retracted, got %#v", cleared)
	}
	// The exempt slot is retracted too, not left standing: nothing read its
	// message this time, and leaving it sealed would surface it on a later,
	// unrelated render once the page slot that outranked it is gone. The
	// token-less message is the one to drop.
	if cleared := responseCookie(response.Cookies(), exemptFlashCookieName); cleared == nil || cleared.Value != "" {
		t.Fatalf("expected the exempt flash cookie retracted when the page slot wins, got %#v", cleared)
	}
}

// TestPopFlashCookieShowsExemptChannelWhenPageSlotIsEmpty is the other half:
// a genuine provider refusal with nothing else pending must still reach the
// owner, so the exempt channel is not merely a place refusals go to die.
func TestPopFlashCookieShowsExemptChannelWhenPageSlotIsEmpty(t *testing.T) {
	exemptPayload, err := json.Marshal(FlashPayload{AuthError: "auth.invalid_credentials", ExpiresAt: time.Now().Add(flashCookieTTL)})
	if err != nil {
		t.Fatalf("marshal exempt flash payload: %v", err)
	}
	exemptFlash := sealCookieForTestApp(t, exemptFlashCookieName, exemptPayload)

	handler := flashProbeHandler()
	app := fiber.New()
	app.Get("/probe", func(c fiber.Ctx) error {
		return c.JSON(handler.popFlashCookie(c))
	})

	request := httptest.NewRequest(http.MethodGet, "/probe", nil)
	request.Header.Set("Cookie", exemptFlashCookieName+"="+exemptFlash)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	var payload FlashPayload
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode probe response: %v", err)
	}
	if payload.AuthError != "auth.invalid_credentials" {
		t.Fatalf("expected the exempt channel to surface when nothing else is pending, got %+v", payload)
	}
}

// TestOIDCStepupContinueCrossSiteRefusalDoesNotClobberAPendingPageFlash
// covers the second CSRF-exempt writer named by the decision:
// requireFirstPartyRequest's own refusal for the step-up continue route fires
// on exactly the cross-site request the guard exists to name, with no session
// or step-up cookie required to reach it.
func TestOIDCStepupContinueCrossSiteRefusalDoesNotClobberAPendingPageFlash(t *testing.T) {
	t.Parallel()

	app, _ := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{
		cookieSecure: true,
		oidcService:  newStubOIDCWorkflowService(true),
	})

	pendingFlash := sealPendingPageFlash(t, FlashPayload{SettingsSuccess: "password_changed"})

	request := httptest.NewRequest(http.MethodGet, oidcCallbackContinuePath, nil)
	request.Header.Set("Cookie", flashCookieName+"="+pendingFlash)
	crossSiteNavigation.applyTo(request)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusSeeOther)

	if touched := responseCookie(response.Cookies(), flashCookieName); touched != nil {
		t.Fatalf("a cross-site continue-route refusal must not touch the page flash cookie, got %#v", touched)
	}
	if exempt := responseCookie(response.Cookies(), exemptFlashCookieName); exempt == nil || strings.TrimSpace(exempt.Value) == "" {
		t.Fatal("expected the refusal on the exempt channel")
	}
}

// TestRegisterPickupCrossSiteRefusalDoesNotClobberAPendingPageFlash covers the
// third: refuseRegisterPickupRequest's conditional flash fires on the same
// cross-site condition, reachable whenever the browser still carries a
// register-pickup cookie.
func TestRegisterPickupCrossSiteRefusalDoesNotClobberAPendingPageFlash(t *testing.T) {
	t.Parallel()

	app, _ := newOnboardingTestApp(t)

	seed := mustAppResponse(t, app, registerRequest("pickup-crosssite@example.com"))
	if seed.StatusCode != http.StatusSeeOther {
		t.Fatalf("seed register failed: status %d", seed.StatusCode)
	}
	pickup := responseCookieValue(seed.Cookies(), registerPickupCookieName)
	if pickup == "" {
		t.Fatal("expected a register-pickup cookie from the seed registration")
	}

	pendingFlash := sealPendingPageFlash(t, FlashPayload{SettingsSuccess: "password_changed"})

	request := httptest.NewRequest(http.MethodGet, "/register/welcome", nil)
	request.Header.Set("Cookie", joinCookieHeader(registerPickupCookieName+"="+pickup, flashCookieName+"="+pendingFlash))
	crossSiteNavigation.applyTo(request)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusSeeOther)

	if touched := responseCookie(response.Cookies(), flashCookieName); touched != nil {
		t.Fatalf("a cross-site register-pickup refusal must not touch the page flash cookie, got %#v", touched)
	}
	if exempt := responseCookie(response.Cookies(), exemptFlashCookieName); exempt == nil || strings.TrimSpace(exempt.Value) == "" {
		t.Fatal("expected the refusal on the exempt channel")
	}
}
