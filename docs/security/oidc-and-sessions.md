# OIDC Account Linking & Session Invalidation

_Part of the [Ovumcy security policy](../../SECURITY.md)._

## OIDC Account Linking

Ovumcy does not trust an upstream OIDC provider to vouch for *which existing local account* a verified email belongs to. The trust given to a configured IdP is "this user controls subject S at issuer I"; **not** "this user controls every Ovumcy account that ever registered with that email".

Concretely: when an OIDC callback returns a (issuer, subject) pair that is not yet linked to any Ovumcy user, the service layer (`internal/services/oidc_login_service.go`) takes one of two paths:

1. **No existing local user with this email** → auto-provision (if `OIDC_AUTO_PROVISION=true` and the email falls under `OIDC_AUTO_PROVISION_ALLOWED_DOMAINS`) and link the new identity inline. No prior owner exists, so no account can be taken over.
2. **A local-auth account already exists for this email** → the service returns `ErrOIDCLinkRequiresConfirmation`. The callback handler does **not** hand this off to a public confirmation page: it redirects straight to `/login` with a message pointing the account holder at Settings. Linking is a permanent `(issuer, subject) -> account` binding — the same weight as a password change — and an unauthenticated page cannot verify a factor "now" the way a live session can.

This defends against the malicious / sloppy upstream IdP scenario: a provider that lets any registrant claim any email (a common default posture in self-hosted OIDC servers like Pocket ID or Authelia under their out-of-the-box configurations) cannot, by asserting `email_verified=true` for somebody else's address, take over the corresponding Ovumcy account — there is no page it can be walked through to complete the link, at any password strength.

### Where a link is actually created

There are exactly two paths, and no third:

1. **Authenticated Settings step-up.** `POST /api/v1/users/current/oidc/link/step-up` (any signed-in owner, gated by `AuthRequired` + `OwnerOnly` + CSRF) mints a sealed step-up cookie and sends the browser to the provider with `prompt=login&max_age=0`, forcing a fresh interactive authentication — the same primitive `StartLocalPasswordSetupReauth` and the erasure step-ups already use. The callback (`completeOIDCIdentityLinkStepup`, dispatched from `CompleteOIDCLogin` by the step-up cookie's purpose, before the `RequiresTOTP`/`RequiresPasswordReset` gates described below ever run — a step-up cookie is checked first and dispatches on its own purpose) verifies the session that started the flow is still the one presenting the callback, checks the exchange's `auth_time`/`iat` against the same freshness window (`OIDCLoginService.CompleteIdentityLinkReauth`), and only then calls `ConfirmAndLinkIdentity`. TOTP does not gate this step separately: the fresh interactive provider authentication **is** the step-up factor, exactly as it is for the sibling flows.
2. **Operator CLI, no session.** `ovumcy link-oidc-identity <email>|--id <id> --issuer <issuer> --subject <subject>` addresses the account the same way `reset-password` does (bare email or `--id`, mutually exclusive) and calls the identical `ConfirmAndLinkIdentity` service method directly. This is the recovery path for an account that cannot reach a live session at all — the same shape as `reset-password` for a lost credential.

**There is no unauthenticated path.** `GET`/`POST /auth/oidc/link-confirm` stay registered but the callback never mints the sealed pending-link cookie either handler reads, so neither can ever complete a link from the public internet; this is intentional, not an oversight — see `internal/api/handlers_auth_oidc_link_confirm.go`'s `startOIDCLinkConfirmation` comment for the reasoning and the earlier iteration (password-only, on a page reachable without a session) that this replaced.

When the target account has TOTP enabled, neither of the two paths above asks for a TOTP code again: the settings step-up's freshness proof and the CLI's operator access already outrank the second factor for this one operation, matching how a forced password reset also outranks TOTP.

A later **sign-in** through that same already-linked identity is a separate code path (`CompleteOIDCLogin` → `authenticateLinkedIdentity`, not either linking path above) and is gated the ordinary way: `OIDCLoginService.Authenticate` derives `RequiresPasswordReset` (true when `MustChangePassword` is set OR the account's TOTP is enrolled but unverifiable — its stored secret no longer decrypts, the state a `SECRET_KEY` rotation leaves behind) and sets `RequiresTOTP` only when `RequiresPasswordReset` is false and TOTP is enabled, i.e. only where the factor is actually verifiable (`TOTPService.Verifiable`). The handler redirects to `/reset-password` or `/auth/2fa` before ever calling `setAuthCookie`, mirroring the same predicate, the same ordering, and the same signal the local login path (`handlers_auth_session_login.go` via `LoginService.Authenticate`) uses.

## Session Invalidation on Credential Rotation

Operations that rotate a long-lived credential bump `users.auth_session_version` in the same database update, immediately invalidating every active `ovumcy_auth` cookie for that account. This applies to:

- Password change (`PUT /api/v1/users/current/password`).
- Password reset via recovery code (`POST /api/v1/password-resets/redeem`).
- Recovery-code regeneration (`POST /api/v1/users/current/recovery-code`) — the current request receives a freshly issued cookie so the originating session stays alive, but every other device is signed out.
- Forced password reset via the `ovumcy reset-password` operator command.
- TOTP 2FA enable (`PUT /api/v1/users/current/2fa`) and disable (`DELETE /api/v1/users/current/2fa`) — toggling the second factor is also a change to the account's auth posture, so any cookie issued before the toggle is invalidated. The originating device receives a freshly issued cookie inline; every other device is signed out.
- Clear data (`POST /api/v1/users/current/data-wipe`) — the bump happens inside the same transaction as the wipe, so a stolen session that triggered the wipe cannot retain access to the emptied account, and a "panic clear" really does sign other devices out. The originating device is re-issued a cookie inline. See *Retention and Deletion*.

If you suspect a session compromise, regenerating the recovery code is the fastest way to force every other device to re-authenticate without changing your password.
