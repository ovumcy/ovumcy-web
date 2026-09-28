none

Test-only: WEB-13 TS-L3 hardens the cookie tests in `internal/api` covering
oidc-logout-bridge, reset-password, register-pickup and calendar-feed-reveal
sealed cookies, plus the shared force-secure/TTL/transport-error suites. Each
failure/refusal path now asserts a concrete `StatusCode` and observes the
clearing `Set-Cookie` (empty value, `Expires` in the past, matching `Path`) on
the response that emits it, via a new `assertSealedCookieCleared` helper,
instead of inferring clearing from a later re-open returning an empty payload.
No production code changed.
