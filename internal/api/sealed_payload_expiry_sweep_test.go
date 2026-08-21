package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// Sealed payload expiry — the sweep that keeps the set closed.
//
// A sealed cookie's `Set-Cookie` `Expires` attribute is a hint the client is
// free to ignore, and the codec's open() has no notion of time, so a payload
// with no server-verified bound is honoured for as long as the AEAD key lives:
// a client that kept the sealed value replays it until the secret rotates.
// Each cookie that carries a bound is pinned by its own regressions; this file
// is the half they cannot cover — the N+1 sealed cookie added later, which
// would ship unbounded without reddening anything.
//
// THE RULE IS NOT "every sealed payload carries expires_at". Three cookies are
// correct without one, for two distinct reasons, and a sweep asserting the
// naive rule would be red on landing and then silenced with an allowlist — the
// exact shape this repository keeps removing. Both exemptions are therefore
// stated as PROPERTIES of the payload the mint actually produces, so a cookie
// added later is judged by what it is and never by being on a list:
//
//  1. the sealed bytes carry an opaque signed token that carries its own `exp`,
//     and the verifier that reads it refuses once the clock is past that moment
//     — the bound rides inside the wrapped value (`ovumcy_auth`,
//     `ovumcy_reset_password`);
//  2. the payload carries nothing usable on replay — it names no account, and
//     two mints from identical caller input seal identical bytes, so the server
//     contributed no secret, no handle and no bound of its own to replay
//     (`ovumcy_flash`).
//
// Everything else must carry a live bound its own reader refuses once past. A
// cookie the sweep cannot place in one of the three categories FAILS: an
// undecidable case is precisely the N+1 this exists to catch, so it is never
// skipped.
//
// BEHAVIOUR, NOT AST — decided deliberately, and the reason matters:
//
//   - the verdict is read from a live seal/open. The classification depends on
//     what a payload SERIALISES to, which its type does not show: `ovumcy_auth`
//     seals a raw JWT that is not JSON at all, and `ovumcy_reset_password`
//     seals a JSON struct whose `token` field is a JWT. Both read as "a struct
//     with no expiry" to a type-level sweep — the same measure-don't-model trap
//     that already made payload-shaped size estimates for these two cookies
//     wrong by half.
//   - "the reader compares it against the clock" is behaviour. An AST form
//     would have to recognise every spelling of that comparison —
//     `time.Now().After(...)`, a `validAt(now)` method, a shared helper — and
//     would still pass a reader that computes the comparison and ignores the
//     result. Here the payload's own bound is rewritten into the past,
//     re-sealed with the production codec, and handed back to the production
//     reader, which must refuse it. The route-table guard
//     (cmd/ovumcy/owner_only_route_chain_guard_test.go) reads the live artefact
//     for the same reason.
//   - only the ROSTER comes from the package source, because there is no
//     runtime registry of sealed cookies to enumerate — the same half
//     TestEverySecretRevealClaimsItsConsumptionMark derives from the source,
//     and for the same reason. Deriving it is what covers a cookie added later.
//
// The limit, stated so no reader concludes the broader claim: exemption 2 is
// decided from the payload's contents and the mint's determinism, so a payload
// that named no account and carried only a CALLER-supplied secret would be read
// as inert. No cookie in the tree has that shape, and one that did would still
// have to be minted and read here before the sweep would pass it at all.

const (
	// A 32-byte key: the codec's HKDF input, and the HS256 key both token
	// verifiers below are handed.
	sealedExpirySweepKeyMaterial = "0123456789abcdef0123456789abcdef"
	sealedExpirySweepUserID      = uint(7)
	// A bcrypt-shaped stored value. The reset token's password-state
	// fingerprint and the step-up state's prepared-hash field each need one
	// that is merely non-empty.
	sealedExpirySweepStoredHash = "$2a$10$sweepsweepsweepsweepsuOSEyMc0nDXjcTPh4uBAeEcJ0KpNCFe"
	// The window a freshly minted bound must fall in to read as live. A value
	// outside it is not a bound this mint just issued: the zero time a stripped
	// mint site serialises lands before now, and so would an issued-at, so
	// neither can be mistaken for the expiry.
	sealedExpirySweepMaxTTL = 400 * 24 * time.Hour
)

// sealedCookieExpiryProbe drives one sealed cookie through its production mint
// and its production reader. It cannot express an exemption: it says only HOW
// to mint and HOW to read, and every verdict comes from the bytes the mint
// sealed.
type sealedCookieExpiryProbe struct {
	// mint runs the production mint path on a real request.
	mint func(handler *Handler, c fiber.Ctx) error
	// honours reports whether the production reader accepted the value
	// presented on this request. Required for a payload-bound cookie: without
	// it the sweep could see the field and never learn whether anything reads
	// it.
	honours func(handler *Handler, c fiber.Ctx) bool
	// verifyTokenAt validates the opaque token the payload carries as of a
	// given clock. Required for the opaque-token category, which is exempt from
	// carrying a payload bound only because this refuses once past it.
	verifyTokenAt func(handler *Handler, token string, now time.Time) error
}

