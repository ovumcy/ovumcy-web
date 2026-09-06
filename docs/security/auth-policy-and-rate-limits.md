# Password & Auth Policy, Rate Limits

_Part of the [Ovumcy security policy](../../SECURITY.md)._

## Password & Auth Policy

**Local passwords:**

- Minimum length: 8 Unicode code points.
- Maximum length: 72 bytes — bcrypt's hard input limit. Longer submissions fail validation with the same stable weak-password error on every password-accepting flow instead of surfacing bcrypt's opaque hashing error.
- Required character classes: at least one uppercase letter, one lowercase letter, and one digit (`ValidatePasswordStrength` in `internal/services/password_policy.go`).
- Storage: bcrypt at cost 12 (the `passwordHashCost` constant in `internal/services`, above the library `bcrypt.DefaultCost` of 10) via `golang.org/x/crypto/bcrypt`. Hashes live in `users.password_hash`. A successful login whose stored hash predates this floor is opportunistically re-hashed at cost 12 in place (`UpdatePasswordHashOnly`), so the effective cost rises for existing accounts without forcing a reset; this transparent upgrade does **not** bump `auth_session_version` (the credential is unchanged).

**Recovery codes:**

- Shape: `OVUM-XXXX-XXXX-XXXX`, 12 characters drawn uniformly via `crypto/rand` from a 32-symbol Crockford-style base32 alphabet (`A`–`Z` without `I`/`O`, digits `2`–`9`) — 60 bits of effective entropy (`GenerateRecoveryCode`, `internal/services/auth_reset_policy.go`).
- Storage: bcrypt-hashed in `users.recovery_code_hash`. The plaintext is shown to the user exactly once at issuance and is never retrievable server-side afterwards.
- Online guessing is bounded by the per-account rate limiter (`Rate Limits`, below); the code's 60 bits of entropy and the bcrypt cost bound offline guessing if the database leaks. Recovery-code hashing involves no `SECRET_KEY` — `GenerateRecoveryCodeHash` is plain bcrypt and verification is a direct compare against `users.recovery_code_hash` — so a database leak **on its own** already permits offline candidate testing; treat any database compromise as a reason to regenerate outstanding recovery codes.

**Session tokens:**

