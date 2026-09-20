package api

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

// Cross-site step-up continuation.
//
// A provider on another registrable site returns the authorization code as a
// form_post, so the callback arrives as a CROSS-SITE POST. The step-up cookies
// are SameSite=None and reach it, but the session cookie is SameSite=Lax and
// does not: Lax withholds cookies on a cross-site POST. Every step-up purpose
// resolves the owner from that session (a request-carried user id is never
// trusted alone), so the callback cannot tell who is completing the action and
// refuses — the R2 failure.
//
// The bounce fixes that without widening any cookie's reach. The cross-site
// POST validates the sealed state, parks what it learned in a single-use
// continuation cookie, and 303s to a same-origin GET. A top-level GET
// navigation is exactly what SameSite=Lax permits, so the session cookie
// arrives there and the completion runs with the owner identified as before.
// The continuation itself stays Lax for the same reason — it only ever has to
// survive that one navigation — and is Secure, HttpOnly, path-scoped to the
// continue route, one-time, and valid for a minute.
const oidcStepupContinuationTTL = time.Minute

type oidcStepupContinuation struct {
	Stepup oidcStepupState `json:"stepup"`
	// Code is the authorization code the provider posted. It is carried rather
	// than re-read on the continue leg because the provider posted it to the
	// callback and nothing re-sends it; it is single-use at the provider too,
	// so a replayed continuation buys an attacker a code the token endpoint
	// has already burned.
	Code      string `json:"code"`
	ExpiresAt string `json:"expires_at"`
}

func newOIDCStepupContinuation(now time.Time, stepup oidcStepupState, code string) (oidcStepupContinuation, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if strings.TrimSpace(code) == "" {
		return oidcStepupContinuation{}, errors.New("oidc stepup continuation requires an authorization code")
	}
	if !stepup.validAt(now) {
		return oidcStepupContinuation{}, errors.New("oidc stepup continuation requires a valid step-up payload")
	}
	return oidcStepupContinuation{
		Stepup:    stepup,
		Code:      code,
		ExpiresAt: now.UTC().Add(oidcStepupContinuationTTL).Format(time.RFC3339Nano),
	}, nil
}

func (continuation oidcStepupContinuation) validAt(now time.Time) bool {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(continuation.ExpiresAt))
	if err != nil || !expiresAt.After(now.UTC()) {
		return false
	}
	return strings.TrimSpace(continuation.Code) != "" && continuation.Stepup.validAt(now)
}

var oidcStepupContinuationCookieSpec = sealedCookieSpec{
	name: oidcStepupContinuationCookieName,
	path: oidcCallbackContinuePath,
	// Lax, not None: the continuation only has to survive the one top-level
	// GET navigation this handler redirects to, which is precisely what Lax
	// allows. Widening it to None would hand the completion leg to any
	// cross-site request that can reach the route.
	forceSecure: true,
}

func (handler *Handler) setOIDCStepupContinuationCookie(c fiber.Ctx, continuation oidcStepupContinuation) error {
	if !handler.cookieSecure {
		return errors.New("oidc stepup continuation cookie requires secure transport")
	}
	if !continuation.validAt(time.Now()) {
		return errors.New("oidc stepup continuation cookie payload is required")
	}

	payload, err := json.Marshal(continuation)
	if err != nil {
		// codecov:ignore -- defensive: the payload is strings and a struct of
		// strings, which encoding/json cannot fail on.
		return err
	}
	return handler.writeSealedCookie(c, oidcStepupContinuationCookieSpec, payload, time.Now().Add(oidcStepupContinuationTTL))
}

