package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

// Sealed cookies — a value a reader refuses is retracted by the response that
// refused it.
//
// The reader is the only place that knows a value was PRESENTED and found
// unusable: every caller sees the same empty payload whether a cookie arrived
// or not, so a caller-side clear has to be repeated at each call site and the
// next one added without it reintroduces the leak. The clear therefore belongs
// to the reader, and this file is what holds that for the whole class rather
// than for the cookies somebody remembered.
//
// THE SET IS DERIVED, NOT LISTED. It is the same roster
// TestEverySealedCookiePayloadCarriesAServerVerifiedExpiry sweeps —
// sealedCookieNamesFromFiles over every sealedCookieSpec this package
// declares, cross-checked against every cookie-name constant it declares, so a
// spec built by a helper call still reddens on its name. The predecessor of
// this guard carried a hand-written list of three OIDC cookies, and the fourth
// OIDC transit reader (readOIDCLinkPendingCookie) was invisible to it for
// exactly as long as the list was. A sealed cookie this guard cannot drive
// FAILS here — no production mint, no production reader — rather than being
// skipped: an undecidable case is the N+1 the sweep exists to catch. There is
// no exemption list, and none is needed today; a reader that must not retract
// would have to say so in the source the roster is read from, never in this
// file.
//
// WHAT MUST NOT BE RETRACTED: a payload the reader HONOURS, for every reader
// that peeks rather than pops. The transit cookies are read and left in place
// so a stray or cross-site hit on the callback path cannot cancel the sign-in,
// step-up or confirmation their owner is in the middle of; only the request
// whose state matches spends them. Two readers are the other kind and spend
// what they read (popFlashCookie, popRegisterPickupCookie), and for those
// "retracts what it refuses" is subsumed by "retracts always". Which kind a
// reader is, is declared beside its probe (sealedCookieExpiryProbe.spentOnRead)
// and asserted in BOTH directions here, so a declaration that is wrong reddens
// instead of quietly weakening the case it belongs to.
//
// THE LIMIT, stated so no reader concludes the broader claim: the refusal arms
// below are the ones a caller can build from a sealed value — a codec that
// will not build, an envelope that will not open, a plaintext that is not the
// payload, and, where the payload carries a bound this sweep can move, one
// past that bound. A bound sealed inside a signed token cannot be moved
// without re-signing it, so the opaque-token cookies (ovumcy_auth,
// ovumcy_reset_password) are driven through the other four arms here, and
// their expired case is the expiry sweep's assertOpaqueBoundRefusedOnceExpired.
// A refusal that is not about the cookie — a wrong six-digit code, a password
// that does not verify — is a different question and deliberately leaves the
// cookie in place.
//
// This file is single-purpose on purpose: it is one narrow security invariant
// spanning every sealed cookie, and folding it into an area aggregator would
// leave the class with no home.

// sealedCookieRefusal is one way a PRESENTED value is refused, together with
// the handler that must refuse it: the codec arm is reachable only on a
// deployment whose codec never builds.
type sealedCookieRefusal struct {
	name    string
	value   string
	handler *Handler
}

func TestEverySealedCookieReaderRetractsTheValueItRefuses(t *testing.T) {
	t.Parallel()

	// The roster's own two halves are anchored on fixtures they own before it
	// is believed here: an extractor that stopped recognising declarations, or
	// a cross-check that stopped reporting, would otherwise hand this guard a
	// short roster and it would report success over the cookies it never saw.
	assertSealedCookieRosterExtractorAnswersBothWays(t)
	assertRosterCrossCheckAnswersBothWays(t)

	files := parseSealedCookiePackageFiles(t)
	roster := sealedCookieRosterFrom(t, files)
	if len(roster) == 0 {
		t.Fatal("the roster came back empty — this guard found no sealed cookie declaration at all and is measuring nothing")
	}
	assertEveryCookieNameConstantIsSweptOrDeclaredUnsealed(t, files, roster)

	for _, cookieName := range roster {
		t.Run(cookieName, func(t *testing.T) {
			t.Parallel()

			handler := newSealedExpirySweepHandler()
			probe, declared := sealedCookieExpiryProbes[cookieName]
			if !declared {
				t.Fatalf(
					"%s: nothing here mints and reads this sealed cookie, so the guard cannot present a refused value to its reader at all — a sealed cookie it cannot drive fails rather than being skipped. Add its production mint and its production reader to sealedCookieExpiryProbes",
					cookieName)
			}
			if probe.honours == nil {
				t.Fatalf(
					"%s: no production reader is declared for this cookie, so nothing here can tell a refusal from an acceptance. Add honours to its sealedCookieExpiryProbes entry",
					cookieName)
			}

			usable := mintSealedCookieForSweep(t, handler, cookieName, probe)
			assertSealedCookieReaderSpendsAsDeclared(t, handler, cookieName, probe, usable)

			for _, refusal := range sealedCookieRefusals(t, handler, cookieName, usable) {
				t.Run(refusal.name, func(t *testing.T) {
					t.Parallel()

					honoured := true
					response := runSealedCookieProbeRequest(t, map[string]string{cookieName: refusal.value}, func(c fiber.Ctx) error {
						honoured = probe.honours(refusal.handler, c)
						return nil
					})
					defer func() { _ = response.Body.Close() }()

					if honoured {
						t.Fatalf("%s: expected %s to be refused, the reader honoured it", cookieName, refusal.name)
					}
					assertSealedCookieRetracted(t, response, cookieName, refusal.name)
				})
			}
		})
	}
}