- `ovumcy_auth` payload is sealed (AES-256-GCM under an HKDF-derived key, see *Cookies* in [docs/security/cryptography.md](cryptography.md#cookies)) and verified per request against `users.auth_session_version`.
- Issued by `setAuthCookie`; reissued inline on the originating device whenever `auth_session_version` is bumped (see *Session Invalidation on Credential Rotation* in [docs/security/oidc-and-sessions.md](oidc-and-sessions.md#session-invalidation-on-credential-rotation)).

**TOTP 2FA:**

- RFC 6238 with a 30-second step.
- Secrets are AES-256-GCM encrypted at rest with per-row AAD binding (see *Field-Level Encryption* in [docs/security/cryptography.md](cryptography.md#field-level-encryption)).
- Replay protection: `users.totp_last_used_step` carries the RFC 6238 step index of the last successfully consumed code. `ClaimTOTPStep` performs an atomic `UPDATE … WHERE totp_last_used_step < ?`, so the same code cannot be consumed twice and concurrent submissions of the same step collapse to a single winner.

## Rate Limits

Per-IP HTTP rate limits enforced by Fiber's limiter middleware. Defaults are tunable through environment variables:

| Endpoint | Default budget | Env override |
| --- | --- | --- |
| `POST /api/v1/sessions` | 8 requests / 15 minutes | `RATE_LIMIT_LOGIN_MAX`, `RATE_LIMIT_LOGIN_WINDOW` |
| `POST /api/v1/users` | 8 requests / 15 minutes | `RATE_LIMIT_REGISTER_MAX`, `RATE_LIMIT_REGISTER_WINDOW` |
| `POST /api/v1/password-resets` | 8 requests / 1 hour | `RATE_LIMIT_FORGOT_PASSWORD_MAX`, `RATE_LIMIT_FORGOT_PASSWORD_WINDOW` |
| `/auth/oidc/*` (OIDC sign-in) | 8 requests / 15 minutes | shares `RATE_LIMIT_LOGIN_MAX`, `RATE_LIMIT_LOGIN_WINDOW` |
| `DELETE /api/v1/sessions/current` | 60 requests / 15 minutes | `RATE_LIMIT_LOGOUT_MAX`, `RATE_LIMIT_LOGOUT_WINDOW` |
| `POST /lang` (language switch) | 300 requests / 1 minute | shares `RATE_LIMIT_API_MAX`, `RATE_LIMIT_API_WINDOW` |
| `/api/*` (catch-all) | 300 requests / 1 minute | `RATE_LIMIT_API_MAX`, `RATE_LIMIT_API_WINDOW` |
| `GET/HEAD /calendar/feed/:token.ics` | 20 requests / 1 minute | `RATE_LIMIT_CALENDAR_FEED_MAX`, `RATE_LIMIT_CALENDAR_FEED_WINDOW` |

A single-endpoint row above is matched the way the router matches, not by raw path bytes: routing is case-insensitive and ignores trailing slashes, so `POST /LANG` and `POST /lang/` reach the same handler as `POST /lang` and draw on the same budget. The match stays exact rather than prefix-wide — `POST /api/v1/sessions/2fa-challenge` does not spend the sign-in row's budget; it draws on the `/api` catch-all and on its own per-account TOTP budget below.

A HEAD request to the feed both counts against this budget and is answered by the feed: `RegisterRoutes` gives every GET route a HEAD route with the same handler chain, registered ahead of the terminal `NotFound` catch-all, so HEAD reaches `ServeCalendarFeed` and returns its status and headers with the body dropped on the wire. It used to reach the catch-all's 404 instead — fiber appends a GET route's auto-generated HEAD copy only at startup, behind every directly-registered `Use` middleware — which is why the row above once distinguished "rate-limited" from "answers the feed". A calendar client that probes with HEAD before fetching therefore spends two of the twenty, one per request.

Every one of them refuses in the application's own error format. A client that asked for JSON receives the shared envelope — `error` with the endpoint's stable key (`too_many_login_attempts` and its siblings), `error_detail` with the category and target — plus `retry_after_seconds`, an extension member echoing the `Retry-After` header, so it inherits that header's bound of whole seconds no larger than the configured window. A browser is answered as the flow needs: the auth and settings forms redirect back to the form with a flash, and the language switch — the only public form with no HTMX behind it — renders the localized status fragment, because a full-page navigation cannot display a JSON body. The `.ics` feed has no page, so it answers the envelope.

The calendar feed deliberately does **not** share the `/api/*` budget. When the budget was introduced it was the only unauthenticated endpoint paying a bcrypt compare on every well-formed request — the verifier check on a selector hit, or the timing-equalization dummy on a selector miss — so at the `/api` budget a single IP could spend 300 bcrypts per minute without any credential. Migration 032 moved verification to a keyed MAC (microseconds), which removed that CPU cliff for every row minted since; a row minted before it still pays one bcrypt until its first successful poll writes its MAC in.

Keep the separate budget and keep it small anyway. It bounds the residual bcrypt of not-yet-migrated rows, and it is the only limit on a cookieless surface that needs no credential to reach: a calendar client polls once per refresh interval (typically 15–60 minutes), so 20/minute is already generous for several devices behind one address. Widening it toward the `/api` budget would buy nothing for real clients.

Behind a trusted proxy (`TRUST_PROXY_ENABLED=true`), the per-IP key is the **rightmost untrusted `X-Forwarded-For` hop** relative to `TRUSTED_PROXIES` (`cmd/ovumcy/ratelimit.go` `rateLimitKeyGenerator`), not fiber's default leftmost `c.IP()`, so a client-spoofed XFF prefix cannot rotate the key and defeat the limit.

Plus per-account, identity-keyed budgets enforced by `AuthAttemptPolicy` (`internal/services/auth_attempt_policy.go`):

- Recovery-code redemption (`POST /api/v1/password-resets`): 8 failures / 1 hour, tuned by the same `RATE_LIMIT_FORGOT_PASSWORD_MAX` / `RATE_LIMIT_FORGOT_PASSWORD_WINDOW` pair as that endpoint's per-IP row above, so the two budgets never drift apart. Wired as the `recovery` scope in `internal/services/password_reset_service.go`. A code that is merely malformed spends the budget exactly as a wrong-but-well-formed one does, so failing the format check early is not a free retry; only a submission with no email at all falls back to the client-keyed bucket alone, there being no identity to key on.
- Login attempts: 8 failures / 15 minutes. The OIDC link-confirmation password challenge (`POST /auth/oidc/link-confirm`) draws from this same budget, so link-confirm cannot be used as a faster password oracle than the login form.
- Logout attempts: 20 per 15 minutes (account-scoped). Unlike the rows around it this one counts **every** logout, not only failures — `CheckAndRecordLogoutAttempt` records the attempt as soon as the session resolves (`internal/api/handlers_auth_session_login.go`), so a 21st legitimate logout inside the window is refused too. Tuned by `RATE_LIMIT_LOGOUT_ACCOUNT_MAX` / `RATE_LIMIT_LOGOUT_ACCOUNT_WINDOW` — deliberately its own pair, not the `RATE_LIMIT_LOGOUT_*` per-IP row above: the per-IP budget must stay wide enough for several owners behind one address, which is exactly why it cannot double as the account budget.
- TOTP login challenge: 5 failures / 15 minutes.
- TOTP disable: 5 failures / 15 minutes.
- Settings re-authentication: 5 failures / 15 minutes, covering every password-gated settings action except the TOTP disable, whose password check draws its own budget above — `POST /api/v1/users/current/data-wipe/validate`, `POST …/data-wipe`, `DELETE /api/v1/users/current`, `PUT …/password`, `PUT …/2fa` (the TOTP-enrollment confirmation), and `POST …/recovery-code` (recovery-code regeneration). Without it these would be faster password oracles than the login form (the `/api` catch-all allows 300 requests per minute against login's 8 per 15 minutes), and `/data-wipe/validate` changes no state, which makes it a pure oracle. Once the budget is spent the endpoints answer `429` even for the correct password. The budget is keyed on `(client, account)` and on the account alone, deliberately **not** on the client address by itself: several independent owners share one address on a household instance, and one owner mistyping must not lock out the others, while the account-wide bucket still caps an attacker rotating addresses.

Per-account budgets are keyed by `HMAC-SHA256(SECRET_KEY, "ovumcy.auth-attempt.identity.v1:" || identity)`, so the limiter never persists the raw identifier.
