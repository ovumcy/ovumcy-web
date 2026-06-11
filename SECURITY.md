# Security Policy

## Supported Versions

Security fixes are provided for the `main` branch only.

| Version | Supported |
| --- | --- |
| `main` | :white_check_mark: |
| older commits/tags | :x: |

## Verifying Release Authenticity

Published container images are keyless-signed with [cosign](https://github.com/sigstore/cosign)
and carry a [SLSA build provenance](https://slsa.dev) attestation and a software bill of
materials, all produced by this repository's GitHub Actions and pushed to the registry. Before
running an image you can confirm it was built by this repository's CI from this source, rather
than substituted or tampered with.

Verify the signature (replace `v1.1.0` with the tag you are pulling):

```bash
cosign verify ghcr.io/ovumcy/ovumcy-web:v1.1.0 \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github.com/ovumcy/ovumcy-web/\.github/workflows/docker-image\.yml@'
```

Verify the build provenance with the GitHub CLI:

```bash
gh attestation verify oci://ghcr.io/ovumcy/ovumcy-web:v1.1.0 --owner ovumcy
```

A failed check means the image was not produced by this repository's release workflow. Do not run it.

## Reporting a Vulnerability

Please report security vulnerabilities privately — not through public GitHub
issues. Use either channel:

- GitHub private vulnerability reporting (preferred):
  https://github.com/ovumcy/ovumcy-web/security/advisories/new
- Email: contact@ovumcy.com (subject: `SECURITY: <short summary>`)

Include impact, reproduction steps, affected endpoints/files, and a suggested
fix if you have one.

We practice coordinated vulnerability disclosure. After a report we acknowledge
receipt within 72 hours, share a triage assessment and remediation plan within
7 days, and aim to ship a fix and coordinate public disclosure within 90 days —
sooner for actively exploited issues. Please give us reasonable time to
remediate before disclosing a vulnerability publicly.

## Known Information Disclosure

A few information-disclosure signals are accepted residual risk because closing them either breaks core flows or requires infrastructure Ovumcy does not assume self-hosters have (for example SMTP).

### Register enumeration: pickup-cookie follow-up oracle (residual)

`POST /api/v1/users` returns identical status, body, and Set-Cookie shape (a single sealed `ovumcy_register_pickup` cookie of fixed length, no `ovumcy_auth` or `ovumcy_recovery_code`) for both a brand-new email and a duplicate. Response timing is also equalized: the duplicate-email branch runs `equalizeRegistrationTiming` (two bcrypt-cost-10 comparisons against fixed placeholder hashes) to match the work `BuildOwnerUserWithRecovery` performs on the new-email branch (password hash + recovery-code hash), so the single POST response carries no observable difference. The follow-up `GET /register/welcome` then dispatches the pickup cookie either to `/register` (with auth and recovery cookies) for a valid pickup or to `/login` (with a neutral flash) for a decoy or expired pickup. An attacker who holds their own pickup cookie from a probe POST can therefore observe which redirect target their cookie resolves to and infer whether the email was new or already registered. This turns the single-request status / cookie / timing oracle into a two-request probe, with both endpoints under per-IP rate limiting, and the login bcrypt-timing equalization (`equalizeAuthCredentialsTiming`) further bounding any cross-endpoint follow-up.

Closing the residual signal entirely is mathematically impossible without an out-of-band verification channel: any in-app dispatch on the pickup cookie reveals the branch, and any login-after-register variant turns the probe into a follow-up `POST /api/v1/sessions` whose success or failure carries the same information. The only options are (a) a magic-link / email-driven enrollment that gates registration behind SMTP delivery (not assumed in the self-hosted deployment model), or (b) acceptance of the documented two-request probe. Both are revisited if Ovumcy ever ships a multi-tenant SaaS variant.

A separate vector — replay of a sealed `ovumcy_register_pickup` cookie captured from somebody else's response within the 5-minute TTL — is **not** part of this residual. The pickup cookie carries an opaque nonce that is consumed atomically through `register_pickup_tokens` on the first `GET /register/welcome` call; a captured cookie reaching the welcome endpoint a second time gets the same neutral `/login` redirect as a decoy or expired pickup, and cannot mint a second `ovumcy_auth` session.

## OIDC Account Linking

Ovumcy does not trust an upstream OIDC provider to vouch for *which existing local account* a verified email belongs to. The trust given to a configured IdP is "this user controls subject S at issuer I"; **not** "this user controls every Ovumcy account that ever registered with that email".

Concretely: when an OIDC callback returns a (issuer, subject) pair that is not yet linked to any Ovumcy user, the service layer (`internal/services/oidc_login_service.go`) takes one of two paths:

1. **No existing local user with this email** → auto-provision (if `OIDC_AUTO_PROVISION=true` and the email falls under `OIDC_AUTO_PROVISION_ALLOWED_DOMAINS`) and link the new identity inline. No prior owner exists, so no account can be taken over.
2. **A local-auth account already exists for this email** → the service returns `ErrOIDCLinkRequiresConfirmation` with the pending claims. The callback handler stores them in the sealed `ovumcy_oidc_link_pending` cookie (5-minute payload-bound TTL, path-scoped to `/auth/oidc/link-confirm`) and redirects the user to a password-confirmation page. Only after the holder of the existing account submits the correct local password does `ConfirmAndLinkIdentity` persist the link and issue a session.

This defends against the malicious / sloppy upstream IdP scenario: a provider that lets any registrant claim any email (a common default posture in self-hosted OIDC servers like Pocket ID or Authelia under their out-of-the-box configurations) cannot, by asserting `email_verified=true` for somebody else's address, take over the corresponding Ovumcy account.

The confirmation step is refused if the existing account has no local password (`local_auth_enabled=false`). Multi-provider linking onto such accounts is intentionally out of scope for the unauthenticated login path — that has to happen through a future authenticated Settings flow, which is not yet shipped.

When the target account has TOTP enabled, the link-confirmation form additionally requires a valid 6-digit code submitted alongside the password. The handler invokes the same `TOTPService.ValidateCode` path as `/api/v1/sessions/2fa-challenge`, including replay rejection (`ErrTOTPReplayed`) and the per-`(client_ip, user_id)` failure counter. Without this gate, an attacker who has only the victim's password — and uses a malicious or sloppy upstream IdP to assert their email — could obtain a session for a 2FA-protected account without ever holding the second factor, and the linked identity would persist for future OIDC sign-ins.

## Field-Level Encryption

Sensitive per-account fields written through `security.EncryptField` (currently `users.totp_secret`) are encrypted with AES-256-GCM under a key derived from `SECRET_KEY` via HKDF-SHA256, and bound to the owner's user id through the AEAD authentication tag (additional-authenticated-data, `ovumcy.field.<purpose>:<row id>`). An attacker who can write directly to the database — for example via a hypothetical SQL injection or via host-level compromise — cannot move a ciphertext from one account into another account's row and have it open: the authentication tag fails to verify under a different aad. The `SECRET_KEY` itself is required for both encrypt and decrypt; database access without the key leaks nothing about the persisted value.

A legacy decrypt path exists for ciphertexts written by Ovumcy versions before aad binding was introduced. The new code transparently re-encrypts those values under the aad-bound format on the next successful 2FA login. Operators do not need to take any explicit migration step; the upgrade happens lazily and without revoking the user's current session.

### Login: `requires_totp` reveals 2FA status

`POST /api/v1/sessions` returns `{"requires_totp": true}` when the supplied password is correct and the account has TOTP enabled, and `{"ok": true}` plus a session cookie otherwise. A credential-dump attacker can use this to triage accounts by whether they have a second factor.

This is inherent to any password-then-TOTP flow that gates the second factor on account state — any uniform response that hides the difference either silently grants access without verifying TOTP (downgrade attack) or unconditionally rejects accounts without TOTP (lockout). The per-account rate limiter (`AuthAttemptPolicy`) and the recovery-code re-auth requirements bound the value of knowing the 2FA status.

## Session Invalidation on Credential Rotation

Operations that rotate a long-lived credential bump `users.auth_session_version` in the same database update, immediately invalidating every active `ovumcy_auth` cookie for that account. This applies to:

- Password change (`PUT /api/v1/users/current/password`).
- Password reset via recovery code (`POST /api/v1/password-resets/redeem`).
- Recovery-code regeneration (`POST /api/v1/users/current/recovery-code`) — the current request receives a freshly issued cookie so the originating session stays alive, but every other device is signed out.
- Forced password reset via the `ovumcy reset-password` operator command.
- TOTP 2FA enable (`PUT /api/v1/users/current/2fa`) and disable (`DELETE /api/v1/users/current/2fa`) — toggling the second factor is also a change to the account's auth posture, so any cookie issued before the toggle is invalidated. The originating device receives a freshly issued cookie inline; every other device is signed out.

If you suspect a session compromise, regenerating the recovery code is the fastest way to force every other device to re-authenticate without changing your password.

## Cookies

Ovumcy uses only first-party cookies. Cookies marked **Sealed** are encrypted with AES-256-GCM under a key derived from `SECRET_KEY` via HKDF-SHA256 and bound to the cookie name through the AEAD authentication tag, so a value from one cookie cannot be reused as another. `Secure` defaults to the operator's `COOKIE_SECURE` setting unless noted otherwise.

| Name | Purpose | TTL | Path | HttpOnly | SameSite | Secure | Sealed |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `ovumcy_auth` | Session token carrying user id, role, and `auth_session_version` | 30 days (persistent) with remember-me, otherwise a browser-session cookie (underlying token TTL 7 days) | `/` | yes | Lax | `COOKIE_SECURE` | yes |
| `ovumcy_csrf` | CSRF double-submit token matched against the `csrf_token` form field on every state-changing request | browser session | `/` | yes | Lax | `COOKIE_SECURE` | no |
| `ovumcy_lang` | User-selected UI language code | 1 year | `/` | no (JS-readable for i18n) | Lax | `COOKIE_SECURE` | no |
| `ovumcy_tz` | Client IANA timezone for server-side calendar math | 1 year | `/` | no (JS-writable, server-validated) | Lax | `COOKIE_SECURE` | no |
| `ovumcy_flash` | One-shot UI message between redirects | 5 minutes | `/` | yes | Lax | `COOKIE_SECURE` | yes |
| `ovumcy_recovery_code` | Carries the freshly issued recovery code to `/recovery-code` | 20 minutes | `/` | yes | Lax | `COOKIE_SECURE` | yes |
| `ovumcy_register_pickup` | Single-use opaque nonce + decoy recovery code for post-register pickup; consumed atomically through `register_pickup_tokens` | 5 minutes | `/` | yes | Lax | `COOKIE_SECURE` | yes |
| `ovumcy_reset_password` | Carries a password-reset token between the recovery-code page and `/reset-password` | 30 minutes | `/` | yes | Lax | `COOKIE_SECURE` | yes |
| `ovumcy_oidc_auth` | OIDC `state`, `nonce`, PKCE verifier during sign-in | 10 minutes | `/auth/oidc/callback` | yes | None | forced `true` | yes |
| `ovumcy_oidc_stepup` | OIDC step-up state (purpose, pending password hash) during local-password setup re-auth | 10 minutes | `/auth/oidc/callback` | yes | None | forced `true` | yes |
| `ovumcy_oidc_logout_bridge` | Carries the session id from `/logout` to the provider end-session bridge | 1 minute | `/auth/oidc/logout` | yes | Lax | `COOKIE_SECURE` | yes |
| `ovumcy_oidc_link_pending` | Holds the (issuer, subject, target user id) of an OIDC identity awaiting password confirmation before being linked to a pre-existing local account; see *OIDC Account Linking* below | 5 minutes (payload-bound) | `/auth/oidc/link-confirm` | yes | Lax | `COOKIE_SECURE` | yes |
| `ovumcy_totp_pending` | 2FA challenge state (user id, remember-me) between password and TOTP submission | 5 minutes (payload-bound) | `/` | yes | Lax | `COOKIE_SECURE` | yes |
| `ovumcy_totp_setup` | Raw TOTP secret transport during enrollment, before the user has confirmed their first code | 5 minutes (payload-bound) | `/` | yes | Lax | `COOKIE_SECURE` | yes |

Notes:

- The two OIDC sign-in cookies require `Secure=true` unconditionally because their `SameSite=None` value is only legal over HTTPS; the OIDC handler refuses to issue them when `COOKIE_SECURE=false`.
- `ovumcy_lang` and `ovumcy_tz` are deliberately non-`HttpOnly` so the browser can write the user's timezone preference and reflect language without a server round trip. They contain no secrets and are validated server-side before use.
- `ovumcy_csrf` is managed by Fiber's CSRF middleware. The OIDC callback path is exempt because it is form-posted from the identity provider, which cannot supply our token; provider-issued `state`/`nonce` cover replay protection for that endpoint instead.

## Data Inventory

What Ovumcy persists per account and per record. All storage is in the operator's configured SQLite file or Postgres database; nothing is sent to any external service.

**`users`** — one row per account:

- Identity: `id`, `email` (unique), `display_name`, `role` (default `owner`), `created_at`.
- Credentials: `password_hash` (bcrypt cost 10), `recovery_code_hash` (bcrypt cost 10), `local_auth_enabled`, `auth_session_version`, `must_change_password`.
- Onboarding: `onboarding_completed`.
- Cycle preferences: `cycle_length`, `period_length`, `luteal_phase`, `auto_period_fill`, `irregular_cycle`, `unpredictable_cycle`, `age_group`, `usage_goal`, `last_period_start`, `long_period_warning_cycle_start`.
- Tracking preferences: `track_bbt`, `temperature_unit`, `track_cervical_mucus`, `hide_sex_chip`, `hide_cycle_factors`, `hide_notes_field`, `show_historical_phases`, `shown_period_tip`.
- 2FA: `totp_enabled`, `totp_secret` (AES-256-GCM aad-bound under an HKDF-derived key, see *Field-Level Encryption*), `totp_last_used_step` (RFC 6238 replay floor).

**`daily_logs`** — one row per (user, calendar day). Dates are stored as UTC midnight and rendered in the user's timezone at read time.

- Period: `is_period`, `cycle_start`, `is_uncertain`, `flow` (`none|spotting|light|medium|heavy`).
- Wellbeing: `mood` (signed scale), `sex_activity` (`none|protected|unprotected`), `bbt` (float, unit selected per account), `cervical_mucus` (`none|dry|moist|creamy|eggwhite`), `pregnancy_test` (`none|negative|positive`).
- `cycle_factor_keys` (JSON list), `symptom_ids` (JSON list, references owner-managed symptoms), `notes` (free text).
- The string value domains listed above (`flow`, `sex_activity`, `cervical_mucus`, `pregnancy_test`) are normalized and validated in `internal/services`, not by DB `CHECK` constraints — the columns are plain `TEXT`. (An early `flow` `CHECK` omitted `spotting` and was dropped in migration 003.)

**`symptom_types`** — owner-managed symptom catalog with archive support: `name`, `icon`, `color`, `is_builtin`, soft-archive flag.

**`oidc_identities`** — populated only when OIDC is enabled. `(issuer, subject) → user_id` link plus `created_at`, `last_used_at`. Rows carry `FOREIGN KEY ... ON DELETE CASCADE` on `users.id`.

**`oidc_logout_states`** — short-lived per-session OIDC end-session metadata (`end_session_endpoint`, `id_token_hint`, `post_logout_redirect_url`). Keyed by `session_id` with `expires_at`. Rows are TTL-bounded, not user-id-bounded.

**`register_pickup_tokens`** — opaque single-use nonces for the post-register pickup flow. 5-minute TTL. Rows are not foreign-keyed; they expire and become unreachable on their own.

**Not stored**: analytics, telemetry, third-party identifiers, advertising attribution, error reports, or per-action audit history. Per-action security-event logging is **off by default** and can be toggled per deployment via `AUDIT_LOG_ENABLED` — see *Logging Policy* below.

## Retention and Deletion

**`POST /api/v1/users/current/data-wipe`** (`Settings → Clear data`) wipes per-account health data while keeping the account active:

- Deletes every `daily_logs` row for the user.
- Deletes every user-defined row in `symptom_types` (built-in symptoms remain).
- Resets cycle and tracking preferences to documented defaults.
- Atomically bumps `auth_session_version`, invalidating every other auth cookie for the account. The originating device is re-issued a fresh cookie inline so the user stays signed in there.

`clear-data` does **not** touch email, password hash, recovery code hash, role, display name, OIDC identity links, TOTP state, or onboarding status.

**`DELETE /api/v1/users/current`** removes the account entirely:

- Deletes every `daily_logs` row for the user.
- Deletes every `symptom_types` row for the user (including built-ins).
- Deletes every `oidc_identities` and `register_pickup_tokens` row for the user explicitly, then deletes the `users` row itself. `oidc_identities` also carries `ON DELETE CASCADE`, but the deletion is performed explicitly so erasure stays complete even if foreign-key enforcement is ever disabled, and so `register_pickup_tokens` (which has no foreign key) is removed rather than left to expire.

The `oidc_logout_states` table is not joined to `users.id`; it carries only a short-lived provider-driven logout reference that becomes unreachable and is pruned on its own TTL.

Both operations require the current password through `validateSettingsActionPassword`. OIDC-only accounts must enrol a local password through the step-up re-auth flow before either danger-zone action becomes available.

## Password & Auth Policy

**Local passwords:**

- Minimum length: 8 Unicode code points.
- Required character classes: at least one uppercase letter, one lowercase letter, and one digit (`ValidatePasswordStrength` in `internal/services/password_policy.go`).
- Storage: bcrypt with `bcrypt.DefaultCost` (currently cost 10) via `golang.org/x/crypto/bcrypt`. Hashes live in `users.password_hash`.

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

- Login attempts: 8 failures / 15 minutes.
- Logout attempts: 20 failures / 15 minutes (account-scoped).
- TOTP login challenge: 5 failures / 15 minutes.
- TOTP disable: 5 failures / 15 minutes.

Per-account budgets are keyed by `HMAC-SHA256(SECRET_KEY, "ovumcy.auth-attempt.identity.v1:" || identity)`, so the limiter never persists the raw identifier.

## SECRET_KEY Usage Map

`SECRET_KEY` (or the file behind `SECRET_KEY_FILE`) is the single application-wide secret. It is used as the input keying material for several distinct subsystems:

| Subsystem | Derivation | Effect of rotation |
| --- | --- | --- |
| Sealed cookies (every `Sealed=yes` cookie above) | `HKDF-SHA256(SECRET_KEY, salt="ovumcy.secure-cookie.salt.v2", info="ovumcy.secure-cookie.key.v2")` → AES-256-GCM, with cookie name as AAD | Existing cookies fail to open and are silently re-issued or rejected. Users see a fresh sign-in. |
| Field encryption (`users.totp_secret`) | `HKDF-SHA256(SECRET_KEY, salt="ovumcy.field-crypto.salt.v1", info="ovumcy.field-crypto.key.v1")` → AES-256-GCM, with `ovumcy.field.<purpose>:<row id>` as AAD | **Existing TOTP ciphertexts no longer decrypt.** All 2FA-enabled accounts will fail their next TOTP challenge — see the caveat below. |
| Rate-limit identity HMAC | `HMAC-SHA256(SECRET_KEY, "ovumcy.auth-attempt.identity.v1:" || identity)` | Existing per-identity counters become unreachable; cooldowns reset. Self-recovering. |
| Auth session token signing | Used inside `BuildAuthSessionTokenWithSessionID` to sign session tokens before they are sealed into `ovumcy_auth` | Subsumed by the sealed-cookie invalidation row above. |

Both HKDF label pairs live next to the shared AEAD primitive (`SealedCipher` in `internal/security/sealed_cipher.go`). The sealed-cookie codec in `internal/api/secure_cookie_codec.go` adds only cookie framing on top: the name→AAD mapping, the `v2` version envelope, and base64url transport.

**Operational caveat — rotating `SECRET_KEY` with 2FA accounts:**

`users.totp_secret` is encrypted under a key derived from the current `SECRET_KEY`. Rotating the secret without a coordinated re-encryption step leaves TOTP-enabled accounts unable to complete the 2FA challenge — they will see a failed verification even when their authenticator app produces the correct code. Recovery options for each affected user are:

1. Sign in with the recovery code, then re-enrol TOTP under the new key.
2. Ask the operator to run `ovumcy reset-password <email>` and disable 2FA via Settings once signed back in.

Plan secret rotation as planned maintenance, communicate it in advance, and consider asking users to disable 2FA before the rotation window.

## Threat Model

**In scope** — Ovumcy actively defends against:

- Credential stuffing and bot enumeration against `/api/v1/sessions`, `/api/v1/users`, and `/api/v1/password-resets` (rate limits, sealed pickup, bcrypt-timing equalization).
- Replay of captured sealed cookies (server-side single-use for register pickup; session-version checks for `ovumcy_auth`).
- DOM-XSS into HTMX error responses (status-error fragment is parsed with `DOMParser` and rebuilt via `document.createElement` + `textContent`, never `innerHTML`).
- Cross-site form submission (CSRF middleware on every state-changing endpoint; OIDC callback uses provider-issued `state`/`nonce` instead).
- Algorithm-confusion attacks against ID tokens (asymmetric-algorithm allowlist; `HS*` and `none` are rejected even if the provider advertises them).
- Malicious OIDC discovery metadata redirecting the logout flow to attacker-controlled hosts (`end_session_endpoint` host-pinned to the configured issuer).
- Malicious OIDC discovery metadata redirecting the JWKS key fetch to attacker-controlled hosts (`jwks_uri` origin-pinned to the configured issuer, so token-signature keys are only fetched same-origin).
- Cross-account ciphertext substitution at the database layer (field encryption is AAD-bound to the row id).
- Trivial password reuse (8-character minimum with three required character classes).
- Account takeover via a malicious or sloppy upstream OIDC IdP asserting a verified email already held by a local-auth Ovumcy account — first-time linking is refused without an explicit password confirmation step (`ovumcy_oidc_link_pending` + `/auth/oidc/link-confirm`), and when the target has TOTP enabled the same submission additionally requires a valid 6-digit code. See *OIDC Account Linking*.

**Out of scope** — Ovumcy assumes the operator is trusted, and does not defend against:

- An operator who reads the SQLite file directly, captures `SECRET_KEY`, or inspects process memory. The deployment model is single-tenant self-hosting; in the typical case the operator is the same person as the user.
- Compromise of the user's endpoint (browser malware, OS keyloggers, shoulder surfing).
- TLS downgrade attacks against an operator who runs the app over plain HTTP and ignores the `COOKIE_SECURE` advisory.
- Side-channel attacks against the host CPU (Spectre-class, cache-timing); Ovumcy uses constant-time comparisons where it matters but cannot defend the underlying hardware.
- Simultaneous compromise of `SECRET_KEY` and the OIDC provider's signing key.

Multi-tenant SaaS deployment, organizational identity management, hardware-key support, and audit-log compliance are not goals of this codebase. They are explicitly out of scope for both threat modeling and feature roadmap.

## Logging Policy

Ovumcy does **not** emit per-action audit logs by default. The `AUDIT_LOG_ENABLED` environment variable controls the audit-event stream:

- `AUDIT_LOG_ENABLED=false` (default) — the runtime emits no `security event:` lines. Go panics, startup configuration errors, and the Fiber request log remain enabled.
- `AUDIT_LOG_ENABLED=true` — the runtime emits per-action security-event lines to stderr via the Go standard `log` package. Each line includes the action name, outcome, request method, **sanitized** request path (concrete date parameters are replaced with `:date` and other identifiers are similarly masked), response format, and — for authenticated requests — `user_id` and role. Example:

  ```
  security event: action="health.day_upsert" outcome="success" method="POST"
                  path="/api/v1/days/:date" format="json" user_id="42"
                  role="owner" domain="health_data" target="day_entry"
  ```

When enabled, these lines are visible to the operator through their container runtime (`docker compose logs`, journald, etc.) and never leave the host. They are intended for ad-hoc incident investigation — for example, to confirm whether a suspected compromise produced state-changing requests, and from which `user_id`. The audit stream is not designed as a compliance audit trail; nothing in Ovumcy itself ships, archives, or rotates these lines.

If you enable `AUDIT_LOG_ENABLED=true`, plan retention and access control around the persistent-identifier content (`user_id`, role). Treat the resulting log stream as the same sensitivity class as the database itself.

The Fiber request log (`time | status | latency | method | path`) is independent of `AUDIT_LOG_ENABLED` and remains enabled in all configurations. It does not include `user_id` or authenticated-session metadata.

The startup banner reflects the current setting (`audit_log=true|false`) so operators can confirm the effective configuration on each boot.

## GDPR Cross-Reference

Ovumcy is privacy-critical software handling special-category personal data (Art. 9). Operators of self-hosted instances act as data controllers; Ovumcy provides the technical controls and operators provide deployment-specific policy. The full operator-facing compliance walkthrough lives in [`docs/gdpr.md`](docs/gdpr.md). The table below maps each in-scope obligation onto the existing technical control and its enforcing test.

| GDPR Article | Obligation | Technical control | Enforcing test |
| --- | --- | --- | --- |
| Art. 5(1)(c) — Data minimisation | Only data necessary for the cycle-tracking purpose is stored | `users`, `daily_logs`, `symptom_types` schemas as described in *Data Inventory* above; no analytics, telemetry, or third-party identifiers | `internal/db/migrations.go` + schema in `migrations/` |
| Art. 5(1)(f) — Integrity & confidentiality | Auth/recovery/reset cookies sealed; TOTP secret encrypted at rest; CSRF on every mutation | AES-256-GCM + HKDF-derived keys + AAD binding | `internal/api/secure_cookie_codec_*_test.go`, `internal/security/field_crypto_test.go`, `internal/api/state_mutation_csrf_regression_test.go` |
| Art. 9(2)(a) — Explicit consent | Self-hosted single-tenant deployments rely on operator-captured consent; the codebase exposes no third-party transmission | `/privacy` page renders the in-app privacy notice; no external network calls | `internal/api/privacy_route_regressions_test.go`, `e2e/privacy.spec.ts` |
| Art. 13/14 — Information to subject | `/privacy` page enumerates storage location, scope of processing, and opt-out paths in five locales | `internal/templates/privacy.html` + `internal/i18n/locales/*.json` privacy keys | `internal/api/privacy_route_regressions_test.go`, `e2e/privacy.spec.ts` |
| Art. 15 — Right to access | Subjects export their full data set as CSV or JSON | `GET /api/v1/exports/csv`, `GET /api/v1/exports/json`, `GET /api/v1/exports/summary` | `internal/api/export_regressions_test.go` and siblings |
| Art. 16 — Right to rectification | Every owner field is editable from the dashboard, calendar, and settings UI | `PUT /api/v1/days/{date}`, `PATCH /api/v1/users/current/profile`, `PATCH /api/v1/symptoms/{id}` | `internal/api/day_upsert_canonicalization_regression_test.go`, `internal/api/settings_profile_persist_regressions_test.go`, `internal/api/settings_symptoms_*_regression_test.go` |
| Art. 17 — Right to erasure | `clear-data` removes health records; `delete-account` explicitly erases every user-scoped row (daily logs, symptoms, OIDC identities, register-pickup tokens) plus the account | `POST /api/v1/users/current/data-wipe`, `DELETE /api/v1/users/current` | `internal/api/settings_clear_data_flow_test.go`, `internal/api/settings_delete_account_regression_test.go`, `internal/db/delete_account_completeness_test.go`, `TestClearDataPreservesAccountIdentityFields` |
| Art. 20 — Right to portability | Same export endpoints return machine-readable formats; structure documented in `docs/export.md` | `GET /api/v1/exports/csv`, `GET /api/v1/exports/json` | `internal/api/export_regressions_test.go`, `internal/services/export_service_integration_test.go` |
| Art. 22 — Automated decision-making | Cycle predictions are statistical aggregations of the subject's own logs; no decision producing legal effects | Prediction policy in `internal/services/prediction_explanation.go`; copy explains conservative bounds | `internal/services/prediction_explanation_test.go`, `internal/services/calendar_view_service_test.go` |
| Art. 25 — Privacy by design / by default | Owner-only middleware on every mutation; auto-period-fill **off by default**; audit log **off by default** | `handler.OwnerOnly` chained explicitly on every `/api/v1/*` mutation; `AUDIT_LOG_ENABLED=false` startup default | `internal/api/owner_only_coverage_regression_test.go`, `internal/api/security_event_logging_audit_flag_test.go`, `cmd/ovumcy/main_test.go` (`TestLoadRuntimeConfigDefaultsAuditLogOff`) |
| Art. 30 — Records of processing | Per-action audit stream available under `AUDIT_LOG_ENABLED=true`; sanitized to drop PII | `internal/api/security_event_logging.go`, `SafeRequestLogPath` | `internal/api/security_event_logging_audit_flag_test.go`, `internal/api/security_event_logging_mutation_regression_test.go` |
| Art. 32 — Security of processing | Sealed cookies, encrypted TOTP secrets, owner-only access, rate limits, CSP, secure headers; operator adds disk-level encryption | Multiple — see *SECRET_KEY Usage Map*, *Cookies*, *Rate Limits* sections above; `cmd/ovumcy/main.go` security headers | `internal/api/secure_cookie_codec_*_test.go`, `internal/security/field_crypto_test.go`, `cmd/ovumcy/main_test.go` (security headers, rate-limit PII), `internal/api/owner_only_coverage_regression_test.go` |
| Art. 33 — Breach notification (72 h) | Disclosure channel published; operator-side runbook in `docs/gdpr.md` | [`SECURITY.md → Reporting a Vulnerability`](#reporting-a-vulnerability), [`docs/gdpr.md → Breach Notification`](docs/gdpr.md#breach-notification-art-33) | Policy-level — not test-enforceable |
| Art. 34 — Communication to subject | Operator obligation; Ovumcy provides per-account email addresses for outreach | Stored in `users.email` (read-only after registration via UI) | Not test-enforceable |
| Art. 35 — DPIA | Operator obligation; Ovumcy ships the technical inputs (data inventory, threat model, encryption posture) needed to build one | [`SECURITY.md → Data Inventory`](#data-inventory), [`SECURITY.md → Threat Model`](#threat-model), [`docs/gdpr.md`](docs/gdpr.md) | Policy-level — not test-enforceable |

Items marked *Policy-level* are operator obligations that cannot be verified by `go test`. Items marked with concrete test files are verified by the runtime test suite; their concrete claim mapping lives in [*Test Enforcement Matrix*](#test-enforcement-matrix) below.

## Test Enforcement Matrix

This section maps each test-enforceable claim above to the Go test that guards it. It is the mechanical check that the privacy and security claims in this document remain true. When a claim changes, the corresponding test must change too; when a test is removed, the claim is no longer enforced and must be retracted from this document.

Policy-level claims (threat model in/out-of-scope, design rationale, marketing-style statements like "privacy-first") are intentionally excluded — they are reviewed by humans, not by `go test`.

### Field-Level Encryption

| Claim | Enforced by |
| --- | --- |
| `users.totp_secret` is AES-256-GCM encrypted under a key derived from `SECRET_KEY` via HKDF-SHA256 | `TestEncryptDecryptField_RoundTrip` in [internal/security/field_crypto_test.go](internal/security/field_crypto_test.go) |
| AAD-bound to row id; cross-row substitution under a different AAD fails to open | `TestDecryptField_RejectsWrongAAD` in [internal/security/field_crypto_test.go](internal/security/field_crypto_test.go) |
| Ciphertext is non-deterministic (distinct outputs for the same plaintext) | `TestEncryptField_ProducesDistinctCiphertexts` in [internal/security/field_crypto_test.go](internal/security/field_crypto_test.go) |
| Wrong key fails to decrypt | `TestDecryptField_WrongKey` in [internal/security/field_crypto_test.go](internal/security/field_crypto_test.go) |
| Tampered ciphertext fails to decrypt | `TestDecryptField_TamperedCiphertext` in [internal/security/field_crypto_test.go](internal/security/field_crypto_test.go) |
| Legacy no-AAD ciphertexts open through the fallback path | `TestDecryptField_LegacyFallback` in [internal/security/field_crypto_test.go](internal/security/field_crypto_test.go) |
| Empty `SECRET_KEY` is refused | `TestEncryptField_EmptyKey`, `TestDecryptField_EmptyKey` in [internal/security/field_crypto_test.go](internal/security/field_crypto_test.go) |
| Golden pre-consolidation ciphertexts (AAD-bound and legacy no-AAD) still decrypt to the same plaintext | `TestDecryptFieldOpensPreConsolidationGoldenCiphertexts` in [internal/security/field_crypto_golden_test.go](internal/security/field_crypto_golden_test.go) |

### OIDC Account Linking

| Claim | Enforced by |
| --- | --- |
| First-time link to a pre-existing local-auth account is refused without explicit password confirmation | `TestOIDCCallbackPendingLinkSealsCookieAndRedirectsToConfirmPage` in [internal/api/auth_oidc_regressions_test.go](internal/api/auth_oidc_regressions_test.go) |
| Pending-link cookie is rejected on tamper / cross-purpose AAD / rotated key / payload expiry | `oidc_link_pending_cookie_test.go` in `internal/api/` |
| `/auth/oidc/link-confirm` POST is CSRF-protected and rejects requests without `csrf_token` | `TestCompleteOIDCLinkConfirmationRejectsRequestWithoutCSRFToken` in [internal/api/auth_oidc_regressions_test.go](internal/api/auth_oidc_regressions_test.go) |
| Wrong password keeps the pending-link cookie alive for retry within the 5-minute TTL | `TestCompleteOIDCLinkConfirmationKeepsCookieOnWrongPassword` in [internal/api/auth_oidc_regressions_test.go](internal/api/auth_oidc_regressions_test.go) |
| Accounts with `local_auth_enabled=false` (OIDC-only) are refused at the link-confirm path | `TestOIDCCallbackPendingLinkForOIDCOnlyUserRefusesWithoutCookie`, `TestCompleteOIDCLinkConfirmationWithLocalAuthDisabledRefusesUnavailable` in [internal/api/auth_oidc_regressions_test.go](internal/api/auth_oidc_regressions_test.go) |
| TOTP-enabled targets require a valid 6-digit code together with the password; missing / wrong / replayed codes refuse the link and do not issue a session | `TestCompleteOIDCLinkConfirmationWithTOTPEnabledRequiresValidCode`, `TestCompleteOIDCLinkConfirmationWithTOTPEnabledRefusesMissingCode`, `TestCompleteOIDCLinkConfirmationWithTOTPEnabledRefusesWrongCode` in [internal/api/auth_oidc_regressions_test.go](internal/api/auth_oidc_regressions_test.go) |
| Link-confirm form exposes the TOTP input only when the target account has TOTP enabled | `TestShowOIDCLinkConfirmPageRendersTOTPFieldForTOTPEnabledTarget`, `TestShowOIDCLinkConfirmPageHidesTOTPFieldForNonTOTPTarget` in [internal/api/auth_oidc_regressions_test.go](internal/api/auth_oidc_regressions_test.go) |
| `MustChangePassword` targets are routed to `/reset-password` instead of receiving an auth cookie | `TestCompleteOIDCLinkConfirmationRoutesMustChangePasswordToReset` in [internal/api/auth_oidc_regressions_test.go](internal/api/auth_oidc_regressions_test.go) |
| `ConfirmAndLinkIdentity` provider/storage failures clear the pending cookie | `TestCompleteOIDCLinkConfirmationConfirmLinkErrorMappingClearsCookie` in [internal/api/auth_oidc_regressions_test.go](internal/api/auth_oidc_regressions_test.go) |
| Successful link emits a sanitized `auth.oidc_link_confirm linked` security event | `TestCompleteOIDCLinkConfirmationEmitsAuditLogOnSuccess` in [internal/api/auth_oidc_regressions_test.go](internal/api/auth_oidc_regressions_test.go) |

### Register Enumeration Residual

| Claim | Enforced by |
| --- | --- |
| `POST /api/v1/users` emits an identical-shape sealed pickup cookie for new and duplicate emails | `auth_register_regressions_test.go`, `auth_register_email_persistence_regressions_test.go` in `internal/api/` |
| Duplicate-email branch runs equalized bcrypt timing | `TestAuthenticateCredentialsEqualizesTimingForMissingUser`, `TestAuthenticateCredentialsEqualizesTimingForDisabledLocalAuth` in [internal/services/auth_service_credentials_timing_test.go](internal/services/auth_service_credentials_timing_test.go) |
| Pickup nonce is single-use via `register_pickup_tokens` (atomic UPDATE) | `register_pickup_handler_test.go` in `internal/api/` |
| `GET /register/welcome` second consumption falls through to `/login` | `register_pickup_handler_test.go` in `internal/api/` |
| Recovery code shape `OVUM-XXXX-XXXX-XXXX` | `TestValidateRecoveryCodeFormat`, `TestNormalizeRecoveryCode` in [internal/services/auth_input_policy_test.go](internal/services/auth_input_policy_test.go) |

### Session Invalidation on Credential Rotation

| Claim | Enforced by |
| --- | --- |
| Password change bumps `auth_session_version`; other devices sign out | `auth_password_change_session_regression_test.go` in `internal/api/` |
| Password reset via recovery code bumps `auth_session_version` | `TestAuthServiceResetPasswordAndRotateRecoveryCode` in [internal/services/auth_service_recovery_test.go](internal/services/auth_service_recovery_test.go) |
| Recovery-code regeneration bumps `auth_session_version`; originating session refreshed inline | `TestAuthServiceRegenerateRecoveryCode` in [internal/services/auth_service_recovery_test.go](internal/services/auth_service_recovery_test.go) |
| Forced `ovumcy reset-password` CLI bumps `auth_session_version` | `TestAuthServiceForceResetPasswordByEmail` in [internal/services/auth_service_recovery_test.go](internal/services/auth_service_recovery_test.go), `internal/cli/reset_test.go` |
| TOTP enable bumps `auth_session_version` and refreshes originating session | `handlers_settings_2fa_session_revocation_test.go` in `internal/api/` |
| TOTP disable bumps `auth_session_version` and refreshes originating session | `handlers_settings_2fa_session_revocation_test.go` in `internal/api/` |
| `clear-data` bumps `auth_session_version` atomically with the wipe | `settings_clear_data_session_revocation_test.go` in `internal/api/` |
| `ovumcy_auth` cookie verifies against current `auth_session_version`; revoked sessions are rejected | `TestAuthMiddlewareRejectsRevokedAuthSessionCookieForAPI`, `TestAuthMiddlewareRejectsRevokedAuthSessionCookieAfterForcedResetForHTML` in [internal/api/auth_cookie_compat_regression_test.go](internal/api/auth_cookie_compat_regression_test.go) |

### Cookies

| Claim | Enforced by |
| --- | --- |
| Sealed cookies use the AES-GCM `v2.<...>` envelope | `TestSecureCookieCodecSealsWithVersion2Prefix` in [internal/api/secure_cookie_codec_rotation_test.go](internal/api/secure_cookie_codec_rotation_test.go) |
| Legacy `v1` cookie payloads are rejected | `TestSecureCookieCodecRejectsLegacyV1Payload` in [internal/api/secure_cookie_codec_rotation_test.go](internal/api/secure_cookie_codec_rotation_test.go) |
| Sealed cookies are AAD-bound to the cookie name; cross-cookie substitution fails | `secure_cookie_codec_security_test.go` in `internal/api/` |
| Golden sealed values from the pre-consolidation codec still open for all 11 purposes | `TestSecureCookieCodecOpensPreConsolidationGoldenValues` in [internal/api/secure_cookie_codec_golden_test.go](internal/api/secure_cookie_codec_golden_test.go) |
| `ovumcy_auth`, `ovumcy_register_pickup`, `ovumcy_recovery_code` all set `SameSite=Lax` | `cookie_security_enabled_test.go`, `cookie_security_default_test.go` in `internal/api/` |
| OIDC sign-in cookies (`ovumcy_oidc_auth`, `ovumcy_oidc_stepup`) require `Secure=true` to issue | `oidc_state_cookie_test.go`, `oidc_stepup_cookie_test.go` in `internal/api/` |
| Login issues a sealed `ovumcy_auth` cookie (not a legacy JWT) | `TestLoginSetsSealedAuthCookieValue`, `TestAuthMiddlewareRejectsLegacyJWTAuthCookieFallback` in [internal/api/auth_cookie_compat_regression_test.go](internal/api/auth_cookie_compat_regression_test.go) |
| Remember-me toggles cookie persistence | `TestLoginRememberMeControlsCookiePersistence` in [internal/api/auth_login_remember_me_regressions_test.go](internal/api/auth_login_remember_me_regressions_test.go) |
| State-changing endpoints require a valid CSRF token | `state_mutation_csrf_regression_test.go`, `auth_logout_csrf_regression_test.go`, `settings_security_csrf_regression_test.go`, `export_csrf_regression_test.go`, `language_switch_csrf_regression_test.go` in `internal/api/` |

### Retention and Deletion

| Claim | Enforced by |
| --- | --- |
| `clear-data` deletes daily_logs and user-defined symptoms and resets preferences | `settings_clear_data_flow_test.go` in `internal/api/` |
| `clear-data` does **not** touch email, password hash, recovery code hash, role, display name, OIDC identity links, TOTP state, or onboarding status | `TestClearDataPreservesAccountIdentityFields` in [internal/api/settings_clear_data_preservation_test.go](internal/api/settings_clear_data_preservation_test.go) **(added in this matrix pass)** |
| `delete-account` deletes daily_logs, all symptoms, and the users row | `settings_delete_account_regression_test.go` in `internal/api/`, `TestOperatorUserService*` in [internal/services/operator_user_service_test.go](internal/services/operator_user_service_test.go) |
| `delete-account` cascades to `oidc_identities` via `ON DELETE CASCADE` FK | Schema-enforced; migration `migrations/014_oidc_identities.sql` |
| Both danger-zone actions require the current password | `settings_clear_data_flow_test.go`, `settings_delete_account_regression_test.go` in `internal/api/` |

### Password & Auth Policy

| Claim | Enforced by |
| --- | --- |
| Passwords require ≥ 8 Unicode code points | `TestValidatePasswordStrength_RejectsWeakPasswords` (`"Short1"` case) in [internal/services/password_policy_test.go](internal/services/password_policy_test.go) |
| Passwords require at least one uppercase, one lowercase, and one digit | `TestValidatePasswordStrength_RejectsWeakPasswords` (alllowercase / ALLUPPERCASE / NoDigitsHere cases) in [internal/services/password_policy_test.go](internal/services/password_policy_test.go) |
| Strong password passes | `TestValidatePasswordStrength_AcceptsStrongPassword` in [internal/services/password_policy_test.go](internal/services/password_policy_test.go) |
| Change-password rejects weak password and mismatch | `TestChangePasswordRejectsWeakNumericPassword`, `TestChangePasswordRejectsPasswordMismatch` in [internal/api/auth_change_password_validation_test.go](internal/api/auth_change_password_validation_test.go) |
| Recovery code is bcrypt-hashed at rest | `TestGenerateRecoveryCodeHash` in [internal/services/auth_reset_policy_test.go](internal/services/auth_reset_policy_test.go) |
| TOTP secret is encrypted under a key derived from `SECRET_KEY`; stored value not equal to plaintext | `internal/security/field_crypto_test.go` (see Field-Level Encryption above) |
| TOTP code reuse within the same 30-second step is rejected | `totp_service_test.go`, `user_repository_totp_step_test.go`, `handlers_auth_2fa_test.go` |
| TOTP enrollment rejects 6-digit codes that do not match | `handlers_settings_2fa_test.go` |
| Forgotten password reset rejects wrong recovery code | `TestAuthServiceFindUserByEmailAndRecoveryCodeRejectsMismatch`, `TestAuthServiceFindUserByEmailAndRecoveryCodeRejectsMissingUser` in [internal/services/auth_service_recovery_test.go](internal/services/auth_service_recovery_test.go) |
| Reset token rejects expired / wrong-purpose / state-mismatched inputs | `TestParsePasswordResetTokenRejectsExpired`, `TestParsePasswordResetTokenRejectsWrongPurpose`, `TestAuthServiceResolveUserByResetTokenRejectsStateMismatch` in [internal/services/auth_reset_policy_test.go](internal/services/auth_reset_policy_test.go), [internal/services/auth_service_recovery_test.go](internal/services/auth_service_recovery_test.go) |
| Auth session token rejects expired / wrong-signature / wrong-algorithm | `TestParseAuthSessionTokenRejectsExpired`, `TestParseAuthSessionTokenRejectsInvalidSignature`, `TestParseAuthSessionTokenRejectsWrongAlgorithm` in [internal/services/auth_session_policy_test.go](internal/services/auth_session_policy_test.go) |

### Rate Limits

| Claim | Enforced by |
| --- | --- |
| Per-account rate-limit keys are HMAC-derived from `SECRET_KEY` (no raw identity persisted) | `TestAuthAttemptPolicyKeysUseScopedHMACFingerprint`, `TestAuthAttemptPolicyKeysOmitIdentityFingerprintForBlankIdentity` in [internal/services/auth_attempt_policy_test.go](internal/services/auth_attempt_policy_test.go) |
| Attempt limiter respects window and reset semantics | `TestAttemptLimiterWindowAndReset`, `TestAttemptLimiterMultiKeyOperations` in [internal/services/attempt_limiter_test.go](internal/services/attempt_limiter_test.go) |
| Logout endpoint is rate-limited per account | `auth_logout_rate_limit_regression_test.go` in `internal/api/` |
| TOTP login rate-limited at 5 failures / 15 min | `TestVerifyTOTPLoginRateLimitsRepeatedAttempts` (or sibling) in `handlers_auth_2fa_test.go` |
| TOTP disable rate-limited at 5 failures / 15 min | `handlers_settings_2fa_test.go` |
| Rate-limit error response is sanitized and contains no PII | `TestAuthRateLimitHandlerLogsSecurityEventWithoutPII` in [cmd/ovumcy/main_test.go](cmd/ovumcy/main_test.go) |
| Edge rate limiters key on the real client IP (rightmost untrusted `X-Forwarded-For` hop); a spoofed prefix shares one bucket, and an untrusted peer / disabled proxy ignores the header | `TestRateLimitKeyGeneratorBucketing`, `TestRightmostUntrustedIP`, `TestTrustedProxyMatcher` in [cmd/ovumcy/rate_limit_keygen_test.go](cmd/ovumcy/rate_limit_keygen_test.go) |

### SECRET_KEY Usage Map

| Claim | Enforced by |
| --- | --- |
| Sealed cookies derive their key from `SECRET_KEY` via HKDF with versioned salt/info labels | `secure_cookie_codec_rotation_test.go`, `secure_cookie_codec_security_test.go` in `internal/api/` |
| Field encryption uses a distinct HKDF label set; AAD prevents cross-row swap | `TestDecryptField_RejectsWrongAAD` in [internal/security/field_crypto_test.go](internal/security/field_crypto_test.go) |
| Cookie and field label sets derive distinct keys; a payload sealed for one purpose cannot be opened as the other | `TestSealedCipherPurposeKeySeparation` in [internal/security/sealed_cipher_test.go](internal/security/sealed_cipher_test.go) |
| Rate-limit identity HMAC uses a distinct domain-separation label | `TestAuthAttemptPolicyKeysUseScopedHMACFingerprint` in [internal/services/auth_attempt_policy_test.go](internal/services/auth_attempt_policy_test.go) |
| Rotating `SECRET_KEY` breaks field-encrypted TOTP secrets (DecryptField with the new key fails) | `TestDecryptField_WrongKey` in [internal/security/field_crypto_test.go](internal/security/field_crypto_test.go) |

### Threat Model

| Claim | Enforced by |
| --- | --- |
| OIDC `end_session_endpoint` is host-pinned; cross-origin endpoint is dropped, logout falls back to local | `TestOIDC_RuntimePoC_HostPinRejectsCrossOriginEndSessionEndpoint`, `TestOIDC_RuntimePoC_HostPinAcceptsSameOriginEndSessionEndpoint` in [internal/security/oidc_runtime_poc_test.go](internal/security/oidc_runtime_poc_test.go) |
| OIDC `jwks_uri` is origin-pinned to the issuer; a cross-origin `jwks_uri` from discovery is rejected before any verifier is built | `TestOIDC_RuntimePoC_JWKSOriginPinRejectsCrossOrigin`, `TestOIDC_RuntimePoC_JWKSOriginPinAcceptsSameOrigin` in [internal/security/oidc_runtime_poc_test.go](internal/security/oidc_runtime_poc_test.go); `TestValidateDiscoveredJWKSURI` in [internal/security/oidc_test.go](internal/security/oidc_test.go) |
| OIDC ID-token signing-algorithm allowlist rejects symmetric algorithms and `none` | `TestOIDC_RuntimePoC_AlgorithmConfusionRejected`, `TestOIDC_RuntimePoC_AlgorithmNoneRejected` in [internal/security/oidc_runtime_poc_test.go](internal/security/oidc_runtime_poc_test.go) |
| OIDC step-up reauth requires a matching `(issuer, subject)` identity for the current user | `auth_oidc_v2_regressions_test.go`, `settings_oidc_local_password_setup_test.go` in `internal/api/` |
| OIDC first-time link to a pre-existing local-auth account is refused without password confirmation (returns `ErrOIDCLinkRequiresConfirmation`; no identity row is created in the bypass path) | `TestOIDCLoginServiceAuthenticateRequiresConfirmationOnFirstLinkToExistingEmail` in [internal/services/oidc_login_service_test.go](internal/services/oidc_login_service_test.go) |
| OIDC link confirmation persists the identity link after a successful password confirmation | `TestOIDCLoginServiceConfirmAndLinkIdentityPersistsLink` in [internal/services/oidc_login_service_test.go](internal/services/oidc_login_service_test.go) |
| OIDC link confirmation refuses if the `(issuer, subject)` was concurrently claimed by a different user | `TestOIDCLoginServiceConfirmAndLinkIdentityRefusesCrossUserClaim` in [internal/services/oidc_login_service_test.go](internal/services/oidc_login_service_test.go) |
| HTMX error fragments are DOM-built, not assigned via `innerHTML` (defense-in-depth XSS) | `web/src/js/__tests__/` JS unit suite (`npm run test:unit`) |
| All other threat-model entries (operator-as-adversary, endpoint compromise, side-channel) are **policy-level** — Ovumcy explicitly declares these out of scope | Not test-enforceable |

### Logging Policy

| Claim | Enforced by |
| --- | --- |
| `AUDIT_LOG_ENABLED=false` (default) emits no `security event:` lines | `TestAuditLogDefaultOffSuppressesSecurityEvents` in [internal/api/security_event_logging_audit_flag_test.go](internal/api/security_event_logging_audit_flag_test.go) |
| `AUDIT_LOG_ENABLED=true` emits security-event lines | `TestAuditLogEnabledRestoresSecurityEvents` in [internal/api/security_event_logging_audit_flag_test.go](internal/api/security_event_logging_audit_flag_test.go) |
| `AuditLogEnabled()` getter reflects `SetAuditLogEnabled()` writes | `TestAuditLogEnabledReporter` in [internal/api/security_event_logging_audit_flag_test.go](internal/api/security_event_logging_audit_flag_test.go) |
| When enabled, day-write logs the sanitized path `/api/v1/days/:date` (no concrete date) | `TestUpsertDayLogsSanitizedPathWithoutConcreteDate` in [internal/api/security_event_logging_mutation_regression_test.go](internal/api/security_event_logging_mutation_regression_test.go) |
| When enabled, symptom mutation log does not leak the user-supplied symptom name | `TestCreateSymptomLogsMutationWithoutLeakingUserInput` in [internal/api/security_event_logging_mutation_regression_test.go](internal/api/security_event_logging_mutation_regression_test.go) |
| CSRF middleware error path does not leak PII into the audit log | `TestCSRFMiddlewareErrorHandlerLogsSecurityEventWithoutPII` in [cmd/ovumcy/main_test.go](cmd/ovumcy/main_test.go) |
| Rate-limit handler does not leak PII into the audit log | `TestAuthRateLimitHandlerLogsSecurityEventWithoutPII` in [cmd/ovumcy/main_test.go](cmd/ovumcy/main_test.go) |
| The Fiber request log's error field is sanitized (emails → `:email`, opaque tokens → `:token`) | `TestSafeLogError` in [internal/api/request_logging_test.go](internal/api/request_logging_test.go) |
