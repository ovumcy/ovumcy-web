package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

// OIDC transit cookies — a value the reader refuses is retracted by the
// response that refused it.
//
// The three readers below are the OIDC half of the convention the TOTP pair
// already follows (`parseTOTPPendingCookie`, `parseTOTPSetupCookie`): the
// reader is the only place that knows a value was presented and found
// unusable, so the clear belongs there and no caller repeats it. Before this
// each refusal arm returned an empty payload and left the cookie riding until
// its own expiry — on `ovumcy_oidc_auth` and `ovumcy_oidc_stepup` that is a
// `SameSite=None` value re-sent to the callback path by any site that can
// cause a request to it.
//
// What is NOT retracted, and must not become so: a payload the reader
// HONOURS. The transit cookies are peeked, not popped, so a stray or
// cross-site hit on the callback path cannot cancel the sign-in or step-up
// their owner is in the middle of; only the callback whose `state` matches
// spends them. The dividing line is not valid-versus-invalid but "could this
// value still be somebody's live flow": a refused value never can be — no
// writer here mints an incomplete payload, and an expired one is past the
// bound its own reader enforces — and an absent cookie has nothing to
// retract. The anchor in each case below is what holds that line: a freshly
// minted value is read back and must survive the read.
//
// This file is single-purpose on purpose: it is one narrow security invariant
// spanning three cookies that each already own a test file, and splitting it
// across those three would leave the class it is about with no home.

// oidcTransitCookieNames is the set this invariant covers: the sealed cookies
// that carry an OIDC flow between the browser, the provider and the callback.
// Each is read by a peek that returns a zero payload on refusal, and each
// mint/read pair is taken from the sealed-cookie probes rather than restated
// here, so a test cannot agree with a reader it describes itself.
var oidcTransitCookieNames = []string{
	oidcStateCookieName,
	oidcStepupCookieName,
	oidcStepupContinuationCookieName,
}

// oidcTransitCookieRefusal is one way a presented value is refused, together
// with the handler that must refuse it: the codec arm is reachable only on a
// deployment whose codec never builds.
type oidcTransitCookieRefusal struct {
	name    string
	value   string
	handler *Handler
}

func TestOIDCTransitCookieReaderRetractsTheValueItRefuses(t *testing.T) {
	t.Parallel()

	for _, cookieName := range oidcTransitCookieNames {
		t.Run(cookieName, func(t *testing.T) {
			t.Parallel()

			handler := newSealedExpirySweepHandler()
			probe, declared := sealedCookieExpiryProbes[cookieName]
			if !declared {
				t.Fatalf("%s: no production mint/read pair is declared for this cookie", cookieName)
			}
			usable := mintSealedCookieForSweep(t, handler, cookieName, probe)

			// The anchor, first and in the same shape as every case below: a
			// value the reader honours is returned WITHOUT being spent.
			// Without it each refusal case would pass just as well against a
			// reader that refuses everything and clears unconditionally.
			honoured := false
			anchor := runSealedCookieProbeRequest(t, map[string]string{cookieName: usable}, func(c fiber.Ctx) error {
				honoured = probe.honours(handler, c)
				return nil
			})
			defer func() { _ = anchor.Body.Close() }()
			if !honoured {
				t.Fatalf("%s: a freshly minted value must still be honoured by its reader", cookieName)
			}
			assertOIDCTransitCookieLeftInPlace(t, anchor, cookieName)

			for _, refusal := range oidcTransitCookieRefusals(t, handler, cookieName, usable) {
				t.Run(refusal.name, func(t *testing.T) {
					t.Parallel()

					refused := true
					response := runSealedCookieProbeRequest(t, map[string]string{cookieName: refusal.value}, func(c fiber.Ctx) error {
						refused = !probe.honours(refusal.handler, c)
						return nil
					})
					defer func() { _ = response.Body.Close() }()

					if !refused {
						t.Fatalf("%s: expected %s to be refused, the reader honoured it", cookieName, refusal.name)
					}
					assertOIDCTransitCookieRetracted(t, response, cookieName)
				})
			}
		})
	}
}