// sealedCookieRefusals enumerates every way a PRESENTED value is refused that
// can be built from the sealed value itself. The missing-cookie branch is
// deliberately absent — nothing was presented, and an empty value is what a
// clear emits.
//
// The fifth arm is decided by the payload the production mint actually
// produced, never by the cookie's name: only a payload carrying a bound in its
// own plaintext has one this guard can move into the past and re-seal.
func sealedCookieRefusals(t *testing.T, handler *Handler, cookieName string, usable string) []sealedCookieRefusal {
	t.Helper()

	sealedNonPayload, err := handler.sealCookieValue(cookieName, []byte("[not the payload shape]"))
	if err != nil {
		t.Fatalf("%s: seal a value whose plaintext is not the payload: %v", cookieName, err)
	}

	refusals := []sealedCookieRefusal{
		// A deployment with no usable secret key. cookieCodec() builds under a
		// sync.Once held on the Handler and caches the error there, so this is
		// not a transient failure a later request recovers from: the server
		// composes one Handler (cmd/ovumcy), and every request it serves from
		// then on refuses the same value. Retracting is what this arm owes,
		// exactly as the others do.
		{name: "codec_unavailable", value: usable, handler: newKeylessSealedCookieHandler()},
		{name: "not_a_sealed_envelope", value: "definitely-not-a-sealed-cookie-value", handler: handler},
		{name: "tampered_ciphertext", value: flipLastBaseEncodedByte(t, usable), handler: handler},
		{name: "sealed_but_not_the_expected_payload", value: sealedNonPayload, handler: handler},
	}

	plaintext := openSealedCookieForSweep(t, handler, cookieName, usable)
	verdict, err := classifySealedPayload(plaintext, time.Now())
	if err != nil {
		t.Fatalf("%s: %v — a sealed payload this guard cannot classify cannot be driven past its own bound either", cookieName, err)
	}
	if verdict.kind == sealedPayloadKindBounded {
		expired := rewriteSealedPayloadBound(t, plaintext, verdict.bound, time.Now().Add(-time.Hour))
		refusals = append(refusals, sealedCookieRefusal{
			name:    "expired_payload",
			value:   resealSealedPayloadForSweep(t, handler, cookieName, expired),
			handler: handler,
		})
	}
	return refusals
}

// newKeylessSealedCookieHandler is a handler whose codec can never build. It
// still clears cookies: a retraction needs no key.
func newKeylessSealedCookieHandler() *Handler {
	return &Handler{location: time.UTC, cookieSecure: true}
}

// assertSealedCookieReaderSpendsAsDeclared is the anchor the refusal cases rest
// on, and the other half of the invariant.
//
// Without the first assertion each refusal below would pass just as well
// against a reader that refuses everything and clears unconditionally. The
// second is what keeps a peeking reader peeking: a read that retracts a value
// it HONOURED hands every stray request to this path the power to cancel the
// flow its owner started. Both directions of the declaration are asserted, so
// a probe that claims the wrong one reddens here rather than silently turning
// the anchor off.
func assertSealedCookieReaderSpendsAsDeclared(
	t *testing.T, handler *Handler, cookieName string, probe sealedCookieExpiryProbe, usable string,
) {
	t.Helper()

	honoured := false
	response := runSealedCookieProbeRequest(t, map[string]string{cookieName: usable}, func(c fiber.Ctx) error {
		honoured = probe.honours(handler, c)
		return nil
	})
	defer func() { _ = response.Body.Close() }()

	if !honoured {
		t.Fatalf(
			"%s: the reader refused a freshly minted value — the probe is not exercising the production read path, so nothing below it proves anything",
			cookieName)
	}

	touched := responseCookie(response.Cookies(), cookieName)
	spent := touched != nil && strings.TrimSpace(touched.Value) == ""
	switch {
	case probe.spentOnRead && !spent:
		t.Fatalf(
			"%s: its probe declares a reader that spends what it reads, but the response left the honoured value in place — one of the two is wrong, and while they disagree the refusal cases below prove less than they claim",
			cookieName)
	case !probe.spentOnRead && spent:
		t.Fatalf(
			"%s: the cookie was retracted by a read that HONOURED it; a stray or cross-site hit on this path can now cancel the flow its owner started",
			cookieName)
	}
}

// assertSealedCookieRetracted pins the obligation of every refusal: the
// response must retract the value it just refused, with an empty value and an
// expiry in the past.
func assertSealedCookieRetracted(t *testing.T, response *http.Response, cookieName string, refusal string) {
	t.Helper()

	retracted := responseCookie(response.Cookies(), cookieName)
	if retracted == nil {
		t.Fatalf(
			"the %s cookie survives the response that refused it (%s): no Set-Cookie retracts it, so the browser keeps sending it until it expires",
			cookieName, refusal)
	}
	if strings.TrimSpace(retracted.Value) != "" {
		t.Fatalf("expected an empty retracted %s cookie value after %s, got %q", cookieName, refusal, retracted.Value)
	}
	if retracted.Expires.IsZero() || retracted.Expires.After(time.Now()) {
		t.Fatalf("expected the retracted %s cookie to expire in the past after %s, got expiry %s", cookieName, refusal, retracted.Expires)
	}
}