// sealedCookieExpiryProbes is keyed by cookie name. It is NOT an allowlist: a
// sealed cookie missing from it fails the sweep, and one present in it is
// judged by the payload its mint produces, not by being here.
var sealedCookieExpiryProbes = map[string]sealedCookieExpiryProbe{
	authCookieName: {
		mint: func(handler *Handler, c fiber.Ctx) error {
			_, err := handler.setAuthCookie(c, &models.User{
				ID:                 sealedExpirySweepUserID,
				Role:               models.RoleOwner,
				AuthSessionVersion: 1,
			}, false)
			return err
		},
		verifyTokenAt: func(handler *Handler, token string, now time.Time) error {
			_, err := services.ParseAuthSessionToken(handler.secretKey, token, now)
			return err
		},
	},
	resetPasswordCookieName: {
		mint: func(handler *Handler, c fiber.Ctx) error {
			token, err := services.BuildPasswordResetToken(
				handler.secretKey, sealedExpirySweepUserID, sealedExpirySweepStoredHash, 0, time.Now())
			if err != nil {
				return err
			}
			return handler.setResetPasswordCookie(c, token, false)
		},
		honours: func(handler *Handler, c fiber.Ctx) bool {
			token, _ := handler.readResetPasswordCookie(c)
			return strings.TrimSpace(token) != ""
		},
		verifyTokenAt: func(handler *Handler, token string, now time.Time) error {
			_, err := services.ParsePasswordResetToken(handler.secretKey, token, now)
			return err
		},
	},
	flashCookieName: {
		mint: func(handler *Handler, c fiber.Ctx) error {
			handler.setFlashCookie(c, FlashPayload{
				SettingsError: "settings.error.generic",
				ForgotEmail:   "owner@example.test",
			})
			return nil
		},
		honours: func(handler *Handler, c fiber.Ctx) bool {
			return strings.TrimSpace(handler.popFlashCookie(c).SettingsError) != ""
		},
	},
	calendarFeedRevealCookieName: {
		mint: func(handler *Handler, c fiber.Ctx) error {
			return handler.setCalendarFeedRevealCookie(
				c, sealedExpirySweepUserID, "https://ovumcy.example.test/calendar/feed/AAAAAAAAAAAAAAAA.ics", false)
		},
		honours: func(handler *Handler, c fiber.Ctx) bool {
			return strings.TrimSpace(handler.readCalendarFeedRevealState(c, sealedExpirySweepUserID).FeedURL) != ""
		},
	},
	recoveryCodeCookieName: {
		mint: func(handler *Handler, c fiber.Ctx) error {
			return handler.setRecoveryCodeIssuanceCookie(
				c, sealedExpirySweepUserID, "OVUM-A1B2-C3D4-E5F6", "/settings", "")
		},
		honours: func(handler *Handler, c fiber.Ctx) bool {
			state := handler.readRecoveryCodeDisplayState(c, sealedExpirySweepUserID, "/dashboard")
			return strings.TrimSpace(state.RecoveryCode) != ""
		},
	},
	totpPendingCookieName: {
		mint: func(handler *Handler, c fiber.Ctx) error {
			return handler.setTOTPPendingCookie(c, sealedExpirySweepUserID, false)
		},
		honours: func(handler *Handler, c fiber.Ctx) bool {
			userID, _, err := handler.parseTOTPPendingCookie(c)
			return err == nil && userID != 0
		},
	},
	totpSetupCookieName: {
		mint: func(handler *Handler, c fiber.Ctx) error {
			return handler.setTOTPSetupCookie(c, sealedExpirySweepUserID, "JBSWY3DPEHPK3PXP")
		},
		honours: func(handler *Handler, c fiber.Ctx) bool {
			secret, err := handler.parseTOTPSetupCookie(c, sealedExpirySweepUserID)
			return err == nil && strings.TrimSpace(secret) != ""
		},
	},
	oidcStateCookieName: {
		mint: func(handler *Handler, c fiber.Ctx) error {
			state, err := newOIDCAuthState(time.Now())
			if err != nil {
				return err
			}
			return handler.setOIDCStateCookie(c, state)
		},
		honours: func(handler *Handler, c fiber.Ctx) bool {
			return strings.TrimSpace(handler.popOIDCStateCookie(c).State) != ""
		},
	},
	oidcStepupCookieName: {
		mint: func(handler *Handler, c fiber.Ctx) error {
			state, err := newOIDCStepupState(
				time.Now(), oidcStepupPurposeLocalPasswordSetup, sealedExpirySweepUserID, sealedExpirySweepStoredHash)
			if err != nil {
				return err
			}
			return handler.setOIDCStepupCookie(c, state)
		},
		honours: func(handler *Handler, c fiber.Ctx) bool {
			return strings.TrimSpace(handler.popOIDCStepupCookie(c).State) != ""
		},
	},
	oidcLinkPendingCookieName: {
		mint: func(handler *Handler, c fiber.Ctx) error {
			payload, err := newOIDCLinkPendingPayload(
				time.Now(), sealedExpirySweepUserID, "https://idp.example.test", "subject-1", "owner@example.test")
			if err != nil {
				return err
			}
			return handler.setOIDCLinkPendingCookie(c, payload)
		},
		honours: func(handler *Handler, c fiber.Ctx) bool {
			_, ok := handler.readOIDCLinkPendingCookie(c)
			return ok
		},
	},
	oidcLogoutBridgeCookieName: {
		mint: func(handler *Handler, c fiber.Ctx) error {
			return handler.setOIDCLogoutBridgeCookie(c, "sweep-session-id", sealedExpirySweepUserID, time.Now())
		},
		honours: func(handler *Handler, c fiber.Ctx) bool {
			return strings.TrimSpace(handler.readOIDCLogoutBridgeCookie(c, time.Now()).SessionID) != ""
		},
	},
	registerPickupCookieName: {
		mint: func(handler *Handler, c fiber.Ctx) error {
			payload, err := newRegisterPickupPayload(time.Now(), "OVUM-A1B2-C3D4-E5F6")
			if err != nil {
				return err
			}
			return handler.setRegisterPickupCookie(c, payload)
		},
		honours: func(handler *Handler, c fiber.Ctx) bool {
			_, ok := handler.popRegisterPickupCookie(c)
			return ok
		},
	},
}

