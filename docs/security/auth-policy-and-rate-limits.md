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
- Online guessing is bounded by the per-account rate limiter (`Rate Limits`, below); the bcrypt cost bounds offline guessing if the database and `SECRET_KEY` leak together.

**Session tokens:**

- `ovumcy_auth` payload is sealed (AES-256-GCM under an HKDF-derived key, see *Cookies* above) and verified per request against `users.auth_session_version`.
- Issued by `setAuthCookie`; reissued inline on the originating device whenever `auth_session_version` is bumped (see *Session Invalidation on Credential Rotation*).

**TOTP 2FA:**

- RFC 6238 with a 30-second step.
- Secrets are AES-256-GCM encrypted at rest with per-row AAD binding (see *Field-Level Encryption*).
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
| `/api/*` (catch-all) | 300 requests / 1 minute | `RATE_LIMIT_API_MAX`, `RATE_LIMIT_API_WINDOW` |

Behind a trusted proxy (`TRUST_PROXY_ENABLED=true`), the per-IP key is the **rightmost untrusted `X-Forwarded-For` hop** relative to `TRUSTED_PROXIES` (`cmd/ovumcy/main.go` `rateLimitKeyGenerator`), not fiber's default leftmost `c.IP()`, so a client-spoofed XFF prefix cannot rotate the key and defeat the limit.

Plus per-account, identity-keyed budgets enforced by `AuthAttemptPolicy` (`internal/services/auth_attempt_policy.go`):

- Login attempts: 8 failures / 15 minutes. The OIDC link-confirmation password challenge (`POST /auth/oidc/link-confirm`) draws from this same budget, so link-confirm cannot be used as a faster password oracle than the login form.
- Logout attempts: 20 failures / 15 minutes (account-scoped).
- TOTP login challenge: 5 failures / 15 minutes.
- TOTP disable: 5 failures / 15 minutes.

Per-account budgets are keyed by `HMAC-SHA256(SECRET_KEY, "ovumcy.auth-attempt.identity.v1:" || identity)`, so the limiter never persists the raw identifier.