// peekOIDCStepupContinuationCookie decodes the continuation WITHOUT clearing
// it. Clearing is the caller's own step once the payload has been validated,
// so a stray request to the continue route cannot destroy an in-flight
// completion: the same consume-after-validate ordering the transit cookies
// follow.
func (handler *Handler) peekOIDCStepupContinuationCookie(c fiber.Ctx) oidcStepupContinuation {
	raw := strings.TrimSpace(c.Cookies(oidcStepupContinuationCookieName))
	if raw == "" {
		return oidcStepupContinuation{}
	}

	decoded, err := handler.openCookieValue(oidcStepupContinuationCookieName, raw)
	if err != nil {
		return oidcStepupContinuation{}
	}

	continuation := oidcStepupContinuation{}
	if err := json.Unmarshal(decoded, &continuation); err != nil {
		return oidcStepupContinuation{}
	}
	if !continuation.validAt(time.Now()) {
		return oidcStepupContinuation{}
	}
	return continuation
}

func (handler *Handler) clearOIDCStepupContinuationCookie(c fiber.Ctx) {
	handler.clearSealedCookie(c, oidcStepupContinuationCookieSpec)
}

// refuseOIDCStepupContinueRequest is the first-party guard's exit for the
// continue route. It spends nothing and says nothing about whether a
// continuation was waiting: an off-origin initiator, an embed, or a
// speculative load all leave through the same settings refusal, so a page on
// another site learns neither that a step-up is in flight nor what it was for.
func (handler *Handler) refuseOIDCStepupContinueRequest(c fiber.Ctx, reason string) error {
	spec := authOIDCAuthenticationFailedErrorSpec()
	handler.logSecurityError(c, "auth.oidc_callback", spec, SecurityEventField{Key: "refused", Value: reason})
	return handler.redirectSettingsRefusal(c, spec)
}

// callbackArrivedCrossSite reports whether the browser says this request came
// from another site. Sec-Fetch-Site is set by the browser and page script
// cannot forge it (the Sec- prefix is a forbidden header name), so a request
// claiming same-origin cannot talk its way into the bounce. A request with no
// Fetch Metadata at all is treated as same-site: that is the pre-existing
// direct path, whose own refusal still applies if no session is present.
func callbackArrivedCrossSite(c fiber.Ctx) bool {
	return strings.TrimSpace(c.Get(headerSecFetchSite)) == secFetchSiteCrossSite
}

// dispatchStepupCompletion routes a validated step-up to the handler written
// for its purpose. The purpose is dispatched on, never inferred: validAt has
// already refused a payload whose purpose is unknown or whose fields do not
// match the purpose it names, and each completion handler re-checks its own.
// An unhandled purpose falls through to the ordinary refusal.
func (handler *Handler) dispatchStepupCompletion(c fiber.Ctx, state oidcStepupState, exchange oidcCallbackExchange) error {
	switch state.Purpose {
	case oidcStepupPurposeLocalPasswordSetup:
		return handler.completeLocalPasswordSetupReauth(c, state, exchange)
	case oidcStepupPurposeErasure:
		return handler.completeErasureStepupReauth(c, state, exchange)
	case oidcStepupPurposeIdentityLink:
		return handler.completeOIDCIdentityLinkStepup(c, state, exchange)
	}
	// codecov:ignore:start -- forward-compat guard: validAt refuses an unknown
	// purpose before dispatch, so a fourth purpose can only reach here by being
	// taught to validAt without being taught to this switch.
	spec := authOIDCAuthenticationFailedErrorSpec()
	handler.logSecurityError(c, "auth.oidc_callback", spec)
	return handler.redirectSettingsRefusal(c, spec)
	// codecov:ignore:end
}

// stepupActionForPurpose names the audit action a bounce refusal belongs to.
// The per-purpose completion handlers each log their own; the bounce runs
// before dispatch, so it has to derive the same name or the cross-site leg
// would report every refusal as a generic callback failure.
func stepupActionForPurpose(state oidcStepupState) string {
	switch state.Purpose {
	case oidcStepupPurposeLocalPasswordSetup:
		return "auth.local_password_setup.callback"
	case oidcStepupPurposeErasure:
		if flow, known := erasureStepupFlowFor(state.Operation); known {
			return flow.stepupAction
		}
		// codecov:ignore -- validAt refuses an erasure payload whose operation
		// is not one of the two known ones.
		return "auth.oidc_callback"
	case oidcStepupPurposeIdentityLink:
		return oidcIdentityLinkStepupAction
	default:
		// codecov:ignore -- validAt refuses an unknown purpose before any
		// caller here can reach it.
		return "auth.oidc_callback"
	}
}

