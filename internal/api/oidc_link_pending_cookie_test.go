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
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestOIDCLinkPendingCookieSetterRefusesAPayloadThatNamesNoOneOrNothing: a
// pending link with no owner or no identity key is never sealed, and the
// setter retracts any earlier value instead.
func TestOIDCLinkPendingCookieSetterRefusesAPayloadThatNamesNoOneOrNothing(t *testing.T) {
	t.Parallel()

	handler := newSealedExpirySweepHandler()
	for name, tc := range map[string]struct {
		userID uint
		claims security.OIDCClaims
	}{
		"zero owner":     {userID: 0, claims: security.OIDCClaims{Issuer: "https://id.example.com", Subject: "sub"}},
		"blank issuer":   {userID: sealedExpirySweepUserID, claims: security.OIDCClaims{Subject: "sub"}},
		"blank subject":  {userID: sealedExpirySweepUserID, claims: security.OIDCClaims{Issuer: "https://id.example.com"}},
		"spaces subject": {userID: sealedExpirySweepUserID, claims: security.OIDCClaims{Issuer: "https://id.example.com", Subject: "  "}},
	} {
		var setErr error
		response := runSealedCookieProbeRequest(t, nil, func(c fiber.Ctx) error {
			setErr = handler.setOIDCLinkPendingCookie(c, tc.userID, 1, tc.claims)
			return nil
		})
		_ = response.Body.Close()
		if setErr == nil {
			t.Fatalf("%s: expected the setter to refuse", name)
		}
		if cookie := responseCookie(response.Cookies(), oidcLinkPendingCookieName); cookie == nil || strings.TrimSpace(cookie.Value) != "" {
			t.Fatalf("%s: expected the refusal to retract the cookie, got %v", name, cookie)
		}
	}
}

// TestOIDCLinkPendingCookieReaderRefusesAPayloadMissingItsVersionOrIdentity:
// a sealed value without a session version or an identity key is refused and
// retracted, never read as version 1 or as an empty identity to link.
func TestOIDCLinkPendingCookieReaderRefusesAPayloadMissingItsVersionOrIdentity(t *testing.T) {
	t.Parallel()

	handler := newSealedExpirySweepHandler()
	base := oidcLinkPendingCookiePayload{
		UserID:         sealedExpirySweepUserID,
		SessionVersion: 1,
		Issuer:         "https://id.example.com",
		Subject:        "sub",
		ExpiresAt:      time.Now().Add(time.Minute),
	}
	for name, mutate := range map[string]func(*oidcLinkPendingCookiePayload){
		"no session version": func(p *oidcLinkPendingCookiePayload) { p.SessionVersion = 0 },
		"no issuer":          func(p *oidcLinkPendingCookiePayload) { p.Issuer = "" },
		"no subject":         func(p *oidcLinkPendingCookiePayload) { p.Subject = " " },
	} {
		payload := base
		mutate(&payload)
		plaintext, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("%s: marshal: %v", name, err)
		}
		sealed := resealSealedPayloadForSweep(t, handler, oidcLinkPendingCookieName, plaintext)

		var readErr error
		response := runSealedCookieProbeRequest(t, map[string]string{oidcLinkPendingCookieName: sealed}, func(c fiber.Ctx) error {
			_, readErr = handler.parseOIDCLinkPendingCookie(c, sealedExpirySweepUserID)
			return nil
		})
		_ = response.Body.Close()
		if readErr == nil {
			t.Fatalf("%s: expected the reader to refuse", name)
		}
		if cookie := responseCookie(response.Cookies(), oidcLinkPendingCookieName); cookie == nil || strings.TrimSpace(cookie.Value) != "" {
			t.Fatalf("%s: expected the refusal to retract the cookie, got %v", name, cookie)
		}
	}
}

func postOIDCIdentityLinkConfirmJSON(t *testing.T, fixture *oidcStepupFixture, pendingCookie string) *http.Response {
	t.Helper()
	csrfCookie, csrfToken := fixture.settingsCSRF(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/users/current/oidc/link/confirm", strings.NewReader(url.Values{"csrf_token": {csrfToken}}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", joinCookieHeader(settingsCookieHeader(fixture.authCookie, csrfCookie), pendingCookie))
	return mustAppResponse(t, fixture.app, request)
}

// TestOIDCIdentityLinkConfirmRefusesWhileOIDCIsOff: with OIDC switched off
// after the callback parked an identity, the confirm links nothing and drops
// the pending link.
func TestOIDCIdentityLinkConfirmRefusesWhileOIDCIsOff(t *testing.T) {
	t.Parallel()

	fixture := newOIDCStepupFixture(t, "settings-oidc-link-confirm-off@example.com")
	pendingCookie := readOIDCLinkPendingCookie(t, runOIDCIdentityLinkCallback(t, fixture))
	fixture.oidcStub.enabled = false

	response := postOIDCIdentityLinkConfirmJSON(t, fixture, pendingCookie)
	defer func() { _ = response.Body.Close() }()

	if fixture.oidcStub.lastConfirmLinkUserID != 0 {
		t.Fatal("a confirm while OIDC is off must link nothing")
	}
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", response.StatusCode)
	}

	// The confirmation page itself has nothing to offer while OIDC is off.
	request := httptest.NewRequest(http.MethodGet, oidcLinkConfirmPagePath, nil)
	request.Header.Set("Cookie", joinCookieHeader(fixture.authCookie, pendingCookie))
	page := mustAppResponse(t, fixture.app, request)
	_ = page.Body.Close()
	if page.StatusCode != http.StatusSeeOther || page.Header.Get("Location") != "/settings" {
		t.Fatalf("expected the page to send the owner to /settings while OIDC is off, got %d to %q", page.StatusCode, page.Header.Get("Location"))
	}
	if cookie := responseCookie(response.Cookies(), oidcLinkPendingCookieName); cookie == nil || strings.TrimSpace(cookie.Value) != "" {
		t.Fatalf("expected the pending link to be dropped, got %v", cookie)
	}
}

// TestOIDCIdentityLinkConfirmMapsALinkRefusal: an identity claimed by another
// account between the callback and the confirm is refused with the claimed
// spec and spends the pending link; a storage fault keeps it for a retry.
func TestOIDCIdentityLinkConfirmMapsALinkRefusal(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		err         error
		wantStatus  int
		keepPending bool
	}{
		"claimed elsewhere": {err: services.ErrOIDCLinkFailed, wantStatus: settingsOIDCIdentityLinkClaimedErrorSpec().Status},
		"storage fault":     {err: services.ErrOIDCIdentityResolveFailed, wantStatus: authOIDCUnavailableErrorSpec().Status, keepPending: true},
	} {
		fixture := newOIDCStepupFixture(t, "settings-oidc-link-confirm-refusal@example.com")
		pendingCookie := readOIDCLinkPendingCookie(t, runOIDCIdentityLinkCallback(t, fixture))
		fixture.oidcStub.confirmLinkErr = tc.err

		response := postOIDCIdentityLinkConfirmJSON(t, fixture, pendingCookie)
		_ = response.Body.Close()

		if response.StatusCode != tc.wantStatus {
			t.Fatalf("%s: expected %d, got %d", name, tc.wantStatus, response.StatusCode)
		}
		cookie := responseCookie(response.Cookies(), oidcLinkPendingCookieName)
		if kept := cookie == nil; kept != tc.keepPending {
			t.Fatalf("%s: pending link kept=%t, want %t (cookie %v)", name, kept, tc.keepPending, cookie)
		}
	}
}