// TestEverySealedCookiePayloadCarriesAServerVerifiedExpiry requires every
// sealed cookie declared in this package to be bounded on the server: by a live
// expiry in its own payload that its reader refuses once past, by the opaque
// token it wraps, or — only where the payload carries nothing usable on replay
// — not at all.
func TestEverySealedCookiePayloadCarriesAServerVerifiedExpiry(t *testing.T) {
	assertSealedPayloadClassifierAnswersBothWays(t)
	assertSealedCookieRosterExtractorAnswersBothWays(t)

	names := sealedCookieNamesDeclaredInPackage(t)
	if len(names) == 0 {
		t.Fatal("the sweep found no sealed cookie declaration at all — it is measuring nothing")
	}

	boundedCookies := 0
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			probe, known := sealedCookieExpiryProbes[name]
			if !known {
				t.Fatalf(
					"%s: nothing here mints and reads this sealed cookie, so the sweep cannot decide whether its payload is bounded — an unclassifiable sealed cookie fails rather than being skipped. Add its production mint and its production reader to sealedCookieExpiryProbes",
					name)
			}

			handler := newSealedExpirySweepHandler()
			sealed := mintSealedCookieForSweep(t, handler, name, probe)
			plaintext := openSealedCookieForSweep(t, handler, name, sealed)

			verdict, err := classifySealedPayload(plaintext, time.Now())
			if err != nil {
				t.Fatalf("%s: %v — a sealed payload the sweep cannot classify is the defect it exists to catch, not a case to skip", name, err)
			}

			switch verdict.kind {
			case sealedPayloadKindBounded:
				boundedCookies++
				assertBoundedSealedCookieRefusedOnceExpired(t, handler, name, probe, sealed, plaintext, verdict)
			case sealedPayloadKindOpaque:
				assertOpaqueBoundRefusedOnceExpired(t, handler, name, probe, sealed, verdict)
			case sealedPayloadKindInert:
				assertSealedPayloadCarriesNothingReplayable(t, handler, name, probe, plaintext, verdict)
			}
		})
	}

	if boundedCookies == 0 {
		t.Fatal("no sealed cookie exercised the read side — the sweep proved nothing about any reader comparing a bound against the clock")
	}
}

// assertBoundedSealedCookieRefusedOnceExpired proves the bound is the server's
// and not the browser's: the reader honours the freshly minted value (the
// positive anchor, without which the refusal below would pass just as well
// against a reader that refuses everything) and refuses the same payload once
// its own bound is rewritten into the past and re-sealed with the production
// codec.
func assertBoundedSealedCookieRefusedOnceExpired(
	t *testing.T,
	handler *Handler,
	name string,
	probe sealedCookieExpiryProbe,
	sealed string,
	plaintext []byte,
	verdict sealedPayloadVerdict,
) {
	t.Helper()

	if probe.honours == nil {
		t.Fatalf(
			"%s: carries %q but the sweep has no reader to ask — a bound nothing reads is a browser hint, so the probe must supply the production reader",
			name, verdict.bound.key)
	}
	if !readSealedCookieForSweep(t, handler, name, sealed, probe.honours) {
		t.Fatalf(
			"%s: the reader refused a freshly minted value — the probe is not exercising the production read path, so nothing below it proves anything",
			name)
	}

	expired := rewriteSealedPayloadBound(t, plaintext, verdict.bound, time.Now().Add(-time.Hour))
	resealed := resealSealedPayloadForSweep(t, handler, name, expired)
	if readSealedCookieForSweep(t, handler, name, resealed, probe.honours) {
		t.Fatalf(
			"%s: the reader honoured a payload whose %q is an hour in the past — the field is minted but never compared against the clock, so a client that kept the sealed value replays it until the key rotates",
			name, verdict.bound.key)
	}
}