// bounceStepupToSameSiteContinue validates what the cross-site POST carried
// and parks it for the same-origin GET that follows. Nothing is exchanged with
// the provider here and no action is committed: the code is still unspent when
// the continue leg resolves the session and completes the step-up.
func (handler *Handler) bounceStepupToSameSiteContinue(c fiber.Ctx, state oidcStepupState, exchange oidcCallbackExchange) error {
	// The refusals below name the purpose the owner actually started, not a
	// generic callback action: an operator reading the audit trail after a
	// failed erasure must not have to guess which step-up it was.
	action := stepupActionForPurpose(state)

	// State first: a callback that does not match the sealed state is not this
	// owner's flow and must not be parked for completion.
	if !state.matchesState(exchange.State) {
		spec := authOIDCAuthenticationFailedErrorSpec()
		handler.logSecurityError(c, action, spec)
		return handler.redirectSettingsRefusal(c, spec)
	}
	if exchange.Error != "" {
		spec := authOIDCUnavailableErrorSpec()
		handler.logSecurityError(c, action, spec)
		return handler.redirectSettingsRefusal(c, spec)
	}

	continuation, err := newOIDCStepupContinuation(time.Now(), state, exchange.Code)
	if err != nil {
		spec := authOIDCAuthenticationFailedErrorSpec()
		handler.logSecurityError(c, action, spec)
		return handler.redirectSettingsRefusal(c, spec)
	}
	if err := handler.setOIDCStepupContinuationCookie(c, continuation); err != nil {
		// codecov:ignore:start -- defensive: the payload validated one line above
		// and the route only runs on a secure deployment, so the two refusals
		// inside the setter are unreachable from here; what is left is an AEAD
		// seal error.
		spec := authOIDCUnavailableErrorSpec()
		handler.logSecurityError(c, action, spec)
		return handler.redirectSettingsRefusal(c, spec)
		// codecov:ignore:end
	}

	// A same-origin document, not a 303. Sec-Fetch-Site describes the whole
	// redirect CHAIN, so a redirect issued from this cross-site POST would
	// still arrive at the continue route labelled cross-site — and that route
	// spends a one-time hand-off, the very class requireFirstPartyRequest
	// guards. An interstitial served from this origin makes the next
	// navigation same-origin in fact, so the guard can stand there and a page
	// on another site cannot produce a request that satisfies it.
	return respondOIDCSameOriginHandoff(c, oidcCallbackContinuePath)
}

// ContinueOIDCStepup is the same-site half of the cross-site bounce: a
// top-level GET navigation, which SameSite=Lax delivers the session cookie to,
// so the completion below identifies the owner exactly as the direct callback
// does. It consumes the continuation only once the payload has validated, and
// the state carried inside it is re-checked by the completion handler.
func (handler *Handler) ContinueOIDCStepup(c fiber.Ctx) error {
	continuation := handler.peekOIDCStepupContinuationCookie(c)
	if !continuation.validAt(time.Now()) {
		spec := authOIDCAuthenticationFailedErrorSpec()
		handler.logSecurityError(c, "auth.oidc_callback", spec)
		return handler.redirectSettingsRefusal(c, spec)
	}
	handler.clearOIDCStepupContinuationCookie(c)

	return handler.dispatchStepupCompletion(c, continuation.Stepup, oidcCallbackExchange{
		Code:  continuation.Code,
		State: continuation.Stepup.State,
	})
}
