# Cryptography: Field Encryption, Cookies & SECRET_KEY

_Part of the [Ovumcy security policy](../../SECURITY.md)._

## Field-Level Encryption

Sensitive per-account fields written through `security.EncryptField` (currently `users.totp_secret`) are encrypted with AES-256-GCM under a key derived from `SECRET_KEY` via HKDF-SHA256, and bound to the owner's user id through the AEAD authentication tag (additional-authenticated-data, `ovumcy.field.<purpose>:<row id>`). An attacker who can write directly to the database — for example via a hypothetical SQL injection or via host-level compromise — cannot move a ciphertext from one account into another account's row and have it open: the authentication tag fails to verify under a different aad. The `SECRET_KEY` itself is required for both encrypt and decrypt; database access without the key leaks nothing about the persisted value.

A legacy decrypt path exists for ciphertexts written by Ovumcy versions before aad binding was introduced. The new code transparently re-encrypts those values under the aad-bound format on the next successful 2FA login. Operators do not need to take any explicit migration step; the upgrade happens lazily and without revoking the user's current session.

### Login: `requires_totp` reveals 2FA status

`POST /api/v1/sessions` returns `{"requires_totp": true}` when the supplied password is correct and the account has TOTP enabled, and `{"ok": true}` plus a session cookie otherwise. A credential-dump attacker can use this to triage accounts by whether they have a second factor.

This is inherent to any password-then-TOTP flow that gates the second factor on account state — any uniform response that hides the difference either silently grants access without verifying TOTP (downgrade attack) or unconditionally rejects accounts without TOTP (lockout). The per-account rate limiter (`AuthAttemptPolicy`) and the recovery-code re-auth requirements bound the value of knowing the 2FA status.

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
- `ovumcy_csrf` is managed by Fiber's CSRF middleware. The OIDC callback path is exempt because the identity provider returns to it directly (a form_post body, or a query redirect under `OIDC_RESPONSE_MODE=query`) and cannot supply our token; provider-issued `state`/`nonce` cover replay protection for that endpoint instead. In query mode the callback is also served over `GET`, a safe method the middleware never validates, protected by the same sealed one-time state cookie.

## SECRET_KEY Usage Map

`SECRET_KEY` (or the file behind `SECRET_KEY_FILE`) is the single application-wide secret. It is used as the input keying material for several distinct subsystems:

| Subsystem | Derivation | Effect of rotation |
| --- | --- | --- |
| Sealed cookies (every `Sealed=yes` cookie above) | `HKDF-SHA256(SECRET_KEY, salt="ovumcy.secure-cookie.salt.v2", info="ovumcy.secure-cookie.key.v2")` → AES-256-GCM, with cookie name as AAD | Existing cookies fail to open and are silently re-issued or rejected. Users see a fresh sign-in. |
| Field encryption (`users.totp_secret`) | `HKDF-SHA256(SECRET_KEY, salt="ovumcy.field-crypto.salt.v1", info="ovumcy.field-crypto.key.v1")` → AES-256-GCM, with `ovumcy.field.<purpose>:<row id>` as AAD | **Existing TOTP ciphertexts no longer decrypt.** All 2FA-enabled accounts will fail their next TOTP challenge — see the caveat below. |
| Field encryption (`users.webhook_url`) | Same field-crypto derivation and AAD scheme (`ovumcy.field.webhook_url:<user id>`) | **Existing webhook URL ciphertexts no longer decrypt.** The request-free notify pass treats an un-openable `webhook_url` as "no deliverable endpoint" and **skips that owner** — it fails safe: no reminder is ever delivered to a garbage target. The owner re-saves the URL under the new key to restore delivery. No `auth_session_version` bump occurs on a webhook settings change. |
| Rate-limit identity HMAC | `HMAC-SHA256(SECRET_KEY, "ovumcy.auth-attempt.identity.v1:" || identity)` | Existing per-identity counters become unreachable; cooldowns reset. Self-recovering. |
| Auth session token signing | Used inside `BuildAuthSessionTokenWithSessionID` to sign session tokens before they are sealed into `ovumcy_auth` | Subsumed by the sealed-cookie invalidation row above. |

Both HKDF label pairs live next to the shared AEAD primitive (`SealedCipher` in `internal/security/sealed_cipher.go`). The sealed-cookie codec in `internal/api/secure_cookie_codec.go` adds only cookie framing on top: the name→AAD mapping, the `v2` version envelope, and base64url transport.

**Operational caveat — rotating `SECRET_KEY` with 2FA accounts:**

`users.totp_secret` is encrypted under a key derived from the current `SECRET_KEY`. Rotating the secret without a coordinated re-encryption step leaves TOTP-enabled accounts unable to complete the 2FA challenge — they will see a failed verification even when their authenticator app produces the correct code. Recovery options for each affected user are:

1. Sign in with the recovery code, then re-enrol TOTP under the new key.
2. Ask the operator to run `ovumcy reset-password <email>` and disable 2FA via Settings once signed back in.

Plan secret rotation as planned maintenance, communicate it in advance, and consider asking users to disable 2FA before the rotation window.

**If the secret is lost entirely (not rotated, gone with no backup):** this is distinct from a planned rotation because there is no old key to coordinate a migration from. Every `users.totp_secret` ciphertext becomes permanently unrecoverable — HKDF derivation means the encryption key only ever existed as a function of `SECRET_KEY`, and there is no escrow. Every sealed cookie and session invalidates, same as a rotation. Affected users follow the same recovery options above (recovery code + re-enrolment, or operator reset), except a user with `local_auth_enabled=false` and no retained recovery code has no self-service option and requires an operator-run `ovumcy reset-password <email>`. Total secret loss should be treated as a data-loss incident, not routine key rotation.
