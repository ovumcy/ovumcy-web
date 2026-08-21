none

Tests only: an account carrying both `must_change_password` and `totp_enabled`
is routed to the forced password reset and is never challenged for TOTP — the
intentional recovery path for an owner whose second factor is unusable after a
`SECRET_KEY` rotation. Nothing pinned that ordering, so flipping it reddened no
test anywhere; three cases now hold it at every caller — the login service, the
`POST /api/v1/sessions` route, and `POST /auth/oidc/link-confirm`, where a
reversal would have fallen through to issuing a session because that handler
branches only on the reset flag. No product code.
