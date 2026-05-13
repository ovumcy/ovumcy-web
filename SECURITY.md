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

### Register: silenced status code, residual `Set-Cookie` signal

`POST /api/auth/register` returns the same status (`201 Created`) and JSON body for both a brand-new email and a duplicate, but only the new-email response sets the `ovumcy_auth` and `ovumcy_recovery_code` cookies. An attacker who can inspect raw response headers (proxy logs, browser dev tools on their own machine, MITM under a separate threat model) can still tell a collision from a success.

Closing the signal entirely requires either (a) a magic-link / email-driven enrollment flow that does not issue cookies at the register endpoint at all, which depends on SMTP infrastructure Ovumcy does not require operators to configure, or (b) emitting decoy cookies whose downstream resolution fails in exactly the same way as a stale session. Both are tracked as follow-up work; the current state already removes the single-request status-code oracle.

Rate limiting on `/api/auth/register` and the login bcrypt-timing equalization (`equalizeAuthCredentialsTiming`) bound how quickly an attacker can probe an email list against either oracle.

### Login: `requires_totp` reveals 2FA status

`POST /api/auth/login` returns `{"requires_totp": true}` when the supplied password is correct and the account has TOTP enabled, and `{"ok": true}` plus a session cookie otherwise. A credential-dump attacker can use this to triage accounts by whether they have a second factor.

This is inherent to any password-then-TOTP flow that gates the second factor on account state — any uniform response that hides the difference either silently grants access without verifying TOTP (downgrade attack) or unconditionally rejects accounts without TOTP (lockout). The per-account rate limiter (`AuthAttemptPolicy`) and the recovery-code re-auth requirements bound the value of knowing the 2FA status.