// oidcTransitCookieRefusals enumerates every way a PRESENTED value is refused
// by these readers: the codec that will not build, an envelope the seal
// refuses, a well-sealed value whose plaintext is not the payload, and a
// payload past the bound it carries. The missing-cookie branch is deliberately
// absent — nothing was presented, and an empty value is what a clear emits.
func oidcTransitCookieRefusals(t *testing.T, handler *Handler, cookieName string, usable string) []oidcTransitCookieRefusal {
	t.Helper()

	sealedNonPayload, err := handler.sealCookieValue(cookieName, []byte("[not the payload shape]"))
	if err != nil {
		t.Fatalf("%s: seal a value whose plaintext is not the payload: %v", cookieName, err)
	}

	return []oidcTransitCookieRefusal{
		// A deployment with no usable secret key: cookieCodec() caches its
		// error under sync.Once for the life of the process, so this is not a
		// transient failure a later request recovers from — every request from
		// here on refuses the same value, and no flow it refuses can complete.
		// Retracting is therefore what the codec arm owes, exactly as the other
		// arms do.
		{name: "codec_unavailable", value: usable, handler: newKeylessOIDCTransitHandler()},
		{name: "not_a_sealed_envelope", value: "definitely-not-a-sealed-cookie-value", handler: handler},
		{name: "tampered_ciphertext", value: flipLastBaseEncodedByte(t, usable), handler: handler},
		{name: "sealed_but_not_the_expected_payload", value: sealedNonPayload, handler: handler},
		{name: "expired_payload", value: expireSealedOIDCTransitPayload(t, handler, cookieName, usable), handler: handler},
	}
}

// newKeylessOIDCTransitHandler is a handler whose codec can never build. It
// still clears cookies: a retraction needs no key.
func newKeylessOIDCTransitHandler() *Handler {
	return &Handler{location: time.UTC, cookieSecure: true}
}

// expireSealedOIDCTransitPayload rewrites the bound the minted payload carries
// into the past and re-seals it with the production codec, so the value the
// reader is handed differs from a live one in nothing but its expiry.
func expireSealedOIDCTransitPayload(t *testing.T, handler *Handler, cookieName string, usable string) string {
	t.Helper()

	payload := map[string]any{}
	if err := json.Unmarshal(openSealedCookieForSweep(t, handler, cookieName, usable), &payload); err != nil {
		t.Fatalf("%s: decode the minted payload: %v", cookieName, err)
	}
	bound, carried := payload["expires_at"].(string)
	if !carried || strings.TrimSpace(bound) == "" {
		t.Fatalf("%s: the minted payload carries no expires_at to move into the past", cookieName)
	}
	payload["expires_at"] = time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)

	rewritten, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("%s: re-encode the expired payload: %v", cookieName, err)
	}
	return resealSealedPayloadForSweep(t, handler, cookieName, rewritten)
}

// assertOIDCTransitCookieRetracted pins the second obligation of every
// refusal: the response must retract the value it just refused, with an empty
// value and an expiry in the past.
func assertOIDCTransitCookieRetracted(t *testing.T, response *http.Response, cookieName string) {
	t.Helper()

	retracted := responseCookie(response.Cookies(), cookieName)
	if retracted == nil {
		t.Fatalf("the refused %s cookie survives the response: no Set-Cookie retracts it, so the browser keeps sending it until it expires", cookieName)
	}
	if strings.TrimSpace(retracted.Value) != "" {
		t.Fatalf("expected an empty retracted %s cookie value, got %q", cookieName, retracted.Value)
	}
	if retracted.Expires.IsZero() || retracted.Expires.After(time.Now()) {
		t.Fatalf("expected the retracted %s cookie to expire in the past, got expiry %s", cookieName, retracted.Expires)
	}
}

// assertOIDCTransitCookieLeftInPlace is the counterpart, and the reason the
// refusal assertions mean anything: a value the reader honoured is left for
// the request that completes the flow. These cookies are peeked, never popped.
func assertOIDCTransitCookieLeftInPlace(t *testing.T, response *http.Response, cookieName string) {
	t.Helper()

	touched := responseCookie(response.Cookies(), cookieName)
	if touched != nil && strings.TrimSpace(touched.Value) == "" {
		t.Fatalf("the %s cookie was retracted by a read that honoured it; a stray hit on this path can now cancel the flow its owner started", cookieName)
	}
}
