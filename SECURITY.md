# Security Policy

## Supported Versions

Security fixes are provided for the `main` branch only.

| Version | Supported |
| --- | --- |
| `main` | :white_check_mark: |
| older commits/tags | :x: |

## Reporting a Vulnerability

Please report security issues privately.

- Email: `contact@ovumcy.com`
- Subject: `SECURITY: <short summary>`
- Include: impact, reproduction steps, affected endpoints/files, and suggested fix if available

We will acknowledge receipt within 72 hours and provide a remediation plan after triage.

Do not open public GitHub issues for unpatched security vulnerabilities.

## Known Information Disclosure

A few information-disclosure signals are accepted residual risk because closing them either breaks core flows or requires infrastructure Ovumcy does not assume self-hosters have (for example SMTP).

### Register enumeration: pickup-cookie follow-up oracle (residual)

`POST /api/auth/register` returns identical status, body, and Set-Cookie shape (a single sealed `ovumcy_register_pickup` cookie of fixed length, no `ovumcy_auth` or `ovumcy_recovery_code`) for both a brand-new email and a duplicate. The follow-up `GET /register/welcome` then dispatches the pickup cookie either to `/register` (with auth and recovery cookies) for a valid pickup or to `/login` (with a neutral flash) for a decoy or expired pickup. An attacker who holds their own pickup cookie from a probe POST can therefore observe which redirect target their cookie resolves to and infer whether the email was new or already registered. This turns the single-request status / cookie oracle into a two-request probe, with both endpoints under per-IP rate limiting, and the login bcrypt-timing equalization (`equalizeAuthCredentialsTiming`) further bounding any cross-endpoint follow-up.

Closing the residual signal entirely is mathematically impossible without an out-of-band verification channel: any in-app dispatch on the pickup cookie reveals the branch, and any login-after-register variant turns the probe into a follow-up POST `/api/auth/login` whose success or failure carries the same information. The only options are (a) a magic-link / email-driven enrollment that gates registration behind SMTP delivery (not assumed in the self-hosted deployment model), or (b) acceptance of the documented two-request probe. Both are revisited if Ovumcy ever ships a multi-tenant SaaS variant.

A separate vector — replay of a sealed `ovumcy_register_pickup` cookie captured from somebody else's response within the 5-minute TTL — is **not** part of this residual. The pickup cookie carries an opaque nonce that is consumed atomically through `register_pickup_tokens` on the first `GET /register/welcome` call; a captured cookie reaching the welcome endpoint a second time gets the same neutral `/login` redirect as a decoy or expired pickup, and cannot mint a second `ovumcy_auth` session.

### Login: `requires_totp` reveals 2FA status

`POST /api/auth/login` returns `{"requires_totp": true}` when the supplied password is correct and the account has TOTP enabled, and `{"ok": true}` plus a session cookie otherwise. A credential-dump attacker can use this to triage accounts by whether they have a second factor.

This is inherent to any password-then-TOTP flow that gates the second factor on account state — any uniform response that hides the difference either silently grants access without verifying TOTP (downgrade attack) or unconditionally rejects accounts without TOTP (lockout). The per-account rate limiter (`AuthAttemptPolicy`) and the recovery-code re-auth requirements bound the value of knowing the 2FA status.

## Session Invalidation on Credential Rotation

Operations that rotate a long-lived credential bump `users.auth_session_version` in the same database update, immediately invalidating every active `ovumcy_auth` cookie for that account. This applies to:

- Password change (`POST /api/settings/change-password`).
- Password reset via recovery code (`POST /api/auth/reset-password`).
- Recovery-code regeneration (`POST /api/settings/regenerate-recovery-code`) — the current request receives a freshly issued cookie so the originating session stays alive, but every other device is signed out.
- Forced password reset via the `ovumcy reset-password` operator command.
- TOTP 2FA enable (`POST /api/settings/2fa/verify`) and disable (`POST /api/settings/2fa/disable`) — toggling the second factor is also a change to the account's auth posture, so any cookie issued before the toggle is invalidated. The originating device receives a freshly issued cookie inline; every other device is signed out.

If you suspect a session compromise, regenerating the recovery code is the fastest way to force every other device to re-authenticate without changing your password.