// assertOpaqueBoundRefusedOnceExpired proves the exemption it grants: the
// wrapped token carries its own `exp`, and the verifier that reads it refuses
// once the clock is past that moment.
func assertOpaqueBoundRefusedOnceExpired(
	t *testing.T,
	handler *Handler,
	name string,
	probe sealedCookieExpiryProbe,
	sealed string,
	verdict sealedPayloadVerdict,
) {
	t.Helper()

	if probe.verifyTokenAt == nil {
		t.Fatalf(
			"%s: seals an opaque token, which is exempt from carrying a payload expiry only because the token carries a bound the server VERIFIES — the probe must supply the verifier that refuses it once expired",
			name)
	}
	if err := probe.verifyTokenAt(handler, verdict.token, time.Now()); err != nil {
		t.Fatalf("%s: the verifier refused a freshly minted token (%v) — the probe is not exercising the real verification path", name, err)
	}
	if err := probe.verifyTokenAt(handler, verdict.token, verdict.bound.at.Add(time.Minute)); err == nil {
		t.Fatalf(
			"%s: the verifier accepted the wrapped token a minute past its own `exp` — the sealed cookie then carries no bound at all, neither in the payload nor in the token",
			name)
	}
	if probe.honours != nil && !readSealedCookieForSweep(t, handler, name, sealed, probe.honours) {
		t.Fatalf("%s: the reader refused a freshly minted value — the probe is not exercising the production read path", name)
	}
}

// assertSealedPayloadCarriesNothingReplayable is the only exemption resting on
// the payload alone, so both halves are measured: the payload names no account,
// and two mints from identical caller input seal identical bytes — so the
// server put nothing of its own into it, and replaying it hands the client back
// exactly what the client already had.
func assertSealedPayloadCarriesNothingReplayable(
	t *testing.T,
	handler *Handler,
	name string,
	probe sealedCookieExpiryProbe,
	plaintext []byte,
	verdict sealedPayloadVerdict,
) {
	t.Helper()

	if verdict.attributedBy != "" {
		t.Fatalf(
			"%s: carries no live expiry, and its payload names an account in %q — an account-scoped payload is usable on replay, so it must carry a bound its reader compares against the clock",
			name, verdict.attributedBy)
	}

	second := openSealedCookieForSweep(t, handler, name, mintSealedCookieForSweep(t, handler, name, probe))
	if string(second) != string(plaintext) {
		t.Fatalf(
			"%s: carries no live expiry, yet two mints from identical input sealed different bytes (%s vs %s) — the payload carries something the server generated, and whatever that is replays unbounded until the key rotates",
			name, string(plaintext), string(second))
	}
}

func newSealedExpirySweepHandler() *Handler {
	return &Handler{
		secretKey: []byte(sealedExpirySweepKeyMaterial),
		location:  time.UTC,
		// The two cross-site OIDC cookies refuse to mint on a deployment that
		// is not on secure transport.
		cookieSecure: true,
		// BuildAuthSessionTokenWithSessionID needs no repository, and nothing
		// in this sweep resolves a user.
		authService: services.NewAuthService(nil),
	}
}

// mintSealedCookieForSweep runs the probe's production mint on a real request
// and returns the sealed value the client would receive.
func mintSealedCookieForSweep(t *testing.T, handler *Handler, name string, probe sealedCookieExpiryProbe) string {
	t.Helper()

	response := runSealedCookieProbeRequest(t, nil, func(c fiber.Ctx) error {
		return probe.mint(handler, c)
	})
	defer func() { _ = response.Body.Close() }()

	cookie := responseCookie(response.Cookies(), name)
	if cookie == nil || strings.TrimSpace(cookie.Value) == "" {
		t.Fatalf("%s: the mint wrote no sealed cookie", name)
	}
	return cookie.Value
}

// readSealedCookieForSweep presents a sealed value to the probe's production
// reader on a real request and reports whether the reader honoured it.
func readSealedCookieForSweep(
	t *testing.T, handler *Handler, name string, sealed string, honours func(*Handler, fiber.Ctx) bool,
) bool {
	t.Helper()

	honoured := false
	response := runSealedCookieProbeRequest(t, map[string]string{name: sealed}, func(c fiber.Ctx) error {
		honoured = honours(handler, c)
		return nil
	})
	defer func() { _ = response.Body.Close() }()
	return honoured
}

