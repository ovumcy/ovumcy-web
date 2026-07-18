# OIDC Account Linking & Session Invalidation

_Part of the [Ovumcy security policy](../../SECURITY.md)._

## OIDC Account Linking

Ovumcy does not trust an upstream OIDC provider to vouch for *which existing local account* a verified email belongs to. The trust given to a configured IdP is "this user controls subject S at issuer I"; **not** "this user controls every Ovumcy account that ever registered with that email".

Concretely: when an OIDC callback returns a (issuer, subject) pair that is not yet linked to any Ovumcy user, the service layer (`internal/services/oidc_login_service.go`) takes one of two paths:

1. **No existing local user with this email** → auto-provision (if `OIDC_AUTO_PROVISION=true` and the email falls under `OIDC_AUTO_PROVISION_ALLOWED_DOMAINS`) and link the new identity inline. No prior owner exists, so no account can be taken over.
2. **A local-auth account already exists for this email** → the service returns `ErrOIDCLinkRequiresConfirmation` with the pending claims. The callback handler stores them in the sealed `ovumcy_oidc_link_pending` cookie (5-minute payload-bound TTL, path-scoped to `/auth/oidc/link-confirm`) and redirects the user to a password-confirmation page. Only after the holder of the existing account submits the correct local password does `ConfirmAndLinkIdentity` persist the link and issue a session.

This defends against the malicious / sloppy upstream IdP scenario: a provider that lets any registrant claim any email (a common default posture in self-hosted OIDC servers like Pocket ID or Authelia under their out-of-the-box configurations) cannot, by asserting `email_verified=true` for somebody else's address, take over the corresponding Ovumcy account.

The confirmation step is refused if the existing account has no local password (`local_auth_enabled=false`). Multi-provider linking onto such accounts is intentionally out of scope for the unauthenticated login path — that has to happen through a future authenticated Settings flow, which is not yet shipped.

When the target account has TOTP enabled, the link-confirmation form additionally requires a valid 6-digit code submitted alongside the password. The handler invokes the same `TOTPService.ValidateCode` path as `/api/v1/sessions/2fa-challenge`, including replay rejection (`ErrTOTPReplayed`) and the per-`(client_ip, user_id)` failure counter. Without this gate, an attacker who has only the victim's password — and uses a malicious or sloppy upstream IdP to assert their email — could obtain a session for a 2FA-protected account without ever holding the second factor, and the linked identity would persist for future OIDC sign-ins.

## Session Invalidation on Credential Rotation

Operations that rotate a long-lived credential bump `users.auth_session_version` in the same database update, immediately invalidating every active `ovumcy_auth` cookie for that account. This applies to:

- Password change (`PUT /api/v1/users/current/password`).
- Password reset via recovery code (`POST /api/v1/password-resets/redeem`).
- Recovery-code regeneration (`POST /api/v1/users/current/recovery-code`) — the current request receives a freshly issued cookie so the originating session stays alive, but every other device is signed out.
- Forced password reset via the `ovumcy reset-password` operator command.
- TOTP 2FA enable (`PUT /api/v1/users/current/2fa`) and disable (`DELETE /api/v1/users/current/2fa`) — toggling the second factor is also a change to the account's auth posture, so any cookie issued before the toggle is invalidated. The originating device receives a freshly issued cookie inline; every other device is signed out.

If you suspect a session compromise, regenerating the recovery code is the fastest way to force every other device to re-authenticate without changing your password.
