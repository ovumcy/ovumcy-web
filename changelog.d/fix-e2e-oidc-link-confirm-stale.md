none

Test-only: the opt-in OIDC browser lane catches up with the closed public
link-confirm route. `e2e/auth-oidc.spec.ts` still drove the retired
`/auth/oidc/link-confirm` password page — it expected the callback to land
there, filled the confirmation password and asserted a session — while the
callback now refuses a first-time link outright, so the hybrid case could only
fail. The case now asserts the refusal (back on `/login`, the
`auth.error.sso_link_confirmation_unavailable` flash rendered, no auth cookie,
no pending-link cookie, no PII in the URL) and then links from the authenticated
Settings step-up card before proving a second SSO sign-in authenticates straight
through. No product code changes.