func runSealedCookieProbeRequest(t *testing.T, cookies map[string]string, run func(c fiber.Ctx) error) *http.Response {
	t.Helper()

	app := fiber.New()
	app.Get("/sweep", func(c fiber.Ctx) error {
		if err := run(c); err != nil {
			return err
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/sweep", nil)
	for cookieName, value := range cookies {
		request.AddCookie(&http.Cookie{Name: cookieName, Value: value})
	}
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("sweep request: %v", err)
	}
	if response.StatusCode != fiber.StatusNoContent {
		t.Fatalf("sweep request answered %d — the probe returned an error instead of running the production path", response.StatusCode)
	}
	return response
}

func openSealedCookieForSweep(t *testing.T, handler *Handler, name string, sealed string) []byte {
	t.Helper()

	plaintext, err := handler.openCookieValue(name, sealed)
	if err != nil {
		t.Fatalf("%s: open the sealed value the mint produced: %v", name, err)
	}
	return plaintext
}

func resealSealedPayloadForSweep(t *testing.T, handler *Handler, name string, plaintext []byte) string {
	t.Helper()

	sealed, err := handler.sealCookieValue(name, plaintext)
	if err != nil {
		t.Fatalf("%s: reseal payload: %v", name, err)
	}
	return sealed
}

// The three categories a sealed payload can fall into. Anything the classifier
// cannot place in one of them is an error, never a fourth silent bucket.
const (
	sealedPayloadKindBounded = "payload-bound"
	sealedPayloadKindOpaque  = "opaque-bound"
	sealedPayloadKindInert   = "inert"
)

// sealedPayloadBoundField is one live expiry found in a payload, together with
// the encoding it was written in, so the sweep can move it into the past
// without changing its shape.
type sealedPayloadBoundField struct {
	key    string
	at     time.Time
	encode func(at time.Time) any
}

type sealedPayloadVerdict struct {
	kind         string
	bound        sealedPayloadBoundField
	token        string
	attributedBy string
}

// classifySealedPayload reads the bytes a mint actually sealed and places them
// in one of the three categories, or returns an error naming why it cannot. It
// never falls through to an exemption: the rule is in the file header, and an
// undecidable payload is the defect, not a case to wave past.
func classifySealedPayload(plaintext []byte, now time.Time) (sealedPayloadVerdict, error) {
	trimmed := strings.TrimSpace(string(plaintext))
	if looksLikeSignedToken(trimmed) {
		expiry, ok := signedTokenExpiry(trimmed)
		if !ok {
			return sealedPayloadVerdict{}, fmt.Errorf("the sealed bytes are a signed token carrying no `exp` claim, so nothing bounds them")
		}
		return sealedPayloadVerdict{
			kind:  sealedPayloadKindOpaque,
			token: trimmed,
			bound: sealedPayloadBoundField{key: "exp", at: expiry},
		}, nil
	}

	fields, err := decodeSealedPayloadObject(plaintext)
	if err != nil {
		return sealedPayloadVerdict{}, fmt.Errorf(
			"the sealed bytes are neither a signed token nor a JSON object (%v), so the sweep cannot tell what bounds them", err)
	}

	bounds := []sealedPayloadBoundField{}
	tokenKeys := []string{}
	tokens := []string{}
	tokenExpiries := []time.Time{}
	attributedBy := ""
	for _, key := range sortedPayloadKeys(fields) {
		value := fields[key]
		if bound, ok := sealedPayloadBoundFrom(key, value, now); ok {
			bounds = append(bounds, bound)
			continue
		}
		if text, ok := value.(string); ok && looksLikeSignedToken(text) {
			expiry, ok := signedTokenExpiry(text)
			if !ok {
				return sealedPayloadVerdict{}, fmt.Errorf("field %q carries a signed token with no `exp` claim, so nothing bounds it", key)
			}
			tokenKeys = append(tokenKeys, key)
			tokens = append(tokens, text)
			tokenExpiries = append(tokenExpiries, expiry)
			continue
		}
		if attributedBy == "" && sealedPayloadFieldNamesAnAccount(key, value) {
			attributedBy = key
		}
	}

	if len(bounds) > 1 {
		return sealedPayloadVerdict{}, fmt.Errorf(
			"the payload carries more than one live bound (%s) and the sweep cannot tell which one the reader enforces", strings.Join(boundKeys(bounds), ", "))
	}
	if len(bounds) == 1 {
		return sealedPayloadVerdict{kind: sealedPayloadKindBounded, bound: bounds[0], attributedBy: attributedBy}, nil
	}
	if len(tokens) > 1 {
		return sealedPayloadVerdict{}, fmt.Errorf(
			"the payload wraps more than one signed token (%s) and the sweep cannot tell which one bounds it", strings.Join(tokenKeys, ", "))
	}
	if len(tokens) == 1 {
		return sealedPayloadVerdict{
			kind:         sealedPayloadKindOpaque,
			token:        tokens[0],
			bound:        sealedPayloadBoundField{key: tokenKeys[0] + ".exp", at: tokenExpiries[0]},
			attributedBy: attributedBy,
		}, nil
	}
	return sealedPayloadVerdict{kind: sealedPayloadKindInert, attributedBy: attributedBy}, nil
}

func boundKeys(bounds []sealedPayloadBoundField) []string {
	keys := make([]string, 0, len(bounds))
	for _, bound := range bounds {
		keys = append(keys, bound.key)
	}
	return keys
}

func decodeSealedPayloadObject(plaintext []byte) (map[string]any, error) {
	decoder := json.NewDecoder(strings.NewReader(string(plaintext)))
	decoder.UseNumber()
	fields := map[string]any{}
	if err := decoder.Decode(&fields); err != nil {
		return nil, err
	}
	return fields, nil
}

func sortedPayloadKeys(fields map[string]any) []string {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// sealedPayloadBoundFrom recognises the encodings this package's payloads write
// an expiry in — an RFC3339 timestamp (what a time.Time marshals to), the
// fixed-width hex unix nanos the register pickup uses, and a bare unix number —
// and reports the field only when the instant is LIVE. That last condition is
// load-bearing: a payload whose mint stopped setting the field still serialises
// the zero time, and a zero time read as "a bound" would exempt exactly the
// defect this sweep exists to catch.
func sealedPayloadBoundFrom(key string, value any, now time.Time) (sealedPayloadBoundField, bool) {
	switch typed := value.(type) {
	case string:
		if at, err := time.Parse(time.RFC3339Nano, typed); err == nil {
			return sealedPayloadBoundField{
				key:    key,
				at:     at,
				encode: func(at time.Time) any { return at.UTC().Format(time.RFC3339Nano) },
			}, sealedPayloadBoundIsLive(at, now)
		}
		if len(typed) == 16 {
			if nanos, err := strconv.ParseInt(typed, 16, 64); err == nil && nanos > 0 {
				at := time.Unix(0, nanos).UTC()
				return sealedPayloadBoundField{
					key:    key,
					at:     at,
					encode: func(at time.Time) any { return fmt.Sprintf("%016x", at.UnixNano()) },
				}, sealedPayloadBoundIsLive(at, now)
			}
		}
	case json.Number:
		number, err := typed.Int64()
		if err != nil {
			return sealedPayloadBoundField{}, false
		}
		if at, scale, ok := instantFromUnixNumber(number); ok {
			return sealedPayloadBoundField{
				key:    key,
				at:     at,
				encode: func(at time.Time) any { return json.Number(strconv.FormatInt(at.UnixNano()/scale, 10)) },
			}, sealedPayloadBoundIsLive(at, now)
		}
	}
	return sealedPayloadBoundField{}, false
}

func sealedPayloadBoundIsLive(at time.Time, now time.Time) bool {
	return at.After(now) && at.Before(now.Add(sealedExpirySweepMaxTTL))
}

// instantFromUnixNumber reads a bare number as a unix instant, returning the
// nanoseconds-per-unit scale it was written in so the same field can be
// rewritten in the same unit.
func instantFromUnixNumber(number int64) (time.Time, int64, bool) {
	switch {
	case number >= 1e9 && number < 1e11:
		return time.Unix(number, 0).UTC(), int64(time.Second), true
	case number >= 1e12 && number < 1e14:
		return time.UnixMilli(number).UTC(), int64(time.Millisecond), true
	case number >= 1e15 && number < 1e17:
		return time.UnixMicro(number).UTC(), int64(time.Microsecond), true
	case number >= 1e18:
		return time.Unix(0, number).UTC(), 1, true
	default:
		return time.Time{}, 0, false
	}
}

// sealedPayloadFieldNamesAnAccount reports whether a field attributes the
// payload to an account. An attributed payload is usable on replay by
// definition — it names whose data it unlocks — so it can never take the
// nothing-to-replay exemption.
func sealedPayloadFieldNamesAnAccount(key string, value any) bool {
	normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
	named := strings.Contains(normalized, "userid") ||
		strings.Contains(normalized, "ownerid") ||
		strings.Contains(normalized, "accountid") ||
		strings.HasSuffix(normalized, "uid")
	return named && !sealedPayloadValueIsZero(value)
}

func sealedPayloadValueIsZero(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case bool:
		return !typed
	case string:
		return strings.TrimSpace(typed) == ""
	case json.Number:
		number, err := typed.Float64()
		return err == nil && number == 0
	default:
		return false
	}
}

// rewriteSealedPayloadBound moves one payload's bound to a given instant,
// keeping the encoding the mint wrote it in, and returns the re-serialised
// payload for the caller to re-seal.
func rewriteSealedPayloadBound(t *testing.T, plaintext []byte, bound sealedPayloadBoundField, at time.Time) []byte {
	t.Helper()

	fields, err := decodeSealedPayloadObject(plaintext)
	if err != nil {
		t.Fatalf("decode payload to rewrite %q: %v", bound.key, err)
	}
	fields[bound.key] = bound.encode(at)
	rewritten, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("re-serialise payload with %q rewritten: %v", bound.key, err)
	}
	return rewritten
}

func looksLikeSignedToken(candidate string) bool {
	segments := strings.Split(strings.TrimSpace(candidate), ".")
	if len(segments) != 3 {
		return false
	}
	for _, segment := range segments {
		if segment == "" {
			return false
		}
		if _, err := base64.RawURLEncoding.DecodeString(segment); err != nil {
			return false
		}
	}
	return true
}

func signedTokenExpiry(candidate string) (time.Time, bool) {
	segments := strings.Split(strings.TrimSpace(candidate), ".")
	if len(segments) != 3 {
		return time.Time{}, false
	}
	claims, err := base64.RawURLEncoding.DecodeString(segments[1])
	if err != nil {
		return time.Time{}, false
	}
	payload := struct {
		Exp *int64 `json:"exp"`
	}{}
	if err := json.Unmarshal(claims, &payload); err != nil || payload.Exp == nil || *payload.Exp <= 0 {
		return time.Time{}, false
	}
	return time.Unix(*payload.Exp, 0).UTC(), true
}

// sealedCookieNamesDeclaredInPackage derives the roster from every
// sealedCookieSpec declared in the package's production files, so a cookie
// added later is judged by this sweep without anyone remembering to list it.
func sealedCookieNamesDeclaredInPackage(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	fileSet := token.NewFileSet()
	files := []*ast.File{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fileSet, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		files = append(files, parsed)
	}

	names, err := sealedCookieNamesFromFiles(files)
	if err != nil {
		t.Fatalf("derive the sealed-cookie roster: %v", err)
	}
	return names
}

func sealedCookieNamesFromFiles(files []*ast.File) ([]string, error) {
	constants := stringConstantsFromFiles(files)

	names := []string{}
	seen := map[string]bool{}
	var failure error
	for _, file := range files {
		ast.Inspect(file, func(node ast.Node) bool {
			composite, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			identifier, ok := composite.Type.(*ast.Ident)
			if !ok || identifier.Name != "sealedCookieSpec" {
				return true
			}

			declared := ""
			for _, element := range composite.Elts {
				pair, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := pair.Key.(*ast.Ident)
				if !ok || key.Name != "name" {
					continue
				}
				resolved, ok := resolveSealedCookieName(pair.Value, constants)
				if !ok {
					failure = fmt.Errorf("a sealedCookieSpec names its cookie with an expression this sweep cannot resolve to a constant")
					return false
				}
				declared = resolved
			}
			if declared == "" {
				failure = fmt.Errorf("a sealedCookieSpec declares no cookie name")
				return false
			}
			if !seen[declared] {
				seen[declared] = true
				names = append(names, declared)
			}
			return true
		})
	}
	sort.Strings(names)
	return names, failure
}

func stringConstantsFromFiles(files []*ast.File) map[string]string {
	constants := map[string]string{}
	for _, file := range files {
		for _, declaration := range file.Decls {
			group, ok := declaration.(*ast.GenDecl)
			if !ok || group.Tok != token.CONST {
				continue
			}
			for _, specification := range group.Specs {
				value, ok := specification.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for index, identifier := range value.Names {
					if index >= len(value.Values) {
						continue
					}
					literal, ok := value.Values[index].(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						continue
					}
					text, err := strconv.Unquote(literal.Value)
					if err != nil {
						continue
					}
					constants[identifier.Name] = text
				}
			}
		}
	}
	return constants
}

func resolveSealedCookieName(expression ast.Expr, constants map[string]string) (string, bool) {
	switch typed := expression.(type) {
	case *ast.Ident:
		value, ok := constants[typed.Name]
		return value, ok
	case *ast.BasicLit:
		if typed.Kind != token.STRING {
			return "", false
		}
		value, err := strconv.Unquote(typed.Value)
		return value, err == nil
	default:
		return "", false
	}
}

// assertSealedPayloadClassifierAnswersBothWays anchors the classifier on
// fixtures this file owns — payloads that must classify each way — so a
// classifier that stopped recognising bounds could not report success over a
// tree it no longer understands. Nothing here reads the live roster: an anchor
// conditioned on the data it judges stops firing the day that data changes.
func assertSealedPayloadClassifierAnswersBothWays(t *testing.T) {
	t.Helper()

	now := time.Now()
	live := now.Add(5 * time.Minute).UTC().Format(time.RFC3339Nano)

	bounded := classifySealedPayloadForTest(t, `{"user_id":1,"expires_at":"`+live+`"}`, now)
	if bounded.kind != sealedPayloadKindBounded || bounded.bound.key != "expires_at" {
		t.Fatalf("a payload carrying a live RFC3339 expiry must read as bounded, got %q/%q", bounded.kind, bounded.bound.key)
	}

	// Rewriting the bound must actually move it out of the live window, or the
	// refusal leg below would be presenting an unexpired payload.
	rewritten := rewriteSealedPayloadBound(t, []byte(`{"user_id":1,"expires_at":"`+live+`"}`), bounded.bound, now.Add(-time.Hour))
	if again := classifySealedPayloadForTest(t, string(rewritten), now); again.kind == sealedPayloadKindBounded {
		t.Fatal("a bound rewritten an hour into the past must no longer read as live")
	}

	hexNow := fmt.Sprintf("%016x", now.Add(10*time.Minute).UnixNano())
	hexBound := classifySealedPayloadForTest(t, `{"nonce":"a1","rc":"OVUM-A1B2-C3D4-E5F6","exp":"`+hexNow+`"}`, now)
	if hexBound.kind != sealedPayloadKindBounded || hexBound.bound.key != "exp" {
		t.Fatalf("a payload carrying fixed-width hex unix nanos must read as bounded, got %q/%q", hexBound.kind, hexBound.bound.key)
	}

	unixBound := classifySealedPayloadForTest(t, `{"session_id":"s","user_id":1,"expires_at_unix":`+strconv.FormatInt(now.Add(time.Minute).Unix(), 10)+`}`, now)
	if unixBound.kind != sealedPayloadKindBounded || unixBound.bound.key != "expires_at_unix" {
		t.Fatalf("a payload carrying bare unix seconds must read as bounded, got %q/%q", unixBound.kind, unixBound.bound.key)
	}

	// The mutant this sweep exists for: a mint that stopped setting the field
	// still serialises the zero time, which must never read as a bound.
	stripped := classifySealedPayloadForTest(t, `{"user_id":1,"expires_at":"0001-01-01T00:00:00Z"}`, now)
	if stripped.kind != sealedPayloadKindInert || stripped.attributedBy != "user_id" {
		t.Fatalf("a payload whose expiry is the zero time must read as inert and attributed, got %q/%q", stripped.kind, stripped.attributedBy)
	}

	inert := classifySealedPayloadForTest(t, `{"auth_error":"auth.error.invalid_credentials"}`, now)
	if inert.kind != sealedPayloadKindInert || inert.attributedBy != "" {
		t.Fatalf("an unattributed payload with no bound must read as inert and unattributed, got %q/%q", inert.kind, inert.attributedBy)
	}

	signed, err := services.BuildPasswordResetToken(
		[]byte(sealedExpirySweepKeyMaterial), sealedExpirySweepUserID, sealedExpirySweepStoredHash, time.Hour, now)
	if err != nil {
		t.Fatalf("build the fixture token: %v", err)
	}
	bare := classifySealedPayloadForTest(t, signed, now)
	if bare.kind != sealedPayloadKindOpaque || bare.token != signed {
		t.Fatalf("sealed bytes that are a signed token must read as opaque-bound, got %q", bare.kind)
	}
	wrapped := classifySealedPayloadForTest(t, `{"token":"`+signed+`","forced":false}`, now)
	if wrapped.kind != sealedPayloadKindOpaque || wrapped.token != signed {
		t.Fatalf("a payload wrapping a signed token must read as opaque-bound, got %q", wrapped.kind)
	}
	if !wrapped.bound.at.After(now) {
		t.Fatal("the wrapped token's `exp` must be read out of the token itself")
	}

	if _, err := classifySealedPayload([]byte("neither a token nor an object"), now); err == nil {
		t.Fatal("sealed bytes the classifier cannot read must fail, never fall through to an exemption")
	}
	if _, err := classifySealedPayload([]byte(`{"user_id":1,"expires_at":"`+live+`","other_expiry":"`+live+`"}`), now); err == nil {
		t.Fatal("a payload carrying two live bounds must fail rather than pick one")
	}
}

func classifySealedPayloadForTest(t *testing.T, payload string, now time.Time) sealedPayloadVerdict {
	t.Helper()

	verdict, err := classifySealedPayload([]byte(payload), now)
	if err != nil {
		t.Fatalf("classify %s: %v", payload, err)
	}
	return verdict
}

// assertSealedCookieRosterExtractorAnswersBothWays anchors the roster half on
// its own fixtures: one declaration it must find, and one it must refuse to
// guess at rather than silently drop from the roster.
func assertSealedCookieRosterExtractorAnswersBothWays(t *testing.T) {
	t.Helper()

	found, err := sealedCookieNamesFromFiles([]*ast.File{parseSweepFixtureFile(t, `package fixture

const probeCookieName = "ovumcy_probe"

var probeCookieSpec = sealedCookieSpec{name: probeCookieName, path: "/"}

var notACookieSpec = struct{ name string }{name: "ovumcy_not_a_cookie"}
`)})
	if err != nil {
		t.Fatalf("the extractor must read a plain spec declaration: %v", err)
	}
	if len(found) != 1 || found[0] != "ovumcy_probe" {
		t.Fatalf("the extractor must resolve a spec's name constant, got %v", found)
	}

	if _, err := sealedCookieNamesFromFiles([]*ast.File{parseSweepFixtureFile(t, `package fixture

var probeCookieSpec = sealedCookieSpec{name: nameFromSomewhereElse, path: "/"}
`)}); err == nil {
		t.Fatal("a spec whose name the extractor cannot resolve must fail — a cookie missing from the roster is never swept")
	}
}

func parseSweepFixtureFile(t *testing.T, source string) *ast.File {
	t.Helper()

	parsed, err := parser.ParseFile(token.NewFileSet(), "fixture.go", source, 0)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return parsed
}
